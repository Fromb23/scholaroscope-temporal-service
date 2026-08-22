package calendar

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func clock(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestCalendarGeneratesDeterministicNamedPeriods(t *testing.T) {
	version := &OrgCalendarVersion{ID: uuid.New(), OrgID: uuid.New(), LearningDays: []string{"MONDAY"}, DayStartTime: clock(t, "08:00"), DayEndTime: clock(t, "15:40"), SlotDurationMinutes: 40, BreakStructure: []BreakWindow{{Label: "First break", StartTime: "10:00", EndTime: "10:20", Kind: "BREAK"}, {Label: "Lunch", StartTime: "12:20", EndTime: "13:00", Kind: "LUNCH"}}}
	slots := generateTimeSlots(version)
	if err := validateGeneratedLessonSlots(slots, 40); err != nil {
		t.Fatal(err)
	}
	lessons, breaks, lunches := 0, 0, 0
	for _, slot := range slots {
		switch slot.SlotType {
		case SlotTypeLesson:
			lessons++
		case SlotTypeBreak:
			breaks++
		case SlotTypeLunch:
			lunches++
		}
	}
	if lessons != 10 || breaks != 1 || lunches != 1 {
		t.Fatalf("lessons=%d breaks=%d lunches=%d", lessons, breaks, lunches)
	}
	again := generateTimeSlots(version)
	for index := range slots {
		if slots[index].DayOfWeek != again[index].DayOfWeek || !slots[index].StartTime.Equal(again[index].StartTime) || !slots[index].EndTime.Equal(again[index].EndTime) || slots[index].SlotType != again[index].SlotType {
			t.Fatal("period structure is not deterministic")
		}
	}
}

func TestCalendarRejectsMalformedAndOverlappingBreaks(t *testing.T) {
	start, end := clock(t, "08:00"), clock(t, "16:00")
	for _, test := range []struct {
		name   string
		breaks []BreakWindow
		code   string
	}{
		{"malformed", []BreakWindow{{StartTime: "25:00", EndTime: "10:00"}}, "INVALID_NON_TEACHING_TIME"},
		{"zero", []BreakWindow{{StartTime: "10:00", EndTime: "10:00"}}, "INVALID_NON_TEACHING_RANGE"},
		{"overlap", []BreakWindow{{StartTime: "10:00", EndTime: "10:30"}, {StartTime: "10:20", EndTime: "10:40"}}, "OVERLAPPING_NON_TEACHING_PERIODS"},
		{"outside", []BreakWindow{{StartTime: "07:50", EndTime: "08:10"}}, "NON_TEACHING_OUTSIDE_DAY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateBreakStructure(start, end, test.breaks)
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Code != test.code {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestCalendarRepresentsRemainderAsTransition(t *testing.T) {
	version := &OrgCalendarVersion{ID: uuid.New(), OrgID: uuid.New(), LearningDays: []string{"MONDAY"}, DayStartTime: clock(t, "08:00"), DayEndTime: clock(t, "08:50"), SlotDurationMinutes: 40}
	slots := generateTimeSlots(version)
	if err := validateGeneratedLessonSlots(slots, 40); err != nil {
		t.Fatal(err)
	}
	if len(slots) != 2 || slots[0].SlotType != SlotTypeLesson || slots[1].SlotType != SlotTypeTransition {
		t.Fatalf("unexpected slots: %+v", slots)
	}
	if got := slots[0].EndTime.Sub(slots[0].StartTime); got != 40*time.Minute {
		t.Fatalf("lesson duration = %s", got)
	}
	if got := slots[1].EndTime.Sub(slots[1].StartTime); got != 10*time.Minute {
		t.Fatalf("transition duration = %s", got)
	}
}

func TestCalendarPreservesFlexibleBreakBoundaries(t *testing.T) {
	version := &OrgCalendarVersion{ID: uuid.New(), OrgID: uuid.New(), LearningDays: []string{"MONDAY"}, DayStartTime: clock(t, "08:00"), DayEndTime: clock(t, "16:00"), SlotDurationMinutes: 40, BreakStructure: []BreakWindow{{Label: "Morning break", StartTime: "11:10", EndTime: "11:50", Kind: "BREAK"}, {Label: "Lunch", StartTime: "13:10", EndTime: "13:40", Kind: "LUNCH"}}}
	slots := generateTimeSlots(version)
	if err := validateGeneratedLessonSlots(slots, 40); err != nil {
		t.Fatal(err)
	}
	got := make([]SlotType, 0, len(slots))
	for _, slot := range slots {
		got = append(got, slot.SlotType)
		if slot.SlotType == SlotTypeLesson && slot.EndTime.Sub(slot.StartTime) != 40*time.Minute {
			t.Fatalf("shortened lesson: %+v", slot)
		}
	}
	want := []SlotType{SlotTypeLesson, SlotTypeLesson, SlotTypeLesson, SlotTypeLesson, SlotTypeTransition, SlotTypeBreak, SlotTypeLesson, SlotTypeLesson, SlotTypeLunch, SlotTypeLesson, SlotTypeLesson, SlotTypeLesson, SlotTypeTransition}
	if len(got) != len(want) {
		t.Fatalf("slot count=%d want=%d got=%v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("slot %d=%s want=%s all=%v", index, got[index], want[index], got)
		}
	}
}

func TestCalendarRejectsNoFullTeachingPeriod(t *testing.T) {
	version := &OrgCalendarVersion{ID: uuid.New(), OrgID: uuid.New(), LearningDays: []string{"MONDAY"}, DayStartTime: clock(t, "08:00"), DayEndTime: clock(t, "08:30"), SlotDurationMinutes: 40}
	err := validateGeneratedLessonSlots(generateTimeSlots(version), 40)
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Code != "NO_TEACHING_PERIODS" {
		t.Fatalf("got %v", err)
	}
}
