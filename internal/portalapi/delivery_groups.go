package portalapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"scholaroscope-temporal-service/internal/launch"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type deliveryGroupRequest struct {
	Name                  string   `json:"name"`
	AssignmentUUIDs       []string `json:"assignment_uuids"`
	SharedPeriodsPerCycle int      `json:"shared_periods_per_cycle"`
	RequiredDoubleLessons int      `json:"required_double_lessons"`
}

func (h *Handler) DeliveryGroups(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	if r.Method == http.MethodPost {
		h.createDeliveryGroup(w, r, session)
		return
	}
	today, _ := time.Parse("2006-01-02", workspaceLocalDate(session.WorkspaceTimezone))
	terms, err := h.academicTerms(r.Context(), session.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delivery_groups_query_failed")
		return
	}
	selected, err := selectTerm(terms, r.URL.Query().Get("term_uuid"), today, true)
	if err != nil || selected == nil || selected.AcademicYearID == nil {
		writeError(w, http.StatusBadRequest, "term_not_found")
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT dg.id, dg.name, dg.teacher_uuid, COALESCE(actor.display_name, ''),
		       dg.subject_uuid, COALESCE(subject.name, ''),
		       dg.weekly_lesson_requirement, dg.required_double_lessons,
		       COUNT(DISTINCT dga.teaching_assignment_uuid)::integer,
		       COUNT(DISTINCT dgl.learner_uuid)::integer,
		       COALESCE(jsonb_agg(DISTINCT jsonb_build_object(
		         'teaching_assignment_uuid', eta.id::text,
		         'cohort_name', eta.cohort_name,
		         'subject_name', eta.subject_name
		       )) FILTER (WHERE eta.id IS NOT NULL), '[]'::jsonb)
		FROM timetable_delivery_group dg
		JOIN external_actor actor ON actor.workspace_id = dg.workspace_id AND actor.id = dg.teacher_uuid
		JOIN external_subject subject ON subject.workspace_id = dg.workspace_id AND subject.id = dg.subject_uuid
		LEFT JOIN timetable_delivery_group_assignment dga ON dga.workspace_id = dg.workspace_id AND dga.delivery_group_id = dg.id
		LEFT JOIN external_teaching_assignment eta ON eta.workspace_id = dga.workspace_id AND eta.id = dga.teaching_assignment_uuid
		LEFT JOIN timetable_delivery_group_learner dgl ON dgl.workspace_id = dg.workspace_id AND dgl.delivery_group_id = dg.id
		WHERE dg.workspace_id = $1 AND dg.academic_year_uuid = $2 AND dg.academic_term_uuid = $3
		  AND dg.lifecycle_status = 'ACTIVE'
		GROUP BY dg.id, dg.name, dg.teacher_uuid, actor.display_name, dg.subject_uuid, subject.name,
		         dg.weekly_lesson_requirement, dg.required_double_lessons
		ORDER BY dg.name`, session.WorkspaceID, selected.AcademicYearID, selected.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delivery_groups_query_failed")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, teacherID, subjectID uuid.UUID
		var name, teacherName, subjectName string
		var weekly, doubles, assignmentCount, learnerCount int
		var assignmentsJSON []byte
		if err := rows.Scan(&id, &name, &teacherID, &teacherName, &subjectID, &subjectName, &weekly, &doubles, &assignmentCount, &learnerCount, &assignmentsJSON); err != nil {
			writeError(w, http.StatusInternalServerError, "delivery_groups_scan_failed")
			return
		}
		var assignments []map[string]any
		_ = json.Unmarshal(assignmentsJSON, &assignments)
		items = append(items, map[string]any{
			"delivery_group_uuid": id.String(), "name": name,
			"teacher_uuid": teacherID.String(), "teacher_name": teacherName,
			"subject_uuid": subjectID.String(), "subject_name": subjectName,
			"shared_periods_per_cycle": weekly, "required_double_lessons": doubles,
			"assignment_count": assignmentCount, "learner_count": learnerCount,
			"assignments": assignments,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"delivery_groups": items, "count": len(items)})
}

func (h *Handler) createDeliveryGroup(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	today, _ := time.Parse("2006-01-02", workspaceLocalDate(session.WorkspaceTimezone))
	terms, err := h.academicTerms(r.Context(), session.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delivery_group_create_failed")
		return
	}
	selected, err := selectTerm(terms, r.URL.Query().Get("term_uuid"), today, false)
	if err != nil || selected == nil || selected.AcademicYearID == nil {
		writeError(w, http.StatusBadRequest, "term_not_schedulable")
		return
	}
	var body deliveryGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_delivery_group")
		return
	}
	assignmentIDs, err := parseUUIDStrings(body.AssignmentUUIDs, "invalid_assignment_uuid")
	if err != nil || len(assignmentIDs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_assignment_uuid")
		return
	}
	if body.SharedPeriodsPerCycle <= 0 || body.RequiredDoubleLessons*2 > body.SharedPeriodsPerCycle {
		writeError(w, http.StatusBadRequest, "invalid_double_demand")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delivery_group_create_failed")
		return
	}
	defer tx.Rollback(r.Context())
	rows, err := tx.Query(r.Context(), `
		SELECT id, teacher_uuid, subject_uuid, cohort_uuid, cohort_subject_uuid, cohort_name, subject_name
		FROM external_teaching_assignment
		WHERE workspace_id = $1 AND id = ANY($2::uuid[]) AND status = 'ACTIVE'
		ORDER BY cohort_name, subject_name`, session.WorkspaceID, assignmentIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "reference_scope_mismatch")
		return
	}
	defer rows.Close()
	var teacherID, subjectID uuid.UUID
	cohortIDs := []uuid.UUID{}
	cohortSubjectIDs := []uuid.UUID{}
	assignmentNames := []string{}
	seen := 0
	for rows.Next() {
		var assignmentID, currentTeacherID, currentSubjectID, cohortID, cohortSubjectID uuid.UUID
		var cohortName, subjectName string
		if err := rows.Scan(&assignmentID, &currentTeacherID, &currentSubjectID, &cohortID, &cohortSubjectID, &cohortName, &subjectName); err != nil {
			writeError(w, http.StatusInternalServerError, "delivery_group_create_failed")
			return
		}
		if seen == 0 {
			teacherID, subjectID = currentTeacherID, currentSubjectID
		}
		if currentTeacherID != teacherID || currentSubjectID != subjectID {
			writeDomainError(w, http.StatusConflict, "delivery_group_teacher_mismatch", map[string]any{"message": "Combined classes for a lesson must have the same authorized teacher and subject. Correct the teaching assignments in Scholaroscope first."})
			return
		}
		seen++
		cohortIDs = append(cohortIDs, cohortID)
		cohortSubjectIDs = append(cohortSubjectIDs, cohortSubjectID)
		assignmentNames = append(assignmentNames, cohortName+" "+subjectName)
	}
	if seen != len(assignmentIDs) {
		writeError(w, http.StatusBadRequest, "reference_scope_mismatch")
		return
	}
	groupID := uuid.New()
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "Combined lesson: " + strings.Join(assignmentNames, ", ")
	}
	_, err = tx.Exec(r.Context(), `
		INSERT INTO timetable_delivery_group (
			id, workspace_id, academic_year_uuid, academic_term_uuid, name,
			teacher_uuid, subject_uuid, weekly_lesson_requirement, required_double_lessons,
			lifecycle_status, source_snapshot_hash
		)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, 'ACTIVE',
		       COALESCE(academic_snapshot_hash, '')
		FROM external_workspace WHERE id = $2`,
		groupID, session.WorkspaceID, *selected.AcademicYearID, selected.ID, name,
		teacherID, subjectID, body.SharedPeriodsPerCycle, body.RequiredDoubleLessons)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delivery_group_create_failed")
		return
	}
	for _, assignmentID := range assignmentIDs {
		if _, err := tx.Exec(r.Context(), `INSERT INTO timetable_delivery_group_assignment (workspace_id, delivery_group_id, teaching_assignment_uuid) VALUES ($1, $2, $3)`, session.WorkspaceID, groupID, assignmentID); err != nil {
			writeError(w, http.StatusInternalServerError, "delivery_group_create_failed")
			return
		}
	}
	learnerRows, err := tx.Query(r.Context(), `
		SELECT DISTINCT enrollment.learner_uuid, enrollment.cohort_uuid
		FROM external_learner_subject_enrollment enrollment
		WHERE enrollment.workspace_id = $1
		  AND enrollment.cohort_subject_uuid = ANY($2::uuid[])
		  AND enrollment.status = 'ACTIVE'
		ORDER BY enrollment.learner_uuid`, session.WorkspaceID, cohortSubjectIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delivery_group_create_failed")
		return
	}
	defer learnerRows.Close()
	learnerCount := 0
	for learnerRows.Next() {
		var learnerID, cohortID uuid.UUID
		if learnerRows.Scan(&learnerID, &cohortID) != nil {
			writeError(w, http.StatusInternalServerError, "delivery_group_create_failed")
			return
		}
		learnerCount++
		if _, err := tx.Exec(r.Context(), `INSERT INTO timetable_delivery_group_learner (workspace_id, delivery_group_id, learner_uuid, cohort_uuid) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, session.WorkspaceID, groupID, learnerID, cohortID); err != nil {
			writeError(w, http.StatusInternalServerError, "delivery_group_create_failed")
			return
		}
	}
	if learnerCount == 0 {
		writeError(w, http.StatusConflict, "delivery_group_has_no_learners")
		return
	}
	if err := h.invalidateDraftsForTermTx(r.Context(), tx, session.WorkspaceID, selected.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "draft_invalidation_failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "delivery_group_create_failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"delivery_group_uuid": groupID.String(), "learner_count": learnerCount})
}

