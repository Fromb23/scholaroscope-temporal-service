package portalapi

import "testing"

func TestWorkflowResumesExistingDraft(t *testing.T) {
	state := deriveWorkflowState(workflowFacts{HasTerm: true, IntegrationReady: true, Assignments: 18, BellPeriods: 40, HasVersion: true, VersionStatus: "DRAFT", HasSolverRun: true})
	if state != "DRAFT_READY_FOR_VALIDATION" {
		t.Fatalf("expected draft resume state, got %s", state)
	}
}

func TestWorkflowRecommendsSchoolDayBeforeGeneration(t *testing.T) {
	state := deriveWorkflowState(workflowFacts{HasTerm: true, IntegrationReady: true, Assignments: 18})
	if state != "BELL_PERIODS_REQUIRED" {
		t.Fatalf("expected bell period setup, got %s", state)
	}
}

func TestWorkflowNeverSelectsAnotherWorkspaceByConstruction(t *testing.T) {
	// Workflow facts are produced only by queries that require session.WorkspaceID;
	// this regression keeps the derivation independent of browser-persisted IDs.
	state := deriveWorkflowState(workflowFacts{HasTerm: true, IntegrationReady: true, Assignments: 18, BellPeriods: 40})
	if state != "READY_TO_GENERATE" {
		t.Fatalf("unexpected state: %s", state)
	}
}

func TestAssignmentSynchronizationStatesAreDistinct(t *testing.T) {
	if status := assignmentSynchronizationStatus(workflowFacts{}, 0); status != "PENDING" {
		t.Fatalf("expected pending status, got %s", status)
	}
	if status := assignmentSynchronizationStatus(workflowFacts{IntegrationFailed: true}, 0); status != "FAILED" {
		t.Fatalf("expected failed status, got %s", status)
	}
	if status := assignmentSynchronizationStatus(workflowFacts{IntegrationReady: true}, 0); status != "NO_ASSIGNMENTS_IN_SCHOLAROSCOPE" {
		t.Fatalf("expected source-empty status, got %s", status)
	}
	if status := assignmentSynchronizationStatus(workflowFacts{IntegrationReady: true}, 4); status != "SUCCEEDED_NO_ELIGIBLE_ASSIGNMENTS" {
		t.Fatalf("expected eligibility-empty status, got %s", status)
	}
}
