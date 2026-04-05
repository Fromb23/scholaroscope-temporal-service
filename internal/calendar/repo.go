package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// CreateCalendarVersion inserts a new org_calendar_version row.
func (r *Repo) CreateCalendarVersion(ctx context.Context, v *OrgCalendarVersion) error {
	learningDaysJSON, err := json.Marshal(v.LearningDays)
	if err != nil {
		return fmt.Errorf("calendar repo: marshal learning_days: %w", err)
	}
	breakStructureJSON, err := json.Marshal(v.BreakStructure)
	if err != nil {
		return fmt.Errorf("calendar repo: marshal break_structure: %w", err)
	}

	query := `
		INSERT INTO org_calendar_version (
			id, org_id, version_number, learning_days,
			day_start_time, day_end_time, slot_duration_minutes,
			break_structure, is_active
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7,
			$8, $9
		)`

	_, err = r.pool.Exec(ctx, query,
		v.ID,
		v.OrgID,
		v.VersionNumber,
		learningDaysJSON,
		v.DayStartTime.Format("15:04:05"),
		v.DayEndTime.Format("15:04:05"),
		v.SlotDurationMinutes,
		breakStructureJSON,
		v.IsActive,
	)
	if err != nil {
		return fmt.Errorf("calendar repo: create version: %w", err)
	}
	return nil
}

// GetActiveCalendarVersion returns the single active version for an org.
func (r *Repo) GetActiveCalendarVersion(ctx context.Context, orgID uuid.UUID) (*OrgCalendarVersion, error) {
	query := `
		SELECT
			id, org_id, version_number, learning_days,
			day_start_time, day_end_time, slot_duration_minutes,
			break_structure, is_active, created_at, updated_at
		FROM org_calendar_version
		WHERE org_id = $1 AND is_active = TRUE
		LIMIT 1`

	row := r.pool.QueryRow(ctx, query, orgID)
	return scanCalendarVersion(row)
}

// GetCalendarVersionByID returns a specific version by its UUID.
func (r *Repo) GetCalendarVersionByID(ctx context.Context, id uuid.UUID) (*OrgCalendarVersion, error) {
	query := `
		SELECT
			id, org_id, version_number, learning_days,
			day_start_time, day_end_time, slot_duration_minutes,
			break_structure, is_active, created_at, updated_at
		FROM org_calendar_version
		WHERE id = $1`

	row := r.pool.QueryRow(ctx, query, id)
	return scanCalendarVersion(row)
}

// ActivateCalendarVersion sets a version as active and deactivates all others
// for the same org — in a single transaction.
func (r *Repo) ActivateCalendarVersion(ctx context.Context, orgID, versionID uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("calendar repo: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Deactivate all versions for this org
	_, err = tx.Exec(ctx,
		`UPDATE org_calendar_version SET is_active = FALSE, updated_at = NOW()
		 WHERE org_id = $1`,
		orgID,
	)
	if err != nil {
		return fmt.Errorf("calendar repo: deactivate versions: %w", err)
	}

	// Activate the target version
	_, err = tx.Exec(ctx,
		`UPDATE org_calendar_version SET is_active = TRUE, updated_at = NOW()
		 WHERE id = $1 AND org_id = $2`,
		versionID, orgID,
	)
	if err != nil {
		return fmt.Errorf("calendar repo: activate version: %w", err)
	}

	return tx.Commit(ctx)
}

// BulkInsertTimeSlots inserts all generated slots using a pgx batch.
func (r *Repo) BulkInsertTimeSlots(ctx context.Context, slots []TimeSlot) error {
	if len(slots) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	query := `
		INSERT INTO time_slot (
			id, org_id, calendar_version_id, day_of_week,
			start_time, end_time, slot_index, slot_type
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT DO NOTHING`

	for _, s := range slots {
		batch.Queue(query,
			s.ID,
			s.OrgID,
			s.CalendarVersionID,
			s.DayOfWeek,
			s.StartTime.Format("15:04:05"),
			s.EndTime.Format("15:04:05"),
			s.SlotIndex,
			string(s.SlotType),
		)
	}

	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()

	for range slots {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("calendar repo: bulk insert slots: %w", err)
		}
	}
	return nil
}

// GetTimeSlotsForVersion returns all slots for a calendar version, ordered.
func (r *Repo) GetTimeSlotsForVersion(ctx context.Context, calendarVersionID uuid.UUID) ([]TimeSlot, error) {
	query := `
		SELECT id, org_id, calendar_version_id, day_of_week,
		       start_time, end_time, slot_index, slot_type, created_at
		FROM time_slot
		WHERE calendar_version_id = $1
		ORDER BY day_of_week, slot_index`

	rows, err := r.pool.Query(ctx, query, calendarVersionID)
	if err != nil {
		return nil, fmt.Errorf("calendar repo: query slots: %w", err)
	}
	defer rows.Close()

	var slots []TimeSlot
	for rows.Next() {
		var s TimeSlot
		var startStr, endStr string
		err := rows.Scan(
			&s.ID, &s.OrgID, &s.CalendarVersionID, &s.DayOfWeek,
			&startStr, &endStr, &s.SlotIndex, &s.SlotType, &s.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("calendar repo: scan slot: %w", err)
		}
		s.StartTime, _ = time.Parse("15:04:05", startStr)
		s.EndTime, _ = time.Parse("15:04:05", endStr)
		slots = append(slots, s)
	}
	return slots, rows.Err()
}

// --- helpers ---

func scanCalendarVersion(row pgx.Row) (*OrgCalendarVersion, error) {
	var v OrgCalendarVersion
	var learningDaysJSON, breakStructureJSON []byte
	var startStr, endStr string

	err := row.Scan(
		&v.ID, &v.OrgID, &v.VersionNumber, &learningDaysJSON,
		&startStr, &endStr, &v.SlotDurationMinutes,
		&breakStructureJSON, &v.IsActive, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("calendar repo: scan version: %w", err)
	}

	if err := json.Unmarshal(learningDaysJSON, &v.LearningDays); err != nil {
		return nil, fmt.Errorf("calendar repo: unmarshal learning_days: %w", err)
	}
	if err := json.Unmarshal(breakStructureJSON, &v.BreakStructure); err != nil {
		return nil, fmt.Errorf("calendar repo: unmarshal break_structure: %w", err)
	}

	v.DayStartTime, _ = time.Parse("15:04:05", startStr)
	v.DayEndTime, _ = time.Parse("15:04:05", endStr)

	return &v, nil
}

// NextVersionNumber returns the next available version number for an org.
func (r *Repo) NextVersionNumber(ctx context.Context, orgID uuid.UUID) (int16, error) {
	var max int16
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(version_number), 0) FROM org_calendar_version WHERE org_id = $1`,
		orgID,
	).Scan(&max)
	if err != nil {
		return 0, fmt.Errorf("calendar repo: next version number: %w", err)
	}
	return max + 1, nil
}
