package portalapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"scholaroscope-temporal-service/internal/launch"
	"scholaroscope-temporal-service/internal/scheduling"

	"github.com/google/uuid"
)

type generatedSlot struct {
	id        uuid.UUID
	day       int
	index     int
	startTime time.Time
	endTime   time.Time
}

func (h *Handler) GenerateVersion(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	versionID, err := uuid.Parse(r.PathValue("versionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_version_uuid")
		return
	}
	var body struct {
		Seed            int64 `json:"seed"`
		TimeBudgetMS    int   `json:"time_budget_ms"`
		IterationBudget int   `json:"iteration_budget"`
		Restarts        int   `json:"restarts"`
		FullCoverage    bool  `json:"full_coverage"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Seed == 0 {
		body.Seed = 1
	}
	if body.TimeBudgetMS <= 0 || body.TimeBudgetMS > 120_000 {
		body.TimeBudgetMS = 30_000
	}
	if body.IterationBudget <= 0 || body.IterationBudget > 20_000_000 {
		body.IterationBudget = 5_000_000
	}
	if body.Restarts <= 0 || body.Restarts > 20 {
		body.Restarts = 5
	}

	problem, slots, timetableID, err := h.loadEngineProblem(r.Context(), session.WorkspaceID, versionID, body.FullCoverage)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	result := scheduling.Solve(problem, scheduling.EngineConfig{Seed: body.Seed, TimeBudget: time.Duration(body.TimeBudgetMS) * time.Millisecond, IterationBudget: body.IterationBudget, Restarts: body.Restarts, MaxConsecutive: 4})
	if err := h.persistSolveResult(r.Context(), session.WorkspaceID, timetableID, versionID, slots, result); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	status := http.StatusOK
	if result.Status == scheduling.StatusInfeasible {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, result)
}

func (h *Handler) loadEngineProblem(ctx context.Context, workspaceID, versionID uuid.UUID, fullCoverage bool) (scheduling.EngineProblem, map[string]generatedSlot, uuid.UUID, error) {
	var timetableID, termID, calendarID uuid.UUID
	var academicYearID *uuid.UUID
	var versionStatus, timetableType string
	err := h.pool.QueryRow(ctx, `
		SELECT tv.timetable_id, tv.status, t.timetable_type, t.academic_term_uuid,
		       t.calendar_id, term.academic_year_uuid
		FROM timetable_version tv
		JOIN timetable t ON t.id = tv.timetable_id AND t.workspace_id = tv.workspace_id
		JOIN external_academic_term term ON term.id = t.academic_term_uuid AND term.workspace_id = tv.workspace_id
		WHERE tv.id = $1 AND tv.workspace_id = $2`, versionID, workspaceID,
	).Scan(&timetableID, &versionStatus, &timetableType, &termID, &calendarID, &academicYearID)
	if err != nil {
		return scheduling.EngineProblem{}, nil, uuid.Nil, errCode("version_not_found")
	}
	if versionStatus == "PUBLISHED" || versionStatus == "SUPERSEDED" {
		return scheduling.EngineProblem{}, nil, uuid.Nil, errCode("published_version_is_immutable")
	}
	if timetableType != "LEARNING" {
		return scheduling.EngineProblem{}, nil, uuid.Nil, errCode("examination_scheduling_feature_gated")
	}
	if academicYearID == nil {
		return scheduling.EngineProblem{}, nil, uuid.Nil, errCode("academic_year_projection_required")
	}

	periodRows, err := h.pool.Query(ctx, `
		SELECT id, day_of_week, slot_index, start_time, end_time
		FROM time_slot
		WHERE org_id = $1 AND calendar_version_id = $2 AND slot_type = 'LESSON'
		ORDER BY day_of_week, slot_index`, workspaceID, calendarID)
	if err != nil {
		return scheduling.EngineProblem{}, nil, uuid.Nil, err
	}
	defer periodRows.Close()
	periods := []scheduling.EnginePeriod{}
	slots := map[string]generatedSlot{}
	for periodRows.Next() {
		var slot generatedSlot
		if err := periodRows.Scan(&slot.id, &slot.day, &slot.index, &slot.startTime, &slot.endTime); err != nil {
			return scheduling.EngineProblem{}, nil, uuid.Nil, err
		}
		id := slot.id.String()
		slots[id] = slot
		periods = append(periods, scheduling.EnginePeriod{ID: id, Day: slot.day, Index: slot.index, Teaching: true, Mandatory: true})
	}
	if len(periods) == 0 {
		return scheduling.EngineProblem{}, nil, uuid.Nil, errCode("active_calendar_has_no_teaching_periods")
	}

	problem := scheduling.EngineProblem{WorkspaceID: workspaceID.String(), AcademicYearID: academicYearID.String(), TermID: termID.String(), Periods: periods, Teachers: map[string]scheduling.EngineTeacher{}, Cohorts: map[string]scheduling.EngineCohort{}, Resources: map[string]scheduling.EngineResource{}, Registrations: map[string]map[string]bool{}, FullCoverage: fullCoverage}
	assignmentRows, err := h.pool.Query(ctx, `
		SELECT assignment.id, assignment.teacher_uuid, assignment.cohort_uuid,
		       assignment.cohort_subject_uuid, assignment.subject_uuid,
		       assignment.scheduling_requirements
		FROM external_teaching_assignment assignment
		JOIN external_actor actor ON actor.id = assignment.teacher_uuid
		 AND actor.workspace_id = assignment.workspace_id AND actor.status = 'ACTIVE'
		JOIN external_actor_role role ON role.actor_id = actor.id
		 AND role.workspace_id = actor.workspace_id AND role.actor_kind = 'TEACHER' AND role.status = 'ACTIVE'
		JOIN external_cohort cohort ON cohort.id = assignment.cohort_uuid
		 AND cohort.workspace_id = assignment.workspace_id AND cohort.status = 'ACTIVE'
		JOIN external_cohort_subject cohort_subject ON cohort_subject.id = assignment.cohort_subject_uuid
		 AND cohort_subject.workspace_id = assignment.workspace_id AND cohort_subject.status = 'ACTIVE'
		WHERE assignment.workspace_id = $1 AND assignment.status = 'ACTIVE'
		  AND cohort.academic_year_uuid = $2`, workspaceID, academicYearID)
	if err != nil {
		return scheduling.EngineProblem{}, nil, uuid.Nil, err
	}
	defer assignmentRows.Close()
	for assignmentRows.Next() {
		var assignmentID, teacherID, cohortID, cohortSubjectID, subjectID uuid.UUID
		var requirementsJSON []byte
		if err := assignmentRows.Scan(&assignmentID, &teacherID, &cohortID, &cohortSubjectID, &subjectID, &requirementsJSON); err != nil {
			return scheduling.EngineProblem{}, nil, uuid.Nil, err
		}
		requirements := map[string]any{}
		_ = json.Unmarshal(requirementsJSON, &requirements)
		weekly := intRequirement(requirements, "weekly_lesson_requirement", 1)
		doubles := intRequirement(requirements, "required_double_lessons", 0)
		teacherKey, cohortKey, subjectKey, cohortSubjectKey := teacherID.String(), cohortID.String(), subjectID.String(), cohortSubjectID.String()
		teacher := problem.Teachers[teacherKey]
		if teacher.ID == "" {
			teacher = scheduling.EngineTeacher{ID: teacherKey, WorkspaceID: problem.WorkspaceID, WorkloadLimit: len(periods), Unavailable: map[string]bool{}, Preferred: map[string]bool{}, QualifiedSubjects: map[string]bool{}}
		}
		teacher.QualifiedSubjects[subjectKey] = true
		if limit := intRequirement(requirements, "teacher_weekly_workload_limit", 0); limit > 0 && limit < teacher.WorkloadLimit {
			teacher.WorkloadLimit = limit
		}
		problem.Teachers[teacherKey] = teacher
		if _, exists := problem.Cohorts[cohortKey]; !exists {
			problem.Cohorts[cohortKey] = scheduling.EngineCohort{ID: cohortKey, WorkspaceID: problem.WorkspaceID, Unavailable: map[string]bool{}}
		}
		if problem.Registrations[cohortKey] == nil {
			problem.Registrations[cohortKey] = map[string]bool{}
		}
		problem.Registrations[cohortKey][cohortSubjectKey] = true
		resourceID, _ := requirements["required_resource_uuid"].(string)
		problem.Assignments = append(problem.Assignments, scheduling.EngineAssignment{ID: assignmentID.String(), WorkspaceID: problem.WorkspaceID, AcademicYearID: problem.AcademicYearID, TermID: problem.TermID, TeacherID: teacherKey, CohortID: cohortKey, CohortSubjectID: cohortSubjectKey, SubjectID: subjectKey, ResourceID: resourceID, WeeklyPeriods: weekly, DoubleBlocks: doubles, Mandatory: true, Active: true})
	}
	if len(problem.Assignments) == 0 {
		return scheduling.EngineProblem{}, nil, uuid.Nil, errCode("missing_teaching_assignments_sync")
	}

	availabilityRows, err := h.pool.Query(ctx, `SELECT teacher_id, timeslot_id FROM teacher_availability WHERE org_id = $1 AND is_available = false`, workspaceID)
	if err == nil {
		defer availabilityRows.Close()
		for availabilityRows.Next() {
			var teacherID, timeslotID uuid.UUID
			if availabilityRows.Scan(&teacherID, &timeslotID) == nil {
				teacher := problem.Teachers[teacherID.String()]
				if teacher.ID != "" {
					teacher.Unavailable[timeslotID.String()] = true
					problem.Teachers[teacher.ID] = teacher
				}
			}
		}
	}
	for _, assignment := range problem.Assignments {
		if assignment.ResourceID == "" {
			continue
		}
		resourceID, parseErr := uuid.Parse(assignment.ResourceID)
		if parseErr != nil {
			return scheduling.EngineProblem{}, nil, uuid.Nil, errCode("invalid_required_resource")
		}
		var status string
		if queryErr := h.pool.QueryRow(ctx, `SELECT status FROM resource WHERE id = $1 AND workspace_id = $2`, resourceID, workspaceID).Scan(&status); queryErr != nil || status != "ACTIVE" {
			return scheduling.EngineProblem{}, nil, uuid.Nil, errCode("required_resource_unavailable")
		}
		problem.Resources[assignment.ResourceID] = scheduling.EngineResource{ID: assignment.ResourceID, WorkspaceID: problem.WorkspaceID, Capacity: 1, Unavailable: map[string]bool{}}
	}
	return problem, slots, timetableID, nil
}

func (h *Handler) persistSolveResult(ctx context.Context, workspaceID, timetableID, versionID uuid.UUID, slots map[string]generatedSlot, result scheduling.SolveResult) error {
	feasibilityJSON, _ := json.Marshal(result.Feasibility)
	validationJSON, _ := json.Marshal(result.Validation)
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if result.Status != scheduling.StatusInfeasible {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM timetable_version WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, versionID, workspaceID).Scan(&status); err != nil {
			return err
		}
		if status == "PUBLISHED" || status == "SUPERSEDED" {
			return errCode("published_version_is_immutable")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM timetable_entry WHERE workspace_id = $1 AND timetable_version_id = $2`, workspaceID, versionID); err != nil {
			return err
		}
		placements := append([]scheduling.EnginePlacement(nil), result.Placements...)
		sort.Slice(placements, func(i, j int) bool {
			if placements[i].AssignmentID != placements[j].AssignmentID {
				return placements[i].AssignmentID < placements[j].AssignmentID
			}
			a, b := slots[placements[i].PeriodIDs[0]], slots[placements[j].PeriodIDs[0]]
			if a.day != b.day {
				return a.day < b.day
			}
			return a.index < b.index
		})
		occurrence := map[string]int{}
		for _, placement := range placements {
			first, last := slots[placement.PeriodIDs[0]], slots[placement.PeriodIDs[len(placement.PeriodIDs)-1]]
			occurrence[placement.AssignmentID]++
			stable := uuid.NewSHA1(timetableID, []byte(placement.AssignmentID+"|"+strconv.Itoa(occurrence[placement.AssignmentID])))
			_, err := tx.Exec(ctx, `
				INSERT INTO timetable_entry (id, workspace_id, timetable_version_id, stable_entry_uuid,
				 entry_kind, teacher_uuid, cohort_uuid, subject_uuid, cohort_subject_uuid, resource_id,
				 day_of_week, start_period_index, duration_periods, start_time, end_time)
				VALUES ($1, $2, $3, $4, 'LEARNING', $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
				uuid.New(), workspaceID, versionID, stable, placement.TeacherID, placement.CohortID,
				placement.SubjectID, placement.CohortSubjectID, nullableString(placement.ResourceID),
				first.day, first.index, len(placement.PeriodIDs), first.startTime, last.endTime)
			if err != nil {
				return errCode("generated_entry_occupancy_conflict")
			}
		}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO solver_run (id, workspace_id, timetable_version_id, status, seed,
		 preflight_duration_ns, solve_duration_ns, iterations, restarts, scheduled_periods,
		 unscheduled_mandatory_lessons, hard_conflicts, soft_violations, feasibility, validation)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15::jsonb)`,
		uuid.New(), workspaceID, versionID, result.Status, result.Metrics.Seed,
		result.Metrics.PreflightDuration.Nanoseconds(), result.Metrics.SolveDuration.Nanoseconds(),
		result.Metrics.Iterations, result.Metrics.Restarts, result.Metrics.ScheduledPeriods,
		result.Validation.Unscheduled, result.Validation.HardConflictCount,
		result.Validation.SoftViolationCount, string(feasibilityJSON), string(validationJSON))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func intRequirement(requirements map[string]any, key string, fallback int) int {
	value, exists := requirements[key]
	if !exists {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		if typed >= 0 {
			return int(typed)
		}
	case int:
		if typed >= 0 {
			return typed
		}
	}
	return fallback
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
