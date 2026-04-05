package scheduling

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// GetLessonSlotsForVersion returns all LESSON slots for a calendar version,
// ordered by day and slot index. The engine iterates these to find placements.
func (r *Repo) GetLessonSlotsForVersion(ctx context.Context, calendarVersionID uuid.UUID) ([]SlotCandidate, error) {
	query := `
		SELECT id, day_of_week, slot_index, slot_type
		FROM time_slot
		WHERE calendar_version_id = $1
		  AND slot_type = 'LESSON'
		ORDER BY day_of_week, slot_index`

	rows, err := r.pool.Query(ctx, query, calendarVersionID)
	if err != nil {
		return nil, fmt.Errorf("scheduling repo: get lesson slots: %w", err)
	}
	defer rows.Close()

	var slots []SlotCandidate
	for rows.Next() {
		var s SlotCandidate
		if err := rows.Scan(&s.ID, &s.DayOfWeek, &s.SlotIndex, &s.SlotType); err != nil {
			return nil, fmt.Errorf("scheduling repo: scan slot: %w", err)
		}
		slots = append(slots, s)
	}
	return slots, rows.Err()
}

// GetConsecutiveSlots returns N consecutive LESSON slots starting from a given
// day and slot index. Used to validate contiguity for multi-slot sessions.
func (r *Repo) GetConsecutiveSlots(ctx context.Context, calendarVersionID uuid.UUID, dayOfWeek, startSlotIndex, count int16) ([]SlotCandidate, error) {
	query := `
		SELECT id, day_of_week, slot_index, slot_type
		FROM time_slot
		WHERE calendar_version_id = $1
		  AND day_of_week = $2
		  AND slot_index >= $3
		  AND slot_index < $4
		  AND slot_type = 'LESSON'
		ORDER BY slot_index`

	rows, err := r.pool.Query(ctx, query,
		calendarVersionID,
		dayOfWeek,
		startSlotIndex,
		startSlotIndex+count,
	)
	if err != nil {
		return nil, fmt.Errorf("scheduling repo: get consecutive slots: %w", err)
	}
	defer rows.Close()

	var slots []SlotCandidate
	for rows.Next() {
		var s SlotCandidate
		if err := rows.Scan(&s.ID, &s.DayOfWeek, &s.SlotIndex, &s.SlotType); err != nil {
			return nil, fmt.Errorf("scheduling repo: scan consecutive slot: %w", err)
		}
		slots = append(slots, s)
	}
	return slots, rows.Err()
}

// ScheduleSession writes scheduled_session + slot_occupancy rows atomically.
// If the UNIQUE constraints on slot_occupancy are violated the tx rolls back
// and the caller logs a conflict.
func (r *Repo) ScheduleSession(ctx context.Context, ss *ScheduledSession, occupancies []SlotOccupancy) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("scheduling repo: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Insert scheduled_session
	_, err = tx.Exec(ctx, `
		INSERT INTO scheduled_session (
			id, org_id, session_id, calendar_version_id,
			timeslot_id, teacher_id, cohort_subject_id,
			duration_slots, schedule_mode, is_pinned
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		ss.ID,
		ss.OrgID,
		ss.SessionID,
		ss.CalendarVersionID,
		ss.TimeslotID,
		ss.TeacherID,
		ss.CohortSubjectID,
		ss.DurationSlots,
		string(ss.ScheduleMode),
		ss.IsPinned,
	)
	if err != nil {
		return fmt.Errorf("scheduling repo: insert scheduled_session: %w", err)
	}

	// Insert one slot_occupancy row per slot in duration
	for _, o := range occupancies {
		_, err = tx.Exec(ctx, `
			INSERT INTO slot_occupancy (
				id, org_id, calendar_version_id, session_id,
				day_of_week, slot_index, teacher_id, cohort_subject_id
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			o.ID,
			o.OrgID,
			o.CalendarVersionID,
			o.SessionID,
			o.DayOfWeek,
			o.SlotIndex,
			o.TeacherID,
			o.CohortSubjectID,
		)
		if err != nil {
			return fmt.Errorf("scheduling repo: insert slot_occupancy: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// GetScheduledSession returns a scheduled session by kernel session ID.
func (r *Repo) GetScheduledSession(ctx context.Context, sessionID uuid.UUID) (*ScheduledSession, error) {
	query := `
		SELECT id, org_id, session_id, calendar_version_id,
		       timeslot_id, teacher_id, cohort_subject_id,
		       duration_slots, schedule_mode, is_pinned,
		       scheduled_at, updated_at
		FROM scheduled_session
		WHERE session_id = $1`

	var ss ScheduledSession
	err := r.pool.QueryRow(ctx, query, sessionID).Scan(
		&ss.ID, &ss.OrgID, &ss.SessionID, &ss.CalendarVersionID,
		&ss.TimeslotID, &ss.TeacherID, &ss.CohortSubjectID,
		&ss.DurationSlots, &ss.ScheduleMode, &ss.IsPinned,
		&ss.ScheduledAt, &ss.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scheduling repo: get scheduled session: %w", err)
	}
	return &ss, nil
}

// ListScheduledSessions returns all scheduled sessions for an org and calendar version.
func (r *Repo) ListScheduledSessions(ctx context.Context, orgID, calendarVersionID uuid.UUID) ([]ScheduledSession, error) {
	query := `
		SELECT id, org_id, session_id, calendar_version_id,
		       timeslot_id, teacher_id, cohort_subject_id,
		       duration_slots, schedule_mode, is_pinned,
		       scheduled_at, updated_at
		FROM scheduled_session
		WHERE org_id = $1
		  AND calendar_version_id = $2
		ORDER BY scheduled_at`

	rows, err := r.pool.Query(ctx, query, orgID, calendarVersionID)
	if err != nil {
		return nil, fmt.Errorf("scheduling repo: list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []ScheduledSession
	for rows.Next() {
		var ss ScheduledSession
		err := rows.Scan(
			&ss.ID, &ss.OrgID, &ss.SessionID, &ss.CalendarVersionID,
			&ss.TimeslotID, &ss.TeacherID, &ss.CohortSubjectID,
			&ss.DurationSlots, &ss.ScheduleMode, &ss.IsPinned,
			&ss.ScheduledAt, &ss.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scheduling repo: scan session: %w", err)
		}
		sessions = append(sessions, ss)
	}
	return sessions, rows.Err()
}

// UnscheduleSession removes a scheduled session and its occupancy rows atomically.
func (r *Repo) UnscheduleSession(ctx context.Context, sessionID uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("scheduling repo: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`DELETE FROM slot_occupancy WHERE session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("scheduling repo: delete occupancy: %w", err)
	}

	_, err = tx.Exec(ctx,
		`DELETE FROM scheduled_session WHERE session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("scheduling repo: delete scheduled session: %w", err)
	}

	return tx.Commit(ctx)
}