func (h *Handler) DeliveryGroupDetail(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	groupID, err := uuid.Parse(r.PathValue("groupId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_delivery_group_uuid")
		return
	}
	tag, err := h.pool.Exec(r.Context(), `UPDATE timetable_delivery_group SET lifecycle_status = 'DISABLED', updated_at = now() WHERE id = $1 AND workspace_id = $2`, groupID, session.WorkspaceID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "delivery_group_not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "DISABLED"})
}

type parallelBlockRequest struct {
	Name               string   `json:"name"`
	DeliveryGroupUUIDs []string `json:"delivery_group_uuids"`
}

func (h *Handler) ParallelBlocks(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	if r.Method == http.MethodPost {
		h.createParallelBlock(w, r, session)
		return
	}
	today, _ := time.Parse("2006-01-02", workspaceLocalDate(session.WorkspaceTimezone))
	terms, err := h.academicTerms(r.Context(), session.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "parallel_blocks_query_failed")
		return
	}
	selected, err := selectTerm(terms, r.URL.Query().Get("term_uuid"), today, true)
	if err != nil || selected == nil || selected.AcademicYearID == nil {
		writeError(w, http.StatusBadRequest, "term_not_found")
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT block.id, block.name,
		       COUNT(DISTINCT member.delivery_group_id)::integer,
		       COALESCE(jsonb_agg(DISTINCT jsonb_build_object(
		         'delivery_group_uuid', dg.id::text,
		         'name', dg.name,
		         'subject_name', subject.name,
		         'teacher_name', actor.display_name,
		         'learner_count', learner_counts.learner_count
		       )) FILTER (WHERE dg.id IS NOT NULL), '[]'::jsonb)
		FROM timetable_parallel_block block
		LEFT JOIN timetable_parallel_block_member member
		  ON member.workspace_id = block.workspace_id AND member.parallel_block_id = block.id
		LEFT JOIN timetable_delivery_group dg
		  ON dg.workspace_id = member.workspace_id AND dg.id = member.delivery_group_id
		LEFT JOIN external_subject subject
		  ON subject.workspace_id = dg.workspace_id AND subject.id = dg.subject_uuid
		LEFT JOIN external_actor actor
		  ON actor.workspace_id = dg.workspace_id AND actor.id = dg.teacher_uuid
		LEFT JOIN (
		  SELECT workspace_id, delivery_group_id, COUNT(DISTINCT learner_uuid)::integer AS learner_count
		  FROM timetable_delivery_group_learner
		  GROUP BY workspace_id, delivery_group_id
		) learner_counts
		  ON learner_counts.workspace_id = dg.workspace_id AND learner_counts.delivery_group_id = dg.id
		WHERE block.workspace_id = $1 AND block.academic_year_uuid = $2 AND block.academic_term_uuid = $3
		  AND block.lifecycle_status = 'ACTIVE'
		GROUP BY block.id, block.name
		ORDER BY block.name`, session.WorkspaceID, selected.AcademicYearID, selected.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "parallel_blocks_query_failed")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name string
		var memberCount int
		var membersJSON []byte
		if err := rows.Scan(&id, &name, &memberCount, &membersJSON); err != nil {
			writeError(w, http.StatusInternalServerError, "parallel_blocks_scan_failed")
			return
		}
		var members []map[string]any
		_ = json.Unmarshal(membersJSON, &members)
		items = append(items, map[string]any{
			"parallel_block_uuid": id.String(),
			"name":                name,
			"member_count":        memberCount,
			"members":             members,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"parallel_blocks": items, "count": len(items)})
}

func (h *Handler) createParallelBlock(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	today, _ := time.Parse("2006-01-02", workspaceLocalDate(session.WorkspaceTimezone))
	terms, err := h.academicTerms(r.Context(), session.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "parallel_block_create_failed")
		return
	}
	selected, err := selectTerm(terms, r.URL.Query().Get("term_uuid"), today, false)
	if err != nil || selected == nil || selected.AcademicYearID == nil {
		writeError(w, http.StatusBadRequest, "term_not_schedulable")
		return
	}
	var body parallelBlockRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_parallel_block")
		return
	}
	groupIDs, err := parseUUIDStrings(body.DeliveryGroupUUIDs, "invalid_delivery_group_uuid")
	if err != nil || len(groupIDs) < 2 {
		writeError(w, http.StatusBadRequest, "parallel_block_too_small")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "parallel_block_create_failed")
		return
	}
	defer tx.Rollback(r.Context())
	var overlap int
	err = tx.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM (
		  SELECT learner_uuid
		  FROM timetable_delivery_group_learner
		  WHERE workspace_id = $1 AND delivery_group_id = ANY($2::uuid[])
		  GROUP BY learner_uuid
		  HAVING COUNT(DISTINCT delivery_group_id) > 1
		) overlap`, session.WorkspaceID, groupIDs).Scan(&overlap)
	if err != nil || overlap > 0 {
		writeError(w, http.StatusConflict, "learner_audience_overlap")
		return
	}
	blockID := uuid.New()
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "Alternative subject block"
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO timetable_parallel_block (id, workspace_id, academic_year_uuid, academic_term_uuid, name, lifecycle_status) VALUES ($1, $2, $3, $4, $5, 'ACTIVE')`, blockID, session.WorkspaceID, *selected.AcademicYearID, selected.ID, name); err != nil {
		writeError(w, http.StatusInternalServerError, "parallel_block_create_failed")
		return
	}
	for _, groupID := range groupIDs {
		if _, err := tx.Exec(r.Context(), `INSERT INTO timetable_parallel_block_member (workspace_id, parallel_block_id, delivery_group_id) VALUES ($1, $2, $3)`, session.WorkspaceID, blockID, groupID); err != nil {
			writeError(w, http.StatusInternalServerError, "parallel_block_create_failed")
			return
		}
	}
	if err := h.invalidateDraftsForTermTx(r.Context(), tx, session.WorkspaceID, selected.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "draft_invalidation_failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "parallel_block_create_failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"parallel_block_uuid": blockID.String()})
}

func (h *Handler) ParallelBlockDetail(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	blockID, err := uuid.Parse(r.PathValue("blockId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_parallel_block_uuid")
		return
	}
	tag, err := h.pool.Exec(r.Context(), `UPDATE timetable_parallel_block SET lifecycle_status = 'DISABLED', updated_at = now() WHERE id = $1 AND workspace_id = $2`, blockID, session.WorkspaceID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "parallel_block_not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "DISABLED"})
}

func parseUUIDStrings(values []string, code string) ([]uuid.UUID, error) {
	result := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for _, value := range values {
		parsed, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil {
			return nil, errCode(code)
		}
		if !seen[parsed] {
			seen[parsed] = true
			result = append(result, parsed)
		}
	}
	return result, nil
}

func (h *Handler) invalidateDraftsForTermTx(ctx context.Context, tx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, workspaceID, termID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		DELETE FROM solver_run
		WHERE workspace_id = $1
		  AND timetable_version_id IN (
		    SELECT version.id
		    FROM timetable_version version
		    JOIN timetable timetable ON timetable.id = version.timetable_id AND timetable.workspace_id = version.workspace_id
		    WHERE version.workspace_id = $1
		      AND timetable.academic_term_uuid = $2
		      AND version.status NOT IN ('PUBLISHED', 'SUPERSEDED', 'ARCHIVED')
		  )`, workspaceID, termID)
	return err
}
