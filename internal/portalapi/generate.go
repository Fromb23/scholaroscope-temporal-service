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
	"github.com/jackc/pgx/v5/pgtype"
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
	if !h.versionTermIsSchedulable(r.Context(), session.WorkspaceID, versionID, session.WorkspaceTimezone) {
		writeError(w, http.StatusConflict, "term_not_schedulable")
		return
	}
	jobID, err := h.startGenerationJob(r.Context(), session.WorkspaceID, versionID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	jobStatus := "FAILED"
	jobError := "generation_failed"
	defer func() {
		_, _ = h.pool.Exec(context.Background(), `
			UPDATE timetable_generation_job
			SET status = $1, error_code = NULLIF($2, ''), finished_at = now()
			WHERE id = $3 AND workspace_id = $4`, jobStatus, jobError, jobID, session.WorkspaceID)
	}()
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
		jobError = "problem_load_failed"
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	result := scheduling.Solve(problem, scheduling.EngineConfig{Seed: body.Seed, TimeBudget: time.Duration(body.TimeBudgetMS) * time.Millisecond, IterationBudget: body.IterationBudget, Restarts: body.Restarts, MaxConsecutive: 4})
	if err := h.persistSolveResult(r.Context(), session.WorkspaceID, timetableID, versionID, slots, result); err != nil {
		jobError = "result_persistence_failed"
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	jobStatus = "COMPLETED"
	jobError = ""
	status := http.StatusOK
	if result.Status == scheduling.StatusInfeasible {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, result)
}

func (h *Handler) startGenerationJob(ctx context.Context, workspaceID, versionID uuid.UUID) (uuid.UUID, error) {
	var versionExists bool
	if err := h.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM timetable_version WHERE id = $1 AND workspace_id = $2)`, versionID, workspaceID).Scan(&versionExists); err != nil || !versionExists {
		return uuid.Nil, errCode("version_not_found")
	}
	_, _ = h.pool.Exec(ctx, `
		UPDATE timetable_generation_job
		SET status = 'FAILED', error_code = 'generation_interrupted', finished_at = now()
		WHERE workspace_id = $1 AND timetable_version_id = $2
		  AND status = 'RUNNING' AND started_at < now() - interval '10 minutes'`, workspaceID, versionID)
	jobID := uuid.New()
	err := h.pool.QueryRow(ctx, `
		INSERT INTO timetable_generation_job (id, workspace_id, timetable_version_id, status)
		VALUES ($1, $2, $3, 'RUNNING')
		ON CONFLICT (workspace_id, timetable_version_id) WHERE status = 'RUNNING' DO NOTHING
		RETURNING id`, jobID, workspaceID, versionID).Scan(&jobID)
	if err != nil {
		return uuid.Nil, errCode("generation_already_running")
	}
	return jobID, err
}

func (h *Handler) loadEngineProblem(ctx context.Context, workspaceID, versionID uuid.UUID, fullCoverage bool) (scheduling.EngineProblem, map[string]generatedSlot, uuid.UUID, error) {
	var timetableID, termID, calendarID uuid.UUID
	var academicYearID *uuid.UUID
	var versionStatus, timetableType string
	var effectiveStart, effectiveEnd time.Time
	err := h.pool.QueryRow(ctx, `
		SELECT tv.timetable_id, tv.status, t.timetable_type, t.academic_term_uuid,
		       t.calendar_id, term.academic_year_uuid, tv.effective_start, tv.effective_end
		FROM timetable_version tv
		JOIN timetable t ON t.id = tv.timetable_id AND t.workspace_id = tv.workspace_id
		JOIN external_academic_term term ON term.id = t.academic_term_uuid AND term.workspace_id = tv.workspace_id
		WHERE tv.id = $1 AND tv.workspace_id = $2`, versionID, workspaceID,
	).Scan(&timetableID, &versionStatus, &timetableType, &termID, &calendarID, &academicYearID, &effectiveStart, &effectiveEnd)
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

	problem := scheduling.EngineProblem{WorkspaceID: workspaceID.String(), AcademicYearID: academicYearID.String(), TermID: termID.String(), Periods: periods, Teachers: map[string]scheduling.EngineTeacher{}, Cohorts: map[string]scheduling.EngineCohort{}, Learners: map[string]scheduling.EngineLearner{}, Resources: map[string]scheduling.EngineResource{}, Registrations: map[string]map[string]bool{}, FullCoverage: fullCoverage}
	exceptions, err := h.applicableCalendarExceptions(ctx, workspaceID, *academicYearID, termID, effectiveStart, effectiveEnd)
	if err != nil {
		return scheduling.EngineProblem{}, nil, uuid.Nil, err
	}
	for _, item := range exceptions {
		problem.CalendarExceptions = append(problem.CalendarExceptions, scheduling.EngineCalendarException{
			ID: item.ID.String(), WorkspaceID: workspaceID.String(), AcademicYearID: academicYearID.String(),
			TermID: termID.String(), Kind: item.Kind, StartsOn: item.StartDate.Format("2006-01-02"),
			EndsOn: item.EndDate.Format("2006-01-02"), BlocksLearning: item.AffectsLearning,
		})
	}
	assignmentRows, err := h.pool.Query(ctx, `
		SELECT assignment.id, assignment.teacher_uuid, assignment.cohort_uuid,
		       assignment.cohort_subject_uuid, assignment.subject_uuid,
		       assignment.scheduling_requirements,
		       override.weekly_lesson_requirement,
		       override.required_double_lessons
		FROM external_teaching_assignment assignment
		JOIN external_actor actor ON actor.id = assignment.teacher_uuid
		 AND actor.workspace_id = assignment.workspace_id AND actor.status = 'ACTIVE'
		JOIN external_actor_role role ON role.actor_id = actor.id
		 AND role.workspace_id = actor.workspace_id AND role.actor_kind = 'TEACHER' AND role.status = 'ACTIVE'
		JOIN external_cohort cohort ON cohort.id = assignment.cohort_uuid
		 AND cohort.workspace_id = assignment.workspace_id AND cohort.status = 'ACTIVE'
		JOIN external_cohort_subject cohort_subject ON cohort_subject.id = assignment.cohort_subject_uuid
		 AND cohort_subject.workspace_id = assignment.workspace_id AND cohort_subject.status = 'ACTIVE'
		LEFT JOIN timetable_demand_override override
		  ON override.workspace_id = assignment.workspace_id
		 AND override.academic_term_uuid = $3
		 AND override.teaching_assignment_uuid = assignment.id
		WHERE assignment.workspace_id = $1 AND assignment.status = 'ACTIVE'
		  AND cohort.academic_year_uuid = $2`, workspaceID, academicYearID, termID)
	if err != nil {
		return scheduling.EngineProblem{}, nil, uuid.Nil, err
	}
	defer assignmentRows.Close()
	for assignmentRows.Next() {
		var assignmentID, teacherID, cohortID, cohortSubjectID, subjectID uuid.UUID
		var requirementsJSON []byte
		var overrideWeekly, overrideDoubles pgtype.Int4
		if err := assignmentRows.Scan(&assignmentID, &teacherID, &cohortID, &cohortSubjectID, &subjectID, &requirementsJSON, &overrideWeekly, &overrideDoubles); err != nil {
			return scheduling.EngineProblem{}, nil, uuid.Nil, err
		}
		requirements := map[string]any{}
		_ = json.Unmarshal(requirementsJSON, &requirements)
		weekly, configured := intRequirementOptional(requirements, "weekly_lesson_requirement")
		doubles := intRequirement(requirements, "required_double_lessons", 0)
		if overrideWeekly.Valid {
			weekly = int(overrideWeekly.Int32)
			configured = true
			doubles = 0
			if overrideDoubles.Valid {
				doubles = int(overrideDoubles.Int32)
			}
		}
		if !configured {
			return scheduling.EngineProblem{}, nil, uuid.Nil, errCode("teaching_demand_unconfigured")
		}
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

	learnerRows, err := h.pool.Query(ctx, `
		SELECT learner.id, membership.cohort_uuid
		FROM external_learner learner
		JOIN external_learner_cohort_membership membership
		  ON membership.workspace_id = learner.workspace_id
		 AND membership.learner_uuid = learner.id
		 AND membership.status = 'ACTIVE'
		JOIN external_cohort cohort
		  ON cohort.workspace_id = membership.workspace_id
		 AND cohort.id = membership.cohort_uuid
		 AND cohort.status = 'ACTIVE'
		WHERE learner.workspace_id = $1
		  AND learner.status = 'ACTIVE'
		  AND cohort.academic_year_uuid = $2`, workspaceID, academicYearID)
	if err == nil {
		defer learnerRows.Close()
		for learnerRows.Next() {
			var learnerID, cohortID uuid.UUID
			if learnerRows.Scan(&learnerID, &cohortID) == nil {
				problem.Learners[learnerID.String()] = scheduling.EngineLearner{
					ID: learnerID.String(), WorkspaceID: problem.WorkspaceID,
					CohortID: cohortID.String(), Active: true,
				}
			}
		}
	}
	groupRows, err := h.pool.Query(ctx, `
		SELECT dg.id, dg.name, dg.teacher_uuid, dg.subject_uuid,
		       dg.weekly_lesson_requirement, dg.required_double_lessons,
		       COALESCE(jsonb_agg(DISTINCT dga.teaching_assignment_uuid::text)
		         FILTER (WHERE dga.teaching_assignment_uuid IS NOT NULL), '[]'::jsonb),
		       COALESCE(jsonb_agg(DISTINCT eta.cohort_uuid::text)
		         FILTER (WHERE eta.cohort_uuid IS NOT NULL), '[]'::jsonb),
		       COALESCE(jsonb_agg(DISTINCT dgl.learner_uuid::text)
		         FILTER (WHERE dgl.learner_uuid IS NOT NULL), '[]'::jsonb)
		FROM timetable_delivery_group dg
		LEFT JOIN timetable_delivery_group_assignment dga
		  ON dga.workspace_id = dg.workspace_id AND dga.delivery_group_id = dg.id
		LEFT JOIN external_teaching_assignment eta
		  ON eta.workspace_id = dga.workspace_id AND eta.id = dga.teaching_assignment_uuid
		LEFT JOIN timetable_delivery_group_learner dgl
		  ON dgl.workspace_id = dg.workspace_id AND dgl.delivery_group_id = dg.id
		WHERE dg.workspace_id = $1
		  AND dg.academic_year_uuid = $2
		  AND dg.academic_term_uuid = $3
		  AND dg.lifecycle_status = 'ACTIVE'
		GROUP BY dg.id, dg.name, dg.teacher_uuid, dg.subject_uuid,
		         dg.weekly_lesson_requirement, dg.required_double_lessons
		ORDER BY dg.name, dg.id`, workspaceID, academicYearID, termID)
	if err != nil {
		return scheduling.EngineProblem{}, nil, uuid.Nil, err
	}
	defer groupRows.Close()
	for groupRows.Next() {
		var groupID, teacherID, subjectID uuid.UUID
		var name string
		var weekly, doubles int
		var assignmentJSON, cohortJSON, learnerJSON []byte
		if err := groupRows.Scan(&groupID, &name, &teacherID, &subjectID, &weekly, &doubles, &assignmentJSON, &cohortJSON, &learnerJSON); err != nil {
			return scheduling.EngineProblem{}, nil, uuid.Nil, err
		}
		var assignmentIDs, cohortIDs, learnerIDs []string
		_ = json.Unmarshal(assignmentJSON, &assignmentIDs)
		_ = json.Unmarshal(cohortJSON, &cohortIDs)
		_ = json.Unmarshal(learnerJSON, &learnerIDs)
		problem.DeliveryGroups = append(problem.DeliveryGroups, scheduling.EngineDeliveryGroup{
			ID: groupID.String(), WorkspaceID: problem.WorkspaceID,
			AcademicYearID: problem.AcademicYearID, TermID: problem.TermID,
			Name: name, TeacherID: teacherID.String(), SubjectID: subjectID.String(),
			AssignmentIDs: assignmentIDs, CohortIDs: cohortIDs, LearnerIDs: learnerIDs,
			WeeklyPeriods: weekly, DoubleBlocks: doubles, Mandatory: true, Active: true,
		})
	}
	blockRows, err := h.pool.Query(ctx, `
		SELECT block.id,
		       COALESCE(jsonb_agg(member.delivery_group_id::text ORDER BY member.delivery_group_id::text)
		         FILTER (WHERE member.delivery_group_id IS NOT NULL), '[]'::jsonb)
		FROM timetable_parallel_block block
		LEFT JOIN timetable_parallel_block_member member
		  ON member.workspace_id = block.workspace_id AND member.parallel_block_id = block.id
		WHERE block.workspace_id = $1
		  AND block.academic_year_uuid = $2
		  AND block.academic_term_uuid = $3
		  AND block.lifecycle_status = 'ACTIVE'
		GROUP BY block.id`, workspaceID, academicYearID, termID)
	if err != nil {
		return scheduling.EngineProblem{}, nil, uuid.Nil, err
	}
	defer blockRows.Close()
	for blockRows.Next() {
		var blockID uuid.UUID
		var groupJSON []byte
		if err := blockRows.Scan(&blockID, &groupJSON); err != nil {
			return scheduling.EngineProblem{}, nil, uuid.Nil, err
		}
		var groupIDs []string
		_ = json.Unmarshal(groupJSON, &groupIDs)
		problem.ParallelBlocks = append(problem.ParallelBlocks, scheduling.EngineParallelBlock{ID: blockID.String(), WorkspaceID: problem.WorkspaceID, GroupIDs: groupIDs, Active: true})
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
			assignmentIDs := placementAssignmentIDs(placement)
			cohortIDs := placementCohortIDs(placement)
			stableKey := placement.AssignmentID
			if placement.DeliveryGroupID != "" {
				stableKey = "group:" + placement.DeliveryGroupID
			}
			occurrence[stableKey]++
			stable := uuid.NewSHA1(timetableID, []byte(stableKey+"|"+strconv.Itoa(occurrence[stableKey])))
			cohortScalar := nullableString(placement.CohortID)
			if placement.DeliveryGroupID != "" && len(cohortIDs) > 1 {
				cohortScalar = nil
			}
			entryID := uuid.New()
			_, err := tx.Exec(ctx, `
				INSERT INTO timetable_entry (id, workspace_id, timetable_version_id, stable_entry_uuid,
				 entry_kind, teacher_uuid, cohort_uuid, subject_uuid, cohort_subject_uuid, resource_id,
				 day_of_week, start_period_index, duration_periods, start_time, end_time,
				 delivery_group_uuid, parallel_block_uuid, learner_count,
				 cohort_uuids, cohort_subject_uuids, teaching_assignment_uuids)
				VALUES ($1, $2, $3, $4, 'LEARNING', $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
				        $15, $16, $17,
				        ARRAY(SELECT unnest($18::text[])::uuid),
				        ARRAY(
				          SELECT DISTINCT eta.cohort_subject_uuid
				          FROM external_teaching_assignment eta
				          WHERE eta.workspace_id = $2
				            AND eta.id = ANY(ARRAY(SELECT unnest($19::text[])::uuid))
				          ORDER BY eta.cohort_subject_uuid
				        ),
				        ARRAY(SELECT unnest($19::text[])::uuid))`,
				entryID, workspaceID, versionID, stable, placement.TeacherID, cohortScalar,
				placement.SubjectID, nullableString(placement.CohortSubjectID), nullableString(placement.ResourceID),
				first.day, first.index, len(placement.PeriodIDs), first.startTime, last.endTime,
				nullableString(placement.DeliveryGroupID), nullableString(placement.ParallelBlockID),
				len(placement.LearnerIDs), cohortIDs, assignmentIDs)
			if err != nil {
				return errCode("generated_entry_occupancy_conflict")
			}
			if len(placement.LearnerIDs) > 0 {
				_, err = tx.Exec(ctx, `
					INSERT INTO timetable_entry_learner_occupancy (
						timetable_entry_id, workspace_id, timetable_version_id,
						day_of_week, period_index, learner_uuid
					)
					SELECT $1, $2, $3, $4, period_index, learner_uuid
					FROM generate_series($5::integer, $5::integer + $6::integer - 1) AS period_index
					CROSS JOIN LATERAL (
						SELECT unnest($7::text[])::uuid AS learner_uuid
					) learners`,
					entryID, workspaceID, versionID, first.day, first.index, len(placement.PeriodIDs), compactStrings(placement.LearnerIDs),
				)
				if err != nil {
					return errCode("generated_entry_occupancy_conflict")
				}
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
	if _, err = tx.Exec(ctx, `
		UPDATE timetable_version version
		SET status = 'DRAFT',
		    validation_summary = $1::jsonb,
		    generator_version = 'hybrid-v1',
		    academic_snapshot_hash = workspace.academic_snapshot_hash,
		    updated_at = now()
		FROM external_workspace workspace
		WHERE version.id = $2
		  AND version.workspace_id = $3
		  AND workspace.id = version.workspace_id`, string(validationJSON), versionID, workspaceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func intRequirement(requirements map[string]any, key string, fallback int) int {
	if value, ok := intRequirementOptional(requirements, key); ok {
		return value
	}
	return fallback
}

func intRequirementOptional(requirements map[string]any, key string) (int, bool) {
	value, exists := requirements[key]
	if !exists {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		if typed >= 0 {
			return int(typed), true
		}
	case int:
		if typed >= 0 {
			return typed, true
		}
	}
	return 0, false
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func placementAssignmentIDs(placement scheduling.EnginePlacement) []string {
	if len(placement.AssignmentIDs) > 0 {
		return compactStrings(placement.AssignmentIDs)
	}
	if strings.TrimSpace(placement.AssignmentID) != "" {
		return []string{placement.AssignmentID}
	}
	return []string{}
}

func placementCohortIDs(placement scheduling.EnginePlacement) []string {
	if len(placement.CohortIDs) > 0 {
		return compactStrings(placement.CohortIDs)
	}
	if strings.TrimSpace(placement.CohortID) != "" {
		return []string{placement.CohortID}
	}
	return []string{}
}

func compactStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
