package calendar

import (
	"time"

	"github.com/google/uuid"
)

type SlotType string

const (
	SlotTypeLesson      SlotType = "LESSON"
	SlotTypeBreak       SlotType = "BREAK"
	SlotTypeLunch       SlotType = "LUNCH"
	SlotTypeAssembly    SlotType = "ASSEMBLY"
	SlotTypeNonTeaching SlotType = "NON_TEACHING"
	SlotTypePrep        SlotType = "PREP"
)

// BreakWindow defines a single break period within a day.
// Stored as JSONB in org_calendar_version.break_structure.
type BreakWindow struct {
	StartTime string `json:"start_time"`     // "10:30"
	EndTime   string `json:"end_time"`       // "10:45"
	Label     string `json:"label"`          // "Morning break"
	Kind      string `json:"kind,omitempty"` // BREAK, LUNCH, ASSEMBLY, NON_TEACHING
}

type OrgCalendarVersion struct {
	ID                  uuid.UUID     `db:"id" json:"id"`
	OrgID               uuid.UUID     `db:"org_id" json:"workspace_id"`
	VersionNumber       int16         `db:"version_number" json:"version_number"`
	LearningDays        []string      `db:"learning_days" json:"learning_days"`   // ["MON","TUE","WED","THU","FRI"]
	DayStartTime        time.Time     `db:"day_start_time" json:"day_start_time"` // only time portion used
	DayEndTime          time.Time     `db:"day_end_time" json:"day_end_time"`
	SlotDurationMinutes int16         `db:"slot_duration_minutes" json:"slot_duration_minutes"`
	BreakStructure      []BreakWindow `db:"break_structure" json:"break_structure"`
	IsActive            bool          `db:"is_active" json:"is_active"`
	CreatedAt           time.Time     `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time     `db:"updated_at" json:"updated_at"`
}

type TimeSlot struct {
	ID                uuid.UUID `db:"id" json:"id"`
	OrgID             uuid.UUID `db:"org_id" json:"workspace_id"`
	CalendarVersionID uuid.UUID `db:"calendar_version_id" json:"calendar_version_id"`
	DayOfWeek         int16     `db:"day_of_week" json:"day_of_week"` // 0=MON .. 6=SUN
	StartTime         time.Time `db:"start_time" json:"start_time"`
	EndTime           time.Time `db:"end_time" json:"end_time"`
	SlotIndex         int16     `db:"slot_index" json:"slot_index"`
	SlotType          SlotType  `db:"slot_type" json:"slot_type"`
	CreatedAt         time.Time `db:"created_at" json:"created_at"`
}

// DayOfWeekFromString maps "MON" → 0, "TUE" → 1, etc.
var DayOfWeekFromString = map[string]int16{
	"MON":       0,
	"MONDAY":    0,
	"TUE":       1,
	"TUESDAY":   1,
	"WED":       2,
	"WEDNESDAY": 2,
	"THU":       3,
	"THURSDAY":  3,
	"FRI":       4,
	"FRIDAY":    4,
	"SAT":       5,
	"SATURDAY":  5,
	"SUN":       6,
	"SUNDAY":    6,
}
