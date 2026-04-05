package scheduling

import (
	"context"
	"fmt"

	"scholaroscope-temporal-service/internal/conflict"

	"github.com/google/uuid"
)

type Service struct {
	repo         *Repo
	conflictRepo *conflict.Repo
}

func NewService(repo *Repo, conflictRepo *conflict.Repo) *Service {
	return &Service{repo: repo, conflictRepo: conflictRepo}
}

// Schedule attempts to place a session into the first valid slot.
// On success: writes scheduled_session + slot_occupancy atomically.
// On failure: logs a scheduling_conflict and returns the conflict type.
func (s *Service) Schedule(ctx context.Context, req *ScheduleRequest) (*ScheduledSession, error) {
	candidates, err := s.repo.GetLessonSlotsForVersion(ctx, req.CalendarVersionID)
	if err != nil {
		return nil, fmt.Errorf("scheduling service: get candidates: %w", err)
	}

	for _, candidate := range candidates {
		// Validate contiguity for multi-slot sessions
		slots, err := s.repo.GetConsecutiveSlots(ctx,
			req.CalendarVersionID,
			candidate.DayOfWeek,
			candidate.SlotIndex,
			req.DurationSlots,
		)
		if err != nil {
			return nil, fmt.Errorf("scheduling service: get consecutive slots: %w", err)
		}

		// Not enough contiguous LESSON slots available from this position
		if int16(len(slots)) < req.DurationSlots {
			continue
		}

		// Build occupancy rows — one per slot in duration
		occupancies := make([]SlotOccupancy, len(slots))
		for i, sl := range slots {
			occupancies[i] = SlotOccupancy{
				ID:                uuid.New(),
				OrgID:             req.OrgID,
				CalendarVersionID: req.CalendarVersionID,
				SessionID:         req.SessionID,
				DayOfWeek:         sl.DayOfWeek,
				SlotIndex:         sl.SlotIndex,
				TeacherID:         req.TeacherID,
				CohortSubjectID:   req.CohortSubjectID,
			}
		}

		ss := &ScheduledSession{
			ID:                uuid.New(),
			OrgID:             req.OrgID,
			SessionID:         req.SessionID,
			CalendarVersionID: req.CalendarVersionID,
			TimeslotID:        candidate.ID,
			TeacherID:         req.TeacherID,
			CohortSubjectID:   req.CohortSubjectID,
			DurationSlots:     req.DurationSlots,
			ScheduleMode:      req.ScheduleMode,
			IsPinned:          false,
		}

		err = s.repo.ScheduleSession(ctx, ss, occupancies)
		if err == nil {
			return ss, nil // placed successfully
		}

		// UNIQUE constraint violation — slot taken, try next candidate
		continue
	}

	// No valid slot found — log conflict
	s.conflictRepo.Log(ctx, &conflict.SchedulingConflict{
		OrgID:             req.OrgID,
		CalendarVersionID: req.CalendarVersionID,
		SessionID:         req.SessionID,
		ConflictType:      conflict.ConflictNoValidSlot,
		Description: fmt.Sprintf(
			"no valid slot found for session %s (teacher: %s, cohort_subject: %s, duration: %d)",
			req.SessionID, req.TeacherID, req.CohortSubjectID, req.DurationSlots,
		),
	})

	return nil, fmt.Errorf("scheduling service: no valid slot for session %s", req.SessionID)
}

// Unschedule removes a session from the timetable and frees its slots.
func (s *Service) Unschedule(ctx context.Context, sessionID uuid.UUID) error {
	if err := s.repo.UnscheduleSession(ctx, sessionID); err != nil {
		return fmt.Errorf("scheduling service: unschedule: %w", err)
	}
	return nil
}
