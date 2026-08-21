package calendar

import (
	"context"
	"fmt"
	"sort"
	"strings"
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

type ValidationError struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string { return e.Message }

func validationError(code, field, message string) error {
	return &ValidationError{Code: code, Field: field, Message: message}
}

// CreateCalendarWithSlots creates a new calendar version and generates
// all time slots from it in one operation. Does not activate it.
func (s *Service) CreateCalendarWithSlots(ctx context.Context, orgID uuid.UUID, input *CreateCalendarInput) (*OrgCalendarVersion, []TimeSlot, error) {
	version, slots, err := s.buildCalendarVersion(ctx, orgID, input)
	if err != nil { return nil, nil, err }

	if err := s.repo.CreateCalendarVersion(ctx, version); err != nil {
		return nil, nil, fmt.Errorf("calendar service: create version: %w", err)
	}

	if err := s.repo.BulkInsertTimeSlots(ctx, slots); err != nil {
		return nil, nil, fmt.Errorf("calendar service: persist slots: %w", err)
	}

	return version, slots, nil
}

// CreateCalendarWithSlotsActivated persists the version, generated slots, and
// active-version switch in one database transaction.
func (s *Service) CreateCalendarWithSlotsActivated(ctx context.Context, orgID uuid.UUID, input *CreateCalendarInput) (*OrgCalendarVersion, []TimeSlot, error) {
	version, slots, err := s.buildCalendarVersion(ctx, orgID, input)
	if err != nil {
		return nil, nil, err
	}
	version.IsActive = true
	if err := s.repo.CreateCalendarVersionWithSlotsActivated(ctx, version, slots); err != nil {
		return nil, nil, fmt.Errorf("calendar service: persist and activate calendar: %w", err)
	}
	return version, slots, nil
}

func (s *Service) buildCalendarVersion(ctx context.Context, orgID uuid.UUID, input *CreateCalendarInput) (*OrgCalendarVersion, []TimeSlot, error) {
	if len(input.LearningDays) == 0 {
		return nil, nil, validationError("LEARNING_DAYS_REQUIRED", "learning_days", "Select at least one learning day.")
	}
	if input.SlotDurationMinutes <= 0 {
		return nil, nil, validationError("INVALID_PERIOD_DURATION", "slot_duration_minutes", "Teaching-period duration must be greater than zero.")
	}
	if !input.DayEndTime.After(input.DayStartTime) {
		return nil, nil, validationError("INVALID_DAY_RANGE", "day_end_time", "School-day end time must be after its start time.")
	}
	if err := validateBreakStructure(input.DayStartTime, input.DayEndTime, input.BreakStructure); err != nil {
		return nil, nil, err
	}
	nextVersion, err := s.repo.NextVersionNumber(ctx, orgID)
	if err != nil {
		return nil, nil, fmt.Errorf("calendar service: get next version: %w", err)
	}
	version := &OrgCalendarVersion{
		ID:                  uuid.New(),
		OrgID:               orgID,
		VersionNumber:       nextVersion,
		LearningDays:        normalizeLearningDays(input.LearningDays),
		DayStartTime:        input.DayStartTime,
		DayEndTime:          input.DayEndTime,
		SlotDurationMinutes: input.SlotDurationMinutes,
		BreakStructure:      input.BreakStructure,
		IsActive:            false,
	}
	if len(version.LearningDays) == 0 {
		return nil, nil, validationError("INVALID_LEARNING_DAYS", "learning_days", "No valid learning day was supplied.")
	}
	if hasDuplicateDays(version.LearningDays) {
		return nil, nil, validationError("DUPLICATE_LEARNING_DAY", "learning_days", "Each learning day may be selected only once.")
	}
	slots := generateTimeSlots(version)
	if err := validateGeneratedLessonSlots(slots, input.SlotDurationMinutes); err != nil {
		return nil, nil, err
	}
	return version, slots, nil
}

func validateGeneratedLessonSlots(slots []TimeSlot, durationMinutes int16) error {
	lessonCount := 0
	for _, slot := range slots {
		if slot.SlotType != SlotTypeLesson {
			continue
		}
		lessonCount++
		if slot.EndTime.Sub(slot.StartTime) != time.Duration(durationMinutes)*time.Minute {
			return validationError("PARTIAL_TEACHING_PERIOD", "day_end_time", "The day bounds and non-teaching periods leave a shortened teaching period. Adjust the end time, breaks, or period duration.")
		}
	}
	if lessonCount == 0 {
		return validationError("NO_TEACHING_PERIODS", "break_structure", "This configuration leaves no teaching periods.")
	}
	return nil
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
		dayOfWeek, ok := DayOfWeekFromString[strings.ToUpper(dayName)]
		if !ok {
			continue
		}

		slotIndex := int16(0)
		breaks := parsedBreaks(v.BreakStructure)
		current := v.DayStartTime

		for current.Before(v.DayEndTime) {
			next := nextBoundary(current, v.DayEndTime, v.SlotDurationMinutes, breaks)
			if !next.After(current) {
				break
			}
			slotType := resolveSlotType(current, next, v.BreakStructure)

			slots = append(slots, TimeSlot{
				ID:                uuid.New(),
				OrgID:             v.OrgID,
				CalendarVersionID: v.ID,
				DayOfWeek:         dayOfWeek,
				StartTime:         current,
				EndTime:           next,
				SlotIndex:         slotIndex,
				SlotType:          slotType,
			})

			current = next
			slotIndex++
		}
	}

	return slots
}

