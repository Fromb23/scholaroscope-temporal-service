package scheduling

import (
	"context"
	"fmt"

	"scholaroscope-temporal-service/internal/availability"
	"scholaroscope-temporal-service/internal/conflict"

	"github.com/google/uuid"
)

type Service struct {
	repo             *Repo
	conflictRepo     *conflict.Repo
	availabilityRepo *availability.Repo
}

func NewService(repo *Repo, conflictRepo *conflict.Repo, availabilityRepo *availability.Repo) *Service {
	return &Service{
		repo:             repo,
		conflictRepo:     conflictRepo,
		availabilityRepo: availabilityRepo,
	}
}

func (s *Service) Schedule(ctx context.Context, req *ScheduleRequest) (*ScheduledSession, error) {
	unavailable, err := s.availabilityRepo.GetUnavailableSlots(ctx, req.OrgID, req.TeacherID)
	if err != nil {
		return nil, fmt.Errorf("scheduling service: get unavailable slots: %w", err)
	}

	candidates, err := s.repo.GetLessonSlotsForVersion(ctx, req.CalendarVersionID)
	if err != nil {
		return nil, fmt.Errorf("scheduling service: get candidates: %w", err)
	}

	for _, candidate := range candidates {
		if _, blocked := unavailable[candidate.ID]; blocked {
			continue
		}

		slots, err := s.repo.GetConsecutiveSlots(ctx,
			req.CalendarVersionID,
			candidate.DayOfWeek,
			candidate.SlotIndex,
			req.DurationSlots,
		)
		if err != nil {
			return nil, fmt.Errorf("scheduling service: get consecutive slots: %w", err)
		}

		if int16(len(slots)) < req.DurationSlots {
			continue
		}

		allAvailable := true
		for _, sl := range slots[1:] {
			if _, blocked := unavailable[sl.ID]; blocked {
				allAvailable = false
				break
			}
		}
		if !allAvailable {
			continue
		}

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

		// Returns re-fetched row with accurate DB timestamps
		persisted, err := s.repo.ScheduleSession(ctx, ss, occupancies)
		if err == nil {
			return persisted, nil
		}

		// UNIQUE constraint violation — slot taken, try next
		continue
	}

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

func (s *Service) Unschedule(ctx context.Context, sessionID uuid.UUID) error {
	if err := s.repo.UnscheduleSession(ctx, sessionID); err != nil {
		return fmt.Errorf("scheduling service: unschedule: %w", err)
	}
	return nil
}
