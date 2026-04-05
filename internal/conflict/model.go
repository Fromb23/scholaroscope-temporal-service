package conflict

import (
	"time"

	"github.com/google/uuid"
)

type ConflictType string

const (
	ConflictTeacherDoubleBooked  ConflictType = "TEACHER_DOUBLE_BOOKED"
	ConflictCohortClash          ConflictType = "COHORT_CLASH"
	ConflictNoValidSlot          ConflictType = "NO_VALID_SLOT"
	ConflictLessonsUnsatisfied   ConflictType = "LESSONS_UNSATISFIED"
	ConflictContiguityViolation  ConflictType = "CONTIGUITY_VIOLATION"
	ConflictOutsideOrgHours      ConflictType = "OUTSIDE_ORG_HOURS"
	ConflictBreakSlotViolation   ConflictType = "BREAK_SLOT_VIOLATION"
)

type SchedulingConflict struct {
	ID                uuid.UUID    `json:"id"`
	OrgID             uuid.UUID    `json:"org_id"`
	CalendarVersionID uuid.UUID    `json:"calendar_version_id"`
	SessionID         uuid.UUID    `json:"session_id"`
	ConflictType      ConflictType `json:"conflict_type"`
	Description       string       `json:"description"`
	Resolved          bool         `json:"resolved"`
	DetectedAt        time.Time    `json:"detected_at"`
	ResolvedAt        *time.Time   `json:"resolved_at"`
}