func validateBreakStructure(dayStart, dayEnd time.Time, breaks []BreakWindow) error {
	parsed := make([]breakRange, 0, len(breaks))
	for index, window := range breaks {
		start, startErr := time.Parse("15:04", window.StartTime)
		end, endErr := time.Parse("15:04", window.EndTime)
		if startErr != nil || endErr != nil {
			return validationError("INVALID_NON_TEACHING_TIME", fmt.Sprintf("break_structure.%d", index), fmt.Sprintf("Non-teaching period %d must use valid 24-hour start and end times.", index+1))
		}
		parsed = append(parsed, breakRange{start: start, end: end})
	}
	sort.Slice(parsed, func(i, j int) bool { return parsed[i].start.Before(parsed[j].start) })
	for i, item := range parsed {
		if !item.end.After(item.start) {
			return validationError("INVALID_NON_TEACHING_RANGE", "break_structure", fmt.Sprintf("Non-teaching period %d must end after it starts.", i+1))
		}
		if item.start.Before(dayStart) || item.end.After(dayEnd) {
			return validationError("NON_TEACHING_OUTSIDE_DAY", "break_structure", fmt.Sprintf("Non-teaching period %d must be inside the school day.", i+1))
		}
		if i > 0 && item.start.Before(parsed[i-1].end) {
			return validationError("OVERLAPPING_NON_TEACHING_PERIODS", "break_structure", "Breaks and named non-teaching periods must not overlap.")
		}
	}
	return nil
}

type breakRange struct {
	start time.Time
	end   time.Time
}

func parsedBreaks(breaks []BreakWindow) []breakRange {
	parsed := make([]breakRange, 0, len(breaks))
	for _, b := range breaks {
		breakStart, err1 := time.Parse("15:04", b.StartTime)
		breakEnd, err2 := time.Parse("15:04", b.EndTime)
		if err1 != nil || err2 != nil {
			continue
		}
		parsed = append(parsed, breakRange{start: breakStart, end: breakEnd})
	}
	sort.Slice(parsed, func(i, j int) bool { return parsed[i].start.Before(parsed[j].start) })
	return parsed
}

func nextBoundary(current, dayEnd time.Time, slotDurationMinutes int16, breaks []breakRange) time.Time {
	next := current.Add(time.Duration(slotDurationMinutes) * time.Minute)
	if next.After(dayEnd) {
		next = dayEnd
	}
	for _, item := range breaks {
		if current.Equal(item.start) {
			return item.end
		}
		if current.Before(item.start) && item.start.Before(next) {
			next = item.start
		}
		if current.Before(item.end) && item.end.Before(next) {
			next = item.end
		}
	}
	return next
}

func normalizeLearningDays(days []string) []string {
	normalized := make([]string, 0, len(days))
	for _, day := range days {
		value := strings.ToUpper(strings.TrimSpace(day))
		if _, ok := DayOfWeekFromString[value]; ok {
			normalized = append(normalized, value)
		}
	}
	return normalized
}

func hasDuplicateDays(days []string) bool {
	seen := map[string]bool{}
	for _, day := range days {
		canonical := fmt.Sprintf("%d", DayOfWeekFromString[day])
		if seen[canonical] {
			return true
		}
		seen[canonical] = true
	}
	return false
}

func resolveSlotType(start, end time.Time, breaks []BreakWindow) SlotType {
	for _, b := range breaks {
		breakStart, err1 := time.Parse("15:04", b.StartTime)
		breakEnd, err2 := time.Parse("15:04", b.EndTime)
		if err1 != nil || err2 != nil {
			continue
		}
		if start.Before(breakEnd) && end.After(breakStart) {
			switch strings.ToUpper(strings.TrimSpace(b.Kind)) {
			case "LUNCH":
				return SlotTypeLunch
			case "ASSEMBLY":
				return SlotTypeAssembly
			case "NON_TEACHING":
				return SlotTypeNonTeaching
			default:
				return SlotTypeBreak
			}
		}
	}
	return SlotTypeLesson
}

// GetSlotsForVersion exposes slot retrieval for event handlers.
func (s *Service) GetSlotsForVersion(ctx context.Context, calendarVersionID uuid.UUID) ([]TimeSlot, error) {
	return s.repo.GetTimeSlotsForVersion(ctx, calendarVersionID)
}

func (s *Service) GetActiveCalendar(ctx context.Context, orgID uuid.UUID) (*OrgCalendarVersion, error) {
	return s.repo.GetActiveCalendarVersion(ctx, orgID)
}

func (s *Service) ActivateCalendar(ctx context.Context, orgID, versionID uuid.UUID) error {
	return s.repo.ActivateCalendarVersion(ctx, orgID, versionID)
}
