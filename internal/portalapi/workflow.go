package portalapi

import (
	"net/http"
	"time"

	"scholaroscope-temporal-service/internal/launch"

	"github.com/google/uuid"
)

type workflowFacts struct {
	HasTerm           bool
	IntegrationReady  bool
	IntegrationFailed bool
	Assignments       int
	BellPeriods       int
	HasVersion        bool
	VersionStatus     string
	GenerationRunning bool
	HasSolverRun      bool
	HardConflicts     int
	Unscheduled       int
}

func deriveWorkflowState(facts workflowFacts) string {
	if !facts.HasTerm {
		return "ACADEMIC_CONTEXT_REQUIRED"
	}
	if facts.IntegrationFailed {
		return "INTEGRATION_DEGRADED"
	}
	if !facts.IntegrationReady || facts.Assignments == 0 {
		return "ASSIGNMENTS_REQUIRED"
	}
	if facts.BellPeriods == 0 {
		return "BELL_PERIODS_REQUIRED"
	}
	if facts.GenerationRunning {
		return "GENERATING"
	}
	if !facts.HasVersion {
		return "READY_TO_GENERATE"
	}
	if facts.VersionStatus == "PUBLISHED" {
		return "PUBLISHED"
	}
	if facts.HardConflicts > 0 {
		return "DRAFT_HAS_CONFLICTS"
	}
	if !facts.HasSolverRun || facts.Unscheduled > 0 {
		return "DRAFT_IN_PROGRESS"
	}
	if facts.VersionStatus == "VALIDATED" {
		return "READY_TO_PUBLISH"
	}
	return "DRAFT_READY_FOR_VALIDATION"
}

func workflowCopy(state, termName string) (string, errorAction) {
	switch state {
	case "ACADEMIC_CONTEXT_REQUIRED":
		return "No active term is available for this workspace. Create or activate a term in Scholaroscope.", errorAction{"Review academic context", "/timetable"}
	case "INTEGRATION_DEGRADED":
		return "Academic synchronization needs attention before the timetable can be updated safely.", errorAction{"Review synchronization", "/classes-teachers"}
	case "ASSIGNMENTS_REQUIRED":
		return "No teachers are assigned to class subjects yet. Complete teacher assignments in Scholaroscope.", errorAction{"Review classes and teachers", "/classes-teachers"}
	case "BELL_PERIODS_REQUIRED":
		return "Set up your school day to create timetable periods.", errorAction{"Set up school day", "/school-day"}
	case "READY_TO_GENERATE":
		return "Generate a timetable for " + termName + ".", errorAction{"Generate timetable", "/timetable"}
	case "GENERATING":
		return "Timetable generation is in progress. This draft will update when generation finishes.", errorAction{"View progress", "/timetable"}
	case "DRAFT_IN_PROGRESS":
		return "Continue the current draft or regenerate it after academic changes.", errorAction{"Open draft", "/timetable"}
	case "DRAFT_HAS_CONFLICTS":
		return "Resolve the highlighted timetable conflicts before validation.", errorAction{"Review conflicts", "/timetable"}
	case "DRAFT_READY_FOR_VALIDATION":
		return "Review the generated grid, then validate it for publication.", errorAction{"Validate timetable", "/timetable"}
	case "READY_TO_PUBLISH":
		return "This timetable is valid and ready to publish.", errorAction{"Publish timetable", "/timetable"}
	case "PUBLISHED":
		return "The current timetable is published. Create a revision to make changes.", errorAction{"Create revision", "/timetable"}
	default:
		return "Continue timetable setup.", errorAction{"Open timetable", "/timetable"}
	}
}

func assignmentSynchronizationStatus(facts workflowFacts, sourceAssignments int) string {
	if facts.IntegrationFailed {
		return "FAILED"
	}
	if !facts.IntegrationReady {
		return "PENDING"
	}
	if facts.Assignments > 0 {
		return "SUCCEEDED"
	}
	if sourceAssignments == 0 {
		return "NO_ASSIGNMENTS_IN_SCHOLAROSCOPE"
	}
	return "SUCCEEDED_NO_ELIGIBLE_ASSIGNMENTS"
}

