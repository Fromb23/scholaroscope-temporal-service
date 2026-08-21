package portalapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTermSelectionDefaultsToActiveAndAllowsExplicitHistory(t *testing.T) {
	today := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	yearID := uuid.New()
	second := academicTerm{ID: uuid.New(), AcademicYearID: &yearID, Name: "2nd Term", StartDate: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC), Status: "CLOSED_HISTORICAL"}
	third := academicTerm{ID: uuid.New(), AcademicYearID: &yearID, Name: "3rd Term", StartDate: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC), Status: "OPEN"}

	selected, err := selectTerm([]academicTerm{second, third}, "", today, true)
	if err != nil || selected.ID != third.ID {
		t.Fatal("expected active 3rd Term by default")
	}
	historical, err := selectTerm([]academicTerm{second, third}, second.ID.String(), today, true)
	if err != nil || historical.ID != second.ID {
		t.Fatal("expected explicit 2nd Term history")
	}
}

func TestSchedulingSelectionRejectsUnavailableTerm(t *testing.T) {
	today := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	yearID := uuid.New()
	unavailable := academicTerm{ID: uuid.New(), AcademicYearID: &yearID, Name: "Frozen term", StartDate: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC), Status: "OPEN", Frozen: true}

	if _, err := selectTerm([]academicTerm{unavailable}, unavailable.ID.String(), today, false); err == nil || err.Error() != "term_not_schedulable" {
		t.Fatalf("expected unavailable term to be rejected, got %v", err)
	}
}

func TestExceptionScopeRejectsOtherTermAndAcceptsIntersectingRange(t *testing.T) {
	yearID, secondID, thirdID := uuid.New(), uuid.New(), uuid.New()
	secondEvents := []applicableException{}
	for day := 1; day <= 4; day++ {
		secondEvents = append(secondEvents, applicableException{AcademicYearID: &yearID, TermID: &secondID, StartDate: time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC)})
	}
	thirdEvent := applicableException{AcademicYearID: &yearID, TermID: &thirdID, StartDate: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)}
	start, end := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	for _, event := range secondEvents {
		if exceptionApplies(event, yearID, thirdID, start, end) {
			t.Fatal("none of four 2nd Term events may apply to 3rd Term generation")
		}
	}
	if !exceptionApplies(thirdEvent, yearID, thirdID, start, end) {
		t.Fatal("intersecting 3rd Term event should apply")
	}
}
