package portalapi

import (
	"fmt"
	"net/http"
	"strings"
)

type errorAction struct {
	Label  string `json:"label"`
	Target string `json:"target"`
}

type domainError struct {
	Type    string         `json:"type"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
	Action  *errorAction   `json:"action,omitempty"`
}

type errorDefinition struct {
	code    string
	message string
	action  *errorAction
}

var domainErrors = map[string]errorDefinition{
	"active_academic_year_not_found":          {"academic_year_required", "This workspace does not have an active academic year. Set one up in Scholaroscope to continue.", &errorAction{"Review synchronization", "/classes-teachers"}},
	"academic_year_projection_required":       {"academic_year_required", "This workspace does not have synchronized academic-year information. Refresh academic data in Scholaroscope to continue.", &errorAction{"Review classes and teachers", "/classes-teachers"}},
	"active_term_not_found":                   {"active_term_required", "No active term is available for this workspace. Create or activate a term in Scholaroscope.", &errorAction{"Review synchronization", "/classes-teachers"}},
	"term_not_found":                          {"term_not_found", "The selected term is not available in this workspace.", &errorAction{"Choose another term", "/timetable"}},
	"term_not_schedulable":                    {"term_not_schedulable", "The selected term is not eligible for timetable scheduling.", &errorAction{"Choose an active term", "/timetable"}},
	"effective_dates_outside_active_term":     {"term_date_mismatch", "The timetable dates must stay within the selected academic term.", &errorAction{"Review academic context", "/timetable"}},
	"active_calendar_required":                {"bell_periods_required", "Your school day has not been configured. Add teaching periods and breaks to generate a timetable.", &errorAction{"Set up school day", "/school-day"}},
	"active_calendar_has_no_teaching_periods": {"bell_periods_required", "Your school day has no teaching periods available for scheduling.", &errorAction{"Set up school day", "/school-day"}},
	"missing_teaching_assignments_sync":       {"teaching_assignments_required", "No teaching assignments have been synchronized yet. Assign teachers to class subjects in Scholaroscope, or refresh this workspace.", &errorAction{"Review classes and teachers", "/classes-teachers"}},
	"teaching_demand_unconfigured":            {"teaching_demand_unconfigured", "One or more teaching assignments do not have a weekly lesson requirement. Configure timetable demand before generation.", &errorAction{"Configure demand", "/classes-teachers"}},
	"complete_solver_validation_required":     {"draft_validation_required", "Validate the timetable and resolve any highlighted issues before publishing.", &errorAction{"Validate timetable", "/timetable"}},
	"draft_regeneration_required":             {"draft_regeneration_required", "This draft was created before the current scheduling checks or changed after generation. Regenerate it before validation.", &errorAction{"Regenerate timetable", "/timetable"}},
	"academic_data_stale":                     {"academic_data_stale", "Academic data changed after this timetable was generated. Regenerate it before validation or publication.", &errorAction{"Regenerate timetable", "/timetable"}},
	"incomplete_or_conflicting_schedule":      {"draft_incomplete", "This timetable still has unscheduled lessons or unresolved scheduling issues.", &errorAction{"Review timetable", "/timetable"}},
	"hard_conflicts_block_publication":        {"hard_conflicts_block_publication", "Resolve all highlighted hard conflicts before publishing this timetable.", &errorAction{"Review conflicts", "/timetable"}},
	"version_not_publishable":                 {"draft_not_publishable", "This timetable version is not available for publication.", &errorAction{"Open current timetable", "/timetable"}},
	"published_version_is_immutable":          {"published_timetable_immutable", "Published timetables cannot be edited. Create a revision to make changes.", &errorAction{"Create revision", "/timetable"}},
	"generation_already_running":              {"generation_already_running", "Timetable generation is already running for this draft.", &errorAction{"View generation progress", "/timetable"}},
	"generated_entry_occupancy_conflict":      {"generated_schedule_conflict", "The generated timetable contains an occupancy conflict and remains an unpublished draft.", &errorAction{"Review conflicts", "/timetable"}},
	"reference_scope_mismatch":                {"selection_scope_mismatch", "One of the selected teachers, classes, subjects, or spaces does not belong to this workspace.", &errorAction{"Review selection", "/timetable"}},
	"room_in_use":                             {"space_in_use", "This space is used by timetable history and cannot be deleted. Deactivate it instead.", &errorAction{"Deactivate space", "/classes-spaces"}},
	"room_not_found":                          {"space_not_found", "The selected physical space is not available in this workspace.", &errorAction{"Review spaces", "/classes-spaces"}},
	"room_save_failed":                        {"space_save_failed", "The physical space could not be saved. Check its name and capacity.", &errorAction{"Review space", "/classes-spaces"}},
	"exclusive_room_already_assigned":         {"exclusive_space_already_assigned", "This exclusive physical space is already assigned to another class. Choose a different space or make it shared.", &errorAction{"Review spaces", "/classes-spaces"}},
	"invalid_double_demand":                   {"invalid_double_demand", "A double lesson uses two periods. Reduce the number of double lessons or increase weekly periods.", &errorAction{"Review classes and teachers", "/classes-teachers"}},
	"delivery_group_teacher_mismatch":         {"combined_lesson_not_authorized", "Combined classes for this lesson must have the same authorized teacher and subject. Correct the teaching assignments in Scholaroscope first.", &errorAction{"Review classes and teachers", "/classes-teachers"}},
	"delivery_group_has_no_learners":          {"combined_lesson_has_no_learners", "No learners are currently synchronized for this subject selection. Refresh Scholaroscope learner participation before combining classes.", &errorAction{"Review classes and teachers", "/classes-teachers"}},
	"learner_audience_overlap":                {"learner_audience_overlap", "One learner appears in more than one alternative group. Correct the subject selection in Scholaroscope or edit the groups.", &errorAction{"Review classes and teachers", "/classes-teachers"}},
	"parallel_block_too_small":                {"parallel_block_too_small", "Run-at-the-same-time alternatives need at least two learner groups.", &errorAction{"Review classes and teachers", "/classes-teachers"}},
	"parallel_block_atomic_move_required":     {"parallel_block_atomic_move_required", "This lesson runs with alternatives. Move the complete block together or choose another lesson.", &errorAction{"Review timetable", "/timetable"}},
	"examination_scheduling_feature_gated":    {"examination_timetables_unavailable", "Examination timetable management is not available in this release.", nil},
	"version_not_found":                       {"draft_not_found", "The selected timetable draft is not available in this workspace.", &errorAction{"Open current timetable", "/timetable"}},
	"version_not_editable":                    {"draft_not_editable", "This timetable version is read-only.", &errorAction{"Open current timetable", "/timetable"}},
}

func errorContract(status int, sourceCode string) domainError {
	if definition, ok := domainErrors[sourceCode]; ok {
		return domainError{Type: "business_rule", Code: definition.code, Message: definition.message, Details: map[string]any{}, Action: definition.action}
	}
	if strings.HasPrefix(sourceCode, "invalid_") {
		return domainError{Type: "validation", Code: "invalid_selection", Message: "Review the selected timetable values and try again.", Details: map[string]any{}}
	}
	if status == http.StatusForbidden || status == http.StatusUnauthorized {
		return domainError{Type: "permission_denied", Code: "permission_denied", Message: "You do not have permission to perform this timetable action.", Details: map[string]any{}}
	}
	if status >= http.StatusInternalServerError || strings.Contains(sourceCode, "_query_failed") || strings.Contains(sourceCode, "_scan_failed") || strings.Contains(sourceCode, "_persistence_failed") {
		return domainError{Type: "internal", Code: "timetable_update_failed", Message: "Something went wrong while updating the timetable. Please try again.", Details: map[string]any{}, Action: &errorAction{"Try again", "/timetable"}}
	}
	return domainError{Type: "business_rule", Code: "timetable_action_blocked", Message: "The timetable action could not be completed. Review the current timetable and try again.", Details: map[string]any{}, Action: &errorAction{"Review timetable", "/timetable"}}
}

func writeDomainError(w http.ResponseWriter, status int, sourceCode string, details map[string]any) {
	contract := errorContract(status, sourceCode)
	if details != nil {
		contract.Details = details
	}
	writeJSON(w, status, map[string]any{"error": contract})
}

func (e domainError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
