package calendar

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo *Repo
}

func NewService(repo *Repo) *Service {
	return &Service{repo: repo}
}

type CreateCalendarInput struct {
	LearningDays        []string
	DayStartTime        time.Time
	DayEndTime          time.Time
	SlotDurationMinutes int16
	BreakStructure      []BreakWindow
}

// CreateCalendarWithSlots creates a new calendar version and generates
// all time slots from it in one operation. Does not activate it.
func (s *Service) CreateCalendarWithSlots(ctx context.Context, orgID uuid.UUID, input *CreateCalendarInput) (*OrgCalendarVersion, []TimeSlot, error) {
	// Determine next version number for this org
	nextVersion, err := s.repo.NextVersionNumber(ctx, orgID)
	if err != nil {
		return nil, nil, fmt.Errorf("calendar service: get next version: %w", err)
	}

	version := &OrgCalendarVersion{
		ID:                  uuid.New(),
		OrgID:               orgID,
		VersionNumber:       nextVersion,
		LearningDays:        input.LearningDays,
		DayStartTime:        input.DayStartTime,
		DayEndTime:          input.DayEndTime,
		SlotDurationMinutes: input.SlotDurationMinutes,
		BreakStructure:      input.BreakStructure,
		IsActive:            false,
	}

	if err := s.repo.CreateCalendarVersion(ctx, version); err != nil {
		return nil, nil, fmt.Errorf("calendar service: create version: %w", err)
	}

	slots := generateTimeSlots(version)

	if err := s.repo.BulkInsertTimeSlots(ctx, slots); err != nil {
		return nil, nil, fmt.Errorf("calendar service: persist slots: %w", err)
	}

	return version, slots, nil
}

// GenerateAndPersistTimeSlots regenerates slots for an existing version.
func (s *Service) GenerateAndPersistTimeSlots(ctx context.Context, versionID uuid.UUID) ([]TimeSlot, error) {
	version, err := s.repo.GetCalendarVersionByID(ctx, versionID)
	if err != nil {
		return nil, fmt.Errorf("calendar service: get version: %w", err)
	}

	slots := generateTimeSlots(version)

	if err := s.repo.BulkInsertTimeSlots(ctx, slots); err != nil {
		return nil, fmt.Errorf("calendar service: persist slots: %w", err)
	}

	return slots, nil
}

func generateTimeSlots(v *OrgCalendarVersion) []TimeSlot {
	var slots []TimeSlot

	for _, dayName := range v.LearningDays {
		dayOfWeek, ok := DayOfWeekFromString[dayName]
		if !ok {
			continue
		}

		current := v.DayStartTime
		slotIndex := int16(0)

		for current.Before(v.DayEndTime) {
			slotEnd := current.Add(time.Duration(v.SlotDurationMinutes) * time.Minute)
			if slotEnd.After(v.DayEndTime) {
				break
			}

			slotType := resolveSlotType(current, slotEnd, v.BreakStructure)

			slots = append(slots, TimeSlot{
				ID:                uuid.New(),
				OrgID:             v.OrgID,
				CalendarVersionID: v.ID,
				DayOfWeek:         dayOfWeek,
				StartTime:         current,
				EndTime:           slotEnd,
				SlotIndex:         slotIndex,
				SlotType:          slotType,
			})

			current = slotEnd
			slotIndex++
		}
	}

	return slots
}

func resolveSlotType(start, end time.Time, breaks []BreakWindow) SlotType {
	for _, b := range breaks {
		breakStart, err1 := time.Parse("15:04", b.StartTime)
		breakEnd, err2 := time.Parse("15:04", b.EndTime)
		if err1 != nil || err2 != nil {
			continue
		}
		if start.Before(breakEnd) && end.After(breakStart) {
			return SlotTypeBreak
		}
	}
	return SlotTypeLesson
}
