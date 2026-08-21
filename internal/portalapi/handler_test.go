package portalapi

import "testing"

func TestPublicationCalendarHelpers(t *testing.T) {
	if got := dayName(0); got != "MONDAY" {
		t.Fatalf("day 0 = %q, want MONDAY", got)
	}
	if got := dayName(6); got != "SUNDAY" {
		t.Fatalf("day 6 = %q, want SUNDAY", got)
	}
	if got := dayName(7); got != "UNKNOWN" {
		t.Fatalf("out-of-range day = %q, want UNKNOWN", got)
	}
	if got := durationMinutes("08:00", "09:20"); got != 80 {
		t.Fatalf("duration = %d, want 80", got)
	}
	if got := durationMinutes("09:00", "08:00"); got != 0 {
		t.Fatalf("invalid duration = %d, want 0", got)
	}
}
