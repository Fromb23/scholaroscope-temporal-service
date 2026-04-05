package availability

import (
	"context"
	"fmt"

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

// Upsert sets or updates a teacher's availability for a specific timeslot.
// Called when kernel emits teacher.assigned or teacher.unassigned events.
func (r *Repo) Upsert(ctx context.Context, input *SetAvailabilityInput) error {
	query := `
		INSERT INTO teacher_availability (id, org_id, teacher_id, timeslot_id, is_available)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (org_id, teacher_id, timeslot_id)
		DO UPDATE SET
			is_available = EXCLUDED.is_available,
			updated_at   = NOW()`

	_, err := r.pool.Exec(ctx, query,
		uuid.New(),
		input.OrgID,
		input.TeacherID,
		input.TimeslotID,
		input.Available,
	)
	if err != nil {
		return fmt.Errorf("availability repo: upsert: %w", err)
	}
	return nil
}

// BulkUpsert sets availability for multiple timeslots in one batch.
// Used when a teacher is assigned to or removed from a cohort-subject.
func (r *Repo) BulkUpsert(ctx context.Context, inputs []SetAvailabilityInput) error {
	if len(inputs) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	query := `
		INSERT INTO teacher_availability (id, org_id, teacher_id, timeslot_id, is_available)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (org_id, teacher_id, timeslot_id)
		DO UPDATE SET
			is_available = EXCLUDED.is_available,
			updated_at   = NOW()`

	for _, input := range inputs {
		batch.Queue(query,
			uuid.New(),
			input.OrgID,
			input.TeacherID,
			input.TimeslotID,
			input.Available,
		)
	}

	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()

	for range inputs {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("availability repo: bulk upsert: %w", err)
		}
	}
	return nil
}

// IsAvailable returns true if the teacher has an explicit availability record
// for the given timeslot AND it is marked available.
// If no record exists, returns true (open by default).
func (r *Repo) IsAvailable(ctx context.Context, orgID, teacherID, timeslotID uuid.UUID) (bool, error) {
	var available bool
	err := r.pool.QueryRow(ctx, `
		SELECT is_available
		FROM teacher_availability
		WHERE org_id    = $1
		  AND teacher_id  = $2
		  AND timeslot_id = $3`,
		orgID, teacherID, timeslotID,
	).Scan(&available)

	if err == pgx.ErrNoRows {
		return true, nil // no record = available by default
	}
	if err != nil {
		return false, fmt.Errorf("availability repo: is available: %w", err)
	}
	return available, nil
}

// GetUnavailableSlots returns all timeslot IDs where teacher is explicitly unavailable.
// Used by the engine to build an exclusion set before iterating candidates.
func (r *Repo) GetUnavailableSlots(ctx context.Context, orgID, teacherID uuid.UUID) (map[uuid.UUID]struct{}, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT timeslot_id
		FROM teacher_availability
		WHERE org_id     = $1
		  AND teacher_id = $2
		  AND is_available = FALSE`,
		orgID, teacherID,
	)
	if err != nil {
		return nil, fmt.Errorf("availability repo: get unavailable slots: %w", err)
	}
	defer rows.Close()

	unavailable := make(map[uuid.UUID]struct{})
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("availability repo: scan unavailable slot: %w", err)
		}
		unavailable[id] = struct{}{}
	}
	return unavailable, rows.Err()
}

// ListForTeacher returns all availability records for a teacher in an org.
func (r *Repo) ListForTeacher(ctx context.Context, orgID, teacherID uuid.UUID) ([]TeacherAvailability, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, org_id, teacher_id, timeslot_id, is_available, updated_at
		FROM teacher_availability
		WHERE org_id     = $1
		  AND teacher_id = $2
		ORDER BY updated_at DESC`,
		orgID, teacherID,
	)
	if err != nil {
		return nil, fmt.Errorf("availability repo: list for teacher: %w", err)
	}
	defer rows.Close()

	var records []TeacherAvailability
	for rows.Next() {
		var a TeacherAvailability
		if err := rows.Scan(&a.ID, &a.OrgID, &a.TeacherID, &a.TimeslotID, &a.IsAvailable, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("availability repo: scan: %w", err)
		}
		records = append(records, a)
	}
	return records, rows.Err()
}
