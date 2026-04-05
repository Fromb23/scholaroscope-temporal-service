package scheduling

import (
	"time"

	"github.com/google/uuid"
)

type ScheduleMode string

const (
	ScheduleModeLearning ScheduleMode = "LEARNING"
	ScheduleModeExam     ScheduleMode = "EXAM"
)

type ScheduledSession struct {
	ID                uuid.UUID    `json:"id"`
	OrgID             uuid.UUID    `json:"org_id"`
	SessionID         uuid.UUID    `json:"session_id"`
	CalendarVersionID uuid.UUID    `json:"calendar_version_id"`
	TimeslotID        uuid.UUID    `json:"timeslot_id"`
	TeacherID         uuid.UUID    `json:"teacher_id"`
	CohortSubjectID   uuid.UUID    `json:"cohort_subject_id"`
	DurationSlots     int16        `json:"duration_slots"`
	ScheduleMode      ScheduleMode `json:"schedule_mode"`
	IsPinned          bool         `json:"is_pinned"`
	ScheduledAt       time.Time    `json:"scheduled_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
}

type SlotOccupancy struct {
	ID                uuid.UUID `json:"id"`
	OrgID             uuid.UUID `json:"org_id"`
	CalendarVersionID uuid.UUID `json:"calendar_version_id"`
	SessionID         uuid.UUID `json:"session_id"`
	DayOfWeek         int16     `json:"day_of_week"`
	SlotIndex         int16     `json:"slot_index"`
	TeacherID         uuid.UUID `json:"teacher_id"`
	CohortSubjectID   uuid.UUID `json:"cohort_subject_id"`
	CreatedAt         time.Time `json:"created_at"`
}

// ScheduleRequest is the input to the scheduling engine.
type ScheduleRequest struct {
	OrgID             uuid.UUID
	SessionID         uuid.UUID
	CalendarVersionID uuid.UUID
	TeacherID         uuid.UUID
	CohortSubjectID   uuid.UUID
	DurationSlots     int16
	ScheduleMode      ScheduleMode
}

// SlotCandidate is a time_slot row the engine evaluates for placement.
type SlotCandidate struct {
	ID        uuid.UUID
	DayOfWeek int16
	SlotIndex int16
	SlotType  string
}
