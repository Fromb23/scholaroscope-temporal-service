package portalapi

import (
	"net/http"
	"testing"
)

func TestKnownPublicationErrorHasStableReadableContract(t *testing.T) {
	error := errorContract(http.StatusConflict, "complete_solver_validation_required")
	if error.Code != "draft_validation_required" {
		t.Fatalf("unexpected code: %s", error.Code)
	}
	if error.Message != "Validate the timetable and resolve any highlighted issues before publishing." {
		t.Fatalf("unexpected message: %s", error.Message)
	}
	if error.Action == nil || error.Action.Target != "/timetable" {
		t.Fatal("expected actionable timetable target")
	}
}

func TestUnexpectedFailureUsesSafeGenericContract(t *testing.T) {
	error := errorContract(http.StatusInternalServerError, "pq: relation secret_table does not exist")
	if error.Code != "timetable_update_failed" {
		t.Fatalf("unexpected code: %s", error.Code)
	}
	if error.Message != "Something went wrong while updating the timetable. Please try again." {
		t.Fatalf("unexpected message: %s", error.Message)
	}
}

func TestStaleAcademicDataExplainsRegeneration(t *testing.T) {
	error := errorContract(http.StatusConflict, "academic_data_stale")
	if error.Code != "academic_data_stale" || error.Action == nil || error.Action.Label != "Regenerate timetable" {
		t.Fatalf("unexpected stale-data contract: %#v", error)
	}
}

func TestPublicationPrerequisitesAreDeterministic(t *testing.T) {
	if blocker := publicationBlocker("DRAFT", "COMPLETE", 0, 0, 0); blocker != "complete_solver_validation_required" {
		t.Fatalf("expected validation blocker, got %s", blocker)
	}
	if blocker := publicationBlocker("VALIDATED", "COMPLETE", 0, 0, 1); blocker != "hard_conflicts_block_publication" {
		t.Fatalf("expected hard-conflict blocker, got %s", blocker)
	}
	if blocker := publicationBlocker("VALIDATED", "COMPLETE", 0, 0, 0); blocker != "" {
		t.Fatalf("expected publishable version, got %s", blocker)
	}
}
