package calendar

import (
	"time"

	"github.com/google/uuid"
)

type SlotType string

const (
	SlotTypeLesson SlotType = "LESSON"
	SlotTypeBreak  SlotType = "BREAK"
	SlotTypePrep   SlotType = "PREP"
)

// BreakWindow defines a single break period within a day.
// Stored as JSONB in org_calendar_version.break_structure.
type BreakWindow struct {
	StartTime string `json:"start_time"` // "10:30"
	EndTime   string `json:"end_time"`   // "10:45"
	Label     string `json:"label"`      // "Morning break"
}

type OrgCalendarVersion struct {
	ID                  uuid.UUID     `db:"id"`
	OrgID               uuid.UUID     `db:"org_id"`
	VersionNumber       int16         `db:"version_number"`
	LearningDays        []string      `db:"learning_days"`  // ["MON","TUE","WED","THU","FRI"]
	DayStartTime        time.Time     `db:"day_start_time"` // only time portion used
	DayEndTime          time.Time     `db:"day_end_time"`
	SlotDurationMinutes int16         `db:"slot_duration_minutes"`
	BreakStructure      []BreakWindow `db:"break_structure"`
	IsActive            bool          `db:"is_active"`
	CreatedAt           time.Time     `db:"created_at"`
	UpdatedAt           time.Time     `db:"updated_at"`
}

type TimeSlot struct {
	ID                uuid.UUID `db:"id"`
	OrgID             uuid.UUID `db:"org_id"`
	CalendarVersionID uuid.UUID `db:"calendar_version_id"`
	DayOfWeek         int16     `db:"day_of_week"` // 0=MON .. 6=SUN
	StartTime         time.Time `db:"start_time"`
	EndTime           time.Time `db:"end_time"`
	SlotIndex         int16     `db:"slot_index"`
	SlotType          SlotType  `db:"slot_type"`
	CreatedAt         time.Time `db:"created_at"`
}

// DayOfWeekFromString maps "MON" → 0, "TUE" → 1, etc.
var DayOfWeekFromString = map[string]int16{
	"MON": 0,
	"TUE": 1,
	"WED": 2,
	"THU": 3,
	"FRI": 4,
	"SAT": 5,
	"SUN": 6,
}