func (h *Handler) Workflow(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	today, _ := time.Parse("2006-01-02", workspaceLocalDate(session.WorkspaceTimezone))
	terms, err := h.academicTerms(r.Context(), session.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "workflow_query_failed")
		return
	}
	selected, selectErr := selectTerm(terms, r.URL.Query().Get("term_uuid"), today, false)
	if selectErr != nil && r.URL.Query().Get("term_uuid") != "" {
		writeError(w, http.StatusBadRequest, selectErr.Error())
		return
	}
	facts := workflowFacts{}
	var termID, academicYearID uuid.UUID
	termName := "the active term"
	if selectErr == nil && selected != nil && selected.AcademicYearID != nil {
		facts.HasTerm = true
		termID, academicYearID, termName = selected.ID, *selected.AcademicYearID, selected.Name
	}
	var provisioning, health string
	var reconciliation bool
	var lastSync *time.Time
	var snapshotHash *string
	var sourceAssignments, eligibleAssignments int
	_ = h.pool.QueryRow(r.Context(), `SELECT provisioning_state, integration_health, reconciliation_required, last_successful_sync_at, academic_snapshot_hash, source_assignment_count, eligible_assignment_count FROM external_workspace WHERE id = $1`, session.WorkspaceID).Scan(&provisioning, &health, &reconciliation, &lastSync, &snapshotHash, &sourceAssignments, &eligibleAssignments)
	facts.IntegrationReady = provisioning == "READY" && lastSync != nil && snapshotHash != nil && *snapshotHash != ""
	facts.IntegrationFailed = (health != "" && health != "HEALTHY" && health != "UNKNOWN") || reconciliation
	if facts.HasTerm {
		_ = h.pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM external_teaching_assignment eta JOIN external_cohort c ON c.id = eta.cohort_uuid AND c.workspace_id = eta.workspace_id WHERE eta.workspace_id = $1 AND eta.status = 'ACTIVE' AND c.status = 'ACTIVE' AND c.academic_year_uuid = $2`, session.WorkspaceID, academicYearID).Scan(&facts.Assignments)
	}
	_ = h.pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM time_slot slot JOIN org_calendar_version calendar ON calendar.id = slot.calendar_version_id AND calendar.org_id = slot.org_id WHERE slot.org_id = $1 AND calendar.is_active = true AND slot.slot_type = 'LESSON'`, session.WorkspaceID).Scan(&facts.BellPeriods)

	var timetableID, versionID *uuid.UUID
	var versionNumber *int
	var versionUpdated *time.Time
	if facts.HasTerm {
		err = h.pool.QueryRow(r.Context(), `
			SELECT t.id, tv.id, tv.version_number, tv.status, tv.updated_at
			FROM timetable t
			JOIN timetable_version tv ON tv.timetable_id = t.id AND tv.workspace_id = t.workspace_id
			WHERE t.workspace_id = $1 AND t.academic_term_uuid = $2 AND t.timetable_type = 'LEARNING'
			ORDER BY CASE WHEN tv.status IN ('DRAFT', 'VALIDATING', 'VALIDATED') THEN 0 ELSE 1 END, tv.updated_at DESC, tv.version_number DESC
			LIMIT 1`, session.WorkspaceID, termID).Scan(&timetableID, &versionID, &versionNumber, &facts.VersionStatus, &versionUpdated)
		facts.HasVersion = err == nil && versionID != nil
	}
	if facts.HasVersion {
		_ = h.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM timetable_generation_job WHERE workspace_id = $1 AND timetable_version_id = $2 AND status = 'RUNNING')`, session.WorkspaceID, versionID).Scan(&facts.GenerationRunning)
		err = h.pool.QueryRow(r.Context(), `SELECT hard_conflicts, unscheduled_mandatory_lessons FROM solver_run WHERE workspace_id = $1 AND timetable_version_id = $2 ORDER BY created_at DESC LIMIT 1`, session.WorkspaceID, versionID).Scan(&facts.HardConflicts, &facts.Unscheduled)
		facts.HasSolverRun = err == nil
		var detectedHard int
		_ = h.pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM scheduling_conflict WHERE org_id = $1 AND timetable_version_id = $2 AND resolved = false AND severity = 'HARD'`, session.WorkspaceID, versionID).Scan(&detectedHard)
		if detectedHard > facts.HardConflicts {
			facts.HardConflicts = detectedHard
		}
	}
	state := deriveWorkflowState(facts)
	explanation, action := workflowCopy(state, termName)
	if state == "ASSIGNMENTS_REQUIRED" && !facts.IntegrationReady {
		explanation = "Academic synchronization is pending. Refresh academic data in Scholaroscope if this does not complete shortly."
		action = errorAction{"Review synchronization", "/classes-teachers"}
	} else if state == "ASSIGNMENTS_REQUIRED" && sourceAssignments > 0 {
		explanation = "Teaching assignments exist in Scholaroscope, but none currently meet timetable eligibility rules. Review active teachers, class subjects, and curriculum setup."
		action = errorAction{"Review classes and teachers", "/classes-teachers"}
	}
	blockers := workflowBlockers(state)
	if state == "ASSIGNMENTS_REQUIRED" && !facts.IntegrationReady {
		blockers = []map[string]string{{"code": state, "message": explanation, "action_label": action.Label, "action_target": action.Target}}
	} else if state == "ASSIGNMENTS_REQUIRED" && sourceAssignments > 0 {
		blockers = []map[string]string{{"code": state, "message": explanation, "action_label": action.Label, "action_target": action.Target}}
	}
	assignmentSync := assignmentSynchronizationStatus(facts, sourceAssignments)
	completed := 0
	for _, ok := range []bool{facts.HasTerm, facts.IntegrationReady, facts.Assignments > 0, facts.BellPeriods > 0, facts.HasVersion} {
		if ok {
			completed++
		}
	}
	var selectedPayload map[string]any
	if selected != nil {
		selectedPayload = termPayload(*selected, today)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state": state, "explanation": explanation,
		"blocking_conditions": blockers,
		"recommended_action":  action,
		"secondary_actions":   []errorAction{{"Review calendar", "/exceptions"}, {"Review classes and spaces", "/classes-spaces"}},
		"active_term":         selectedPayload,
		"relevant_timetable":  map[string]any{"timetable_uuid": timetableID, "version_uuid": versionID, "version_number": versionNumber, "status": facts.VersionStatus},
		"progress":            map[string]any{"completed": completed, "total": 5, "assignments": facts.Assignments, "teaching_periods": facts.BellPeriods},
		"synchronization":     map[string]any{"status": assignmentSync, "last_successful_sync_at": lastSync, "integration_health": health, "source_assignment_count": sourceAssignments, "eligible_assignment_count": eligibleAssignments},
		"last_updated_at":     versionUpdated,
	})
}

func workflowBlockers(state string) []map[string]string {
	if state == "READY_TO_GENERATE" || state == "DRAFT_READY_FOR_VALIDATION" || state == "READY_TO_PUBLISH" || state == "PUBLISHED" {
		return []map[string]string{}
	}
	message, action := workflowCopy(state, "the active term")
	return []map[string]string{{"code": state, "message": message, "action_label": action.Label, "action_target": action.Target}}
}
