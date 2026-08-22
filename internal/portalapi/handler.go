package portalapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"scholaroscope-temporal-service/internal/calendar"
	"scholaroscope-temporal-service/internal/launch"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	pool            *pgxpool.Pool
	calendarService *calendar.Service
}

func (h *Handler) ValidateVersion(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	versionID, err := uuid.Parse(r.PathValue("versionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_version_uuid")
		return
	}
	if !h.academicSnapshotIsCurrent(r.Context(), session.WorkspaceID, versionID) {
		writeError(w, http.StatusConflict, "academic_data_stale")
		return
	}
	if !h.versionTermIsSchedulable(r.Context(), session.WorkspaceID, versionID, session.WorkspaceTimezone) {
		writeError(w, http.StatusConflict, "term_not_schedulable")
		return
	}
	if err := h.rebuildConflictsForVersion(r.Context(), session.WorkspaceID, versionID); err != nil {
		writeError(w, http.StatusInternalServerError, "validation_failed")
		return
	}
	var solveStatus string
	var unscheduled, solverHard int
	if err := h.pool.QueryRow(r.Context(), `
		SELECT status, unscheduled_mandatory_lessons, hard_conflicts
		FROM solver_run
		WHERE workspace_id = $1 AND timetable_version_id = $2
		ORDER BY created_at DESC LIMIT 1`, session.WorkspaceID, versionID).Scan(&solveStatus, &unscheduled, &solverHard); err != nil {
		writeError(w, http.StatusConflict, "draft_regeneration_required")
		return
	}
	summary, err := h.conflictSummaryForVersion(r.Context(), session.WorkspaceID, versionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "validation_failed")
		return
	}
	status := "VALIDATED"
	if summary.HardConflicts > 0 || solverHard > 0 || unscheduled > 0 || (solveStatus != "COMPLETE" && solveStatus != "COMPLETE_WITH_SOFT_VIOLATIONS") {
		status = "BLOCKED"
	}
	versionStatus := "VALIDATED"
	if status == "BLOCKED" {
		versionStatus = "DRAFT"
	}
	hardConflicts := max(summary.HardConflicts, solverHard)
	validationSummary := mustJSON(map[string]any{"status": status, "hard_conflicts": hardConflicts, "soft_conflicts": summary.SoftConflicts, "unscheduled_mandatory_lessons": unscheduled})
	if _, err := h.pool.Exec(r.Context(), `UPDATE timetable_version SET status = $1, validation_summary = $2::jsonb, updated_at = now() WHERE id = $3 AND workspace_id = $4 AND status NOT IN ('PUBLISHED', 'SUPERSEDED', 'ARCHIVED')`, versionStatus, validationSummary, versionID, session.WorkspaceID); err != nil {
		writeError(w, http.StatusInternalServerError, "validation_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":                        status,
		"hard_conflicts":                hardConflicts,
		"soft_conflicts":                summary.SoftConflicts,
		"unscheduled_mandatory_lessons": unscheduled,
		"can_publish":                   status == "VALIDATED",
	})
}

func (h *Handler) PublishVersion(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	versionID, err := uuid.Parse(r.PathValue("versionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_version_uuid")
		return
	}
	var body struct {
		EffectiveDate string `json:"effective_date"`
		Reason        string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	projection, err := h.publishVersion(r.Context(), session, versionID, body.Reason)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, projection)
}

type conflictSummary struct {
	HardConflicts int `json:"hard_conflicts"`
	SoftConflicts int `json:"soft_conflicts"`
}

func publicationBlocker(versionStatus, solveStatus string, unscheduled, solverHardConflicts, detectedHardConflicts int) string {
	if versionStatus != "VALIDATED" {
		return "complete_solver_validation_required"
	}
	if (solveStatus != "COMPLETE" && solveStatus != "COMPLETE_WITH_SOFT_VIOLATIONS") || unscheduled > 0 || solverHardConflicts > 0 {
		return "incomplete_or_conflicting_schedule"
	}
	if detectedHardConflicts > 0 {
		return "hard_conflicts_block_publication"
	}
	return ""
}

func (h *Handler) academicSnapshotIsCurrent(ctx context.Context, workspaceID, versionID uuid.UUID) bool {
	var current bool
	err := h.pool.QueryRow(ctx, `
		SELECT workspace.academic_snapshot_hash IS NOT NULL
		   AND version.academic_snapshot_hash = workspace.academic_snapshot_hash
		FROM timetable_version version
		JOIN external_workspace workspace ON workspace.id = version.workspace_id
		WHERE version.id = $1 AND version.workspace_id = $2`, versionID, workspaceID).Scan(&current)
	return err == nil && current
}

func (h *Handler) versionTermIsSchedulable(ctx context.Context, workspaceID, versionID uuid.UUID, timezone string) bool {
	var term academicTerm
	err := h.pool.QueryRow(ctx, `
		SELECT term.id, term.academic_year_uuid, term.scholaroscope_academic_year_ref,
		       term.name, term.academic_year_label, term.start_date, term.end_date,
		       term.status, term.calendar_ready, term.is_frozen
		FROM timetable_version version
		JOIN timetable timetable ON timetable.id = version.timetable_id AND timetable.workspace_id = version.workspace_id
		JOIN external_academic_term term ON term.id = timetable.academic_term_uuid AND term.workspace_id = version.workspace_id
		WHERE version.id = $1 AND version.workspace_id = $2`, versionID, workspaceID).Scan(
		&term.ID, &term.AcademicYearID, &term.AcademicYearRef, &term.Name, &term.AcademicYearLabel,
		&term.StartDate, &term.EndDate, &term.Status, &term.CalendarReady, &term.Frozen,
	)
	if err != nil {
		return false
	}
	today, parseErr := time.Parse("2006-01-02", workspaceLocalDate(timezone))
	if parseErr != nil {
		return false
	}
	lifecycle := termLifecycle(term, today)
	return lifecycle == "ACTIVE" || lifecycle == "UPCOMING"
}

func (h *Handler) conflictSummaryForVersion(ctx context.Context, workspaceID, versionID uuid.UUID) (*conflictSummary, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT severity, COUNT(*)
		FROM scheduling_conflict
		WHERE org_id = $1
		  AND timetable_version_id = $2
		  AND resolved = false
		GROUP BY severity`,
		workspaceID,
		versionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	summary := &conflictSummary{}
	for rows.Next() {
		var severity string
		var count int
		if err := rows.Scan(&severity, &count); err != nil {
			return nil, err
		}
		if severity == "HARD" {
			summary.HardConflicts = count
		} else {
			summary.SoftConflicts += count
		}
	}
	return summary, rows.Err()
}

func (h *Handler) publishVersion(ctx context.Context, session *launch.PortalSession, versionID uuid.UUID, reason string) (map[string]any, error) {
	if !h.academicSnapshotIsCurrent(ctx, session.WorkspaceID, versionID) {
		return nil, errCode("academic_data_stale")
	}
	if !h.versionTermIsSchedulable(ctx, session.WorkspaceID, versionID, session.WorkspaceTimezone) {
		return nil, errCode("term_not_schedulable")
	}
	var persistedStatus string
	if err := h.pool.QueryRow(ctx, `SELECT status FROM timetable_version WHERE id = $1 AND workspace_id = $2`, versionID, session.WorkspaceID).Scan(&persistedStatus); err != nil {
		return nil, errCode("version_not_publishable")
	}
	if persistedStatus != "VALIDATED" {
		return nil, errCode("complete_solver_validation_required")
	}
	var solveStatus string
	var unscheduled, solverHardConflicts int
	if err := h.pool.QueryRow(ctx, `
		SELECT status, unscheduled_mandatory_lessons, hard_conflicts
		FROM solver_run
		WHERE workspace_id = $1 AND timetable_version_id = $2
		ORDER BY created_at DESC LIMIT 1`, session.WorkspaceID, versionID,
	).Scan(&solveStatus, &unscheduled, &solverHardConflicts); err != nil {
		return nil, errCode("complete_solver_validation_required")
	}
	if err := h.rebuildConflictsForVersion(ctx, session.WorkspaceID, versionID); err != nil {
		return nil, err
	}
	summary, err := h.conflictSummaryForVersion(ctx, session.WorkspaceID, versionID)
	if err != nil {
		return nil, err
	}
	if blocker := publicationBlocker(persistedStatus, solveStatus, unscheduled, solverHardConflicts, summary.HardConflicts); blocker != "" {
		return nil, errCode(blocker)
	}
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var timetableID uuid.UUID
	var versionNumber int
	var effectiveStart, effectiveEnd time.Time
	var previousVersionID *uuid.UUID
	var termName, academicYearLabel, scholaroscopeTermRef string
	err = tx.QueryRow(ctx, `
		SELECT tv.timetable_id, tv.version_number, tv.effective_start, tv.effective_end,
		       (
		           SELECT prev.id
		           FROM timetable_version prev
		           WHERE prev.workspace_id = tv.workspace_id
		             AND prev.timetable_id = tv.timetable_id
		             AND prev.status = 'PUBLISHED'
		             AND prev.id <> tv.id
		           ORDER BY prev.published_at DESC NULLS LAST, prev.version_number DESC
		           LIMIT 1
		       ) AS previous_version_id,
		       COALESCE(term.name, ''), COALESCE(term.academic_year_label, ''),
		       COALESCE(term.scholaroscope_term_ref, '')
		FROM timetable_version tv
		JOIN timetable t ON t.id = tv.timetable_id AND t.workspace_id = tv.workspace_id
		LEFT JOIN external_academic_term term ON term.id = t.academic_term_uuid AND term.workspace_id = tv.workspace_id
		WHERE tv.id = $1
		  AND tv.workspace_id = $2
		  AND tv.status = 'VALIDATED'`,
		versionID,
		session.WorkspaceID,
	).Scan(&timetableID, &versionNumber, &effectiveStart, &effectiveEnd, &previousVersionID, &termName, &academicYearLabel, &scholaroscopeTermRef)
	if err != nil {
		return nil, errCode("version_not_publishable")
	}
	entries, err := h.entriesForVersionTx(ctx, tx, session.WorkspaceID, versionID)
	if err != nil {
		return nil, err
	}
	diff := []map[string]any{}
	eventType := "temporal.timetable.learning.published.v1"
	if previousVersionID == nil {
		for _, entry := range entries {
			diff = append(diff, map[string]any{
				"change_type":       "ENTRY_ADDED",
				"stable_entry_uuid": entry["stable_entry_uuid"],
				"after":             entry,
			})
		}
	} else {
		previousEntries, err := h.entriesForVersionTx(ctx, tx, session.WorkspaceID, *previousVersionID)
		if err != nil {
			return nil, err
		}
		diff = computeDiff(previousEntries, entries)
		eventType = "temporal.timetable.learning.amended.v1"
	}
	_, err = tx.Exec(ctx, `
		UPDATE timetable_version
		SET status = 'SUPERSEDED', updated_at = now()
		WHERE workspace_id = $1
		  AND timetable_id = $2
		  AND status = 'PUBLISHED'
		  AND id <> $3`,
		session.WorkspaceID,
		timetableID,
		versionID,
	)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE timetable_version
		SET status = 'PUBLISHED',
		    publication_reason = $3,
		    published_by_actor_id = $4,
		    published_at = now(),
		    updated_at = now()
		WHERE id = $1
		  AND workspace_id = $2`,
		versionID,
		session.WorkspaceID,
		reason,
		session.ActorID,
	)
	if err != nil {
		return nil, err
	}
	outboxID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_event (
			id, workspace_id, installation_id, event_type, schema_version,
			aggregate_type, aggregate_uuid, aggregate_version, correlation_id,
			idempotency_key, payload
		)
		VALUES ($1, $2, $3, $15, '1.0',
		        'TIMETABLE_VERSION', $4, $5, $6, $7,
		        jsonb_build_object(
		          'version_uuid', $4::text,
		          'timetable_uuid', $8::text,
		          'version_label', $9::text,
		          'effective_from', $10::text,
		          'effective_until', $11::text,
			          'publication_reason', $12::text,
			          'term_label', $16::text,
			          'academic_year_label', $17::text,
			          'scholaroscope_term_id', NULLIF($18::text, ''),
		          'published_at', now(),
		          'entries', $13::jsonb,
		          'diff', $14::jsonb
		        ))`,
		outboxID,
		session.WorkspaceID,
		session.InstallationID,
		versionID,
		versionNumber,
		uuid.New(),
		"publication:"+versionID.String(),
		timetableID,
		versionNumber,
		effectiveStart.Format("2006-01-02"),
		effectiveEnd.Format("2006-01-02"),
		reason,
		mustJSON(entries),
		mustJSON(diff),
		eventType,
		termName,
		academicYearLabel,
		scholaroscopeTermRef,
	)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":          "PUBLISHED",
		"version_uuid":    versionID,
		"outbox_event_id": outboxID,
	}, nil
}

type errCode string

func (e errCode) Error() string {
	return string(e)
}

func mustJSON(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	return string(body)
}

func (h *Handler) entriesForVersionTx(ctx context.Context, tx pgx.Tx, workspaceID, versionID uuid.UUID) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, `
		SELECT te.id, te.stable_entry_uuid, te.teacher_uuid, COALESCE(a.display_name, ''),
		       te.cohort_uuid, COALESCE(c.name, ''),
		       te.subject_uuid, COALESCE(s.name, ''), COALESCE(s.code, ''),
		       te.cohort_subject_uuid, te.room_id, COALESCE(r.name, ''),
		       te.day_of_week, te.start_time::text, te.end_time::text,
		       te.duration_periods, te.start_period_index,
		       COALESCE(eta.teacher_ref, ''), COALESCE(eta.cohort_ref, ''),
		       COALESCE(eta.subject_ref, ''), COALESCE(eta.cohort_subject_ref, ''),
		       te.delivery_group_uuid, te.parallel_block_uuid, te.learner_count,
		       COALESCE((
		         SELECT jsonb_agg(jsonb_build_object(
		           'cohort_uuid', cohort.id::text,
		           'cohort_ref', cohort.scholaroscope_cohort_ref,
		           'cohort_name', cohort.name
		         ) ORDER BY cohort.name)
		         FROM external_cohort cohort
		         WHERE cohort.workspace_id = te.workspace_id
		           AND cohort.id = ANY(te.cohort_uuids)
		       ), CASE WHEN te.cohort_uuid IS NULL THEN '[]'::jsonb ELSE jsonb_build_array(jsonb_build_object(
		           'cohort_uuid', te.cohort_uuid::text,
		           'cohort_ref', COALESCE(c.scholaroscope_cohort_ref, ''),
		           'cohort_name', COALESCE(c.name, '')
		       )) END) AS cohorts,
		       COALESCE((
		         SELECT jsonb_agg(jsonb_build_object(
		           'cohort_subject_uuid', cohort_subject.id::text,
		           'cohort_subject_ref', cohort_subject.scholaroscope_cohort_subject_ref,
		           'cohort_uuid', cohort_subject.cohort_uuid::text
		         ) ORDER BY cohort_subject.label)
		         FROM external_cohort_subject cohort_subject
		         WHERE cohort_subject.workspace_id = te.workspace_id
		           AND cohort_subject.id = ANY(te.cohort_subject_uuids)
		       ), CASE WHEN te.cohort_subject_uuid IS NULL THEN '[]'::jsonb ELSE jsonb_build_array(jsonb_build_object(
		           'cohort_subject_uuid', te.cohort_subject_uuid::text,
		           'cohort_subject_ref', COALESCE(eta.cohort_subject_ref, ''),
		           'cohort_uuid', COALESCE(te.cohort_uuid::text, '')
		       )) END) AS cohort_subjects,
		       to_jsonb(COALESCE(te.teaching_assignment_uuids, '{}'::uuid[])) AS teaching_assignment_uuids,
		       EXISTS(
		         SELECT 1 FROM scheduling_conflict conflict
		         WHERE conflict.org_id = te.workspace_id
		           AND conflict.timetable_version_id = te.timetable_version_id
		           AND conflict.timetable_entry_id = te.id
		           AND conflict.resolved = false AND conflict.severity = 'HARD'
		       ) AS has_hard_conflict
		FROM timetable_entry te
		LEFT JOIN external_actor a ON a.workspace_id = te.workspace_id AND a.id = te.teacher_uuid
		LEFT JOIN external_cohort c ON c.workspace_id = te.workspace_id AND c.id = te.cohort_uuid
		LEFT JOIN external_subject s ON s.workspace_id = te.workspace_id AND s.id = te.subject_uuid
		LEFT JOIN room r ON r.workspace_id = te.workspace_id AND r.id = te.room_id
		LEFT JOIN external_teaching_assignment eta
		       ON eta.workspace_id = te.workspace_id
		      AND eta.teacher_uuid = te.teacher_uuid
		      AND eta.cohort_subject_uuid = te.cohort_subject_uuid
		      AND eta.status = 'ACTIVE'
		WHERE te.workspace_id = $1
		  AND te.timetable_version_id = $2
		ORDER BY te.day_of_week, te.start_time, te.id`,
		workspaceID,
		versionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []map[string]any{}
	for rows.Next() {
		var entryID, stableID uuid.UUID
		var teacherID, cohortID, subjectID, cohortSubjectID, roomID *uuid.UUID
		var teacherName, cohortName, subjectName, subjectCode, roomName string
		var day, duration, startPeriod int
		var startTime, endTime string
		var teacherRef, cohortRef, subjectRef, cohortSubjectRef string
		var deliveryGroupID, parallelBlockID *uuid.UUID
		var learnerCount int
		var cohortsJSON, cohortSubjectsJSON, assignmentIDsJSON []byte
		var hasHardConflict bool
		if err := rows.Scan(
			&entryID, &stableID, &teacherID, &teacherName,
			&cohortID, &cohortName,
			&subjectID, &subjectName, &subjectCode,
			&cohortSubjectID, &roomID, &roomName,
			&day, &startTime, &endTime,
			&duration, &startPeriod,
			&teacherRef, &cohortRef, &subjectRef, &cohortSubjectRef,
			&deliveryGroupID, &parallelBlockID, &learnerCount,
			&cohortsJSON, &cohortSubjectsJSON, &assignmentIDsJSON,
			&hasHardConflict,
		); err != nil {
			return nil, err
		}
		var cohorts, cohortSubjects []map[string]any
		var teachingAssignmentUUIDs []string
		_ = json.Unmarshal(cohortsJSON, &cohorts)
		_ = json.Unmarshal(cohortSubjectsJSON, &cohortSubjects)
		_ = json.Unmarshal(assignmentIDsJSON, &teachingAssignmentUUIDs)
		if cohortName == "" && len(cohorts) > 0 {
			names := []string{}
			for _, item := range cohorts {
				if name, ok := item["cohort_name"].(string); ok && name != "" {
					names = append(names, name)
				}
			}
			cohortName = strings.Join(names, ", ")
		}
		entries = append(entries, map[string]any{
			"entry_uuid":          entryID.String(),
			"stable_entry_uuid":   stableID.String(),
			"logical_entry_uuid":  stableID.String(),
			"teacher_uuid":        uuidString(teacherID),
			"teacher_name":        teacherName,
			"teacher_ref":         teacherRef,
			"teacher_id":          teacherRef,
			"cohort_uuid":         uuidString(cohortID),
			"cohort_name":         cohortName,
			"cohort_ref":          cohortRef,
			"cohort_id":           cohortRef,
			"subject_uuid":        uuidString(subjectID),
			"subject_name":        subjectName,
			"subject_code":        subjectCode,
			"subject_ref":         subjectRef,
			"subject_id":          subjectRef,
			"cohort_subject_uuid": uuidString(cohortSubjectID),
			"cohort_subject_ref":  cohortSubjectRef,
			"cohorts":             cohorts,
			"cohort_subjects":     cohortSubjects,
			"teaching_assignment_uuids": teachingAssignmentUUIDs,
			"delivery_group_uuid": uuidString(deliveryGroupID),
			"parallel_block_uuid": uuidString(parallelBlockID),
			"learner_count":       learnerCount,
			"room_uuid":           uuidString(roomID),
			"room_name":           roomName,
			"day_of_week":         dayName(day),
			"start_time":          startTime,
			"end_time":            endTime,
			"duration_minutes":    durationMinutes(startTime, endTime),
			"duration_periods":    duration,
			"start_period_index":  startPeriod,
			"has_hard_conflict":   hasHardConflict,
		})
	}
	return entries, rows.Err()
}

func durationMinutes(start, end string) int {
	startAt, startErr := time.Parse("15:04", start)
	endAt, endErr := time.Parse("15:04", end)
	if startErr != nil || endErr != nil || !endAt.After(startAt) {
		return 0
	}
	return int(endAt.Sub(startAt).Minutes())
}

func uuidString(value *uuid.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func dayName(day int) string {
	names := []string{"MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY", "SUNDAY"}
	if day < 0 || day >= len(names) {
		return "UNKNOWN"
	}
	return names[day]
}

func computeDiff(before, after []map[string]any) []map[string]any {
	beforeByStable := map[string]map[string]any{}
	afterByStable := map[string]map[string]any{}
	for _, entry := range before {
		if key, ok := entry["stable_entry_uuid"].(string); ok {
			beforeByStable[key] = entry
		}
	}
	for _, entry := range after {
		if key, ok := entry["stable_entry_uuid"].(string); ok {
			afterByStable[key] = entry
		}
	}
	diff := []map[string]any{}
	for key, entry := range afterByStable {
		beforeEntry, existed := beforeByStable[key]
		if !existed {
			diff = append(diff, map[string]any{"change_type": "ENTRY_ADDED", "stable_entry_uuid": key, "after": entry})
			continue
		}
		if mustJSON(beforeEntry) != mustJSON(entry) {
			changeType := "TIME_CHANGED"
			if beforeEntry["teacher_uuid"] != entry["teacher_uuid"] {
				changeType = "TEACHER_CHANGED"
			} else if beforeEntry["cohort_uuid"] != entry["cohort_uuid"] {
				changeType = "COHORT_CHANGED"
			} else if beforeEntry["subject_uuid"] != entry["subject_uuid"] {
				changeType = "SUBJECT_CHANGED"
			} else if beforeEntry["room_uuid"] != entry["room_uuid"] {
				changeType = "ROOM_CHANGED"
			} else if beforeEntry["day_of_week"] != entry["day_of_week"] {
				changeType = "DAY_CHANGED"
			}
			diff = append(diff, map[string]any{"change_type": changeType, "stable_entry_uuid": key, "before": beforeEntry, "after": entry})
		}
	}
	for key, entry := range beforeByStable {
		if _, exists := afterByStable[key]; !exists {
			diff = append(diff, map[string]any{"change_type": "ENTRY_REMOVED", "stable_entry_uuid": key, "before": entry})
		}
	}
	return diff
}

func (h *Handler) rebuildConflictsForVersion(ctx context.Context, workspaceID, versionID uuid.UUID) error {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE scheduling_conflict
		SET resolved = true, resolved_at = now()
		WHERE org_id = $1
		  AND timetable_version_id = $2
		  AND resolved = false`,
		workspaceID,
		versionID,
	); err != nil {
		return err
	}
	statements := []string{
		`INSERT INTO scheduling_conflict (id, org_id, timetable_version_id, timetable_entry_id, constraint_code, affected_teacher_uuid, blocking_entry_uuid, severity, conflict_type, description)
		 SELECT gen_random_uuid(), e1.workspace_id, e1.timetable_version_id, e1.id, 'TEACHER_DOUBLE_BOOKED', e1.teacher_uuid, e2.id, 'HARD', 'TEACHER_DOUBLE_BOOKED', 'Teacher is assigned to overlapping timetable entries.'
		 FROM timetable_entry e1
		 JOIN timetable_entry e2 ON e2.workspace_id = e1.workspace_id AND e2.timetable_version_id = e1.timetable_version_id AND e2.id > e1.id
		 WHERE e1.workspace_id = $1 AND e1.timetable_version_id = $2
		   AND e1.teacher_uuid IS NOT NULL AND e1.teacher_uuid = e2.teacher_uuid
		   AND e1.day_of_week = e2.day_of_week AND e1.start_time < e2.end_time AND e2.start_time < e1.end_time
		 ON CONFLICT DO NOTHING`,
		`INSERT INTO scheduling_conflict (id, org_id, timetable_version_id, timetable_entry_id, constraint_code, affected_cohort_uuid, blocking_entry_uuid, severity, conflict_type, description)
		 SELECT gen_random_uuid(), e1.workspace_id, e1.timetable_version_id, e1.id, 'COHORT_DOUBLE_BOOKED', e1.cohort_uuid, e2.id, 'HARD', 'COHORT_DOUBLE_BOOKED', 'Cohort is assigned to overlapping timetable entries.'
		 FROM timetable_entry e1
		 JOIN timetable_entry e2 ON e2.workspace_id = e1.workspace_id AND e2.timetable_version_id = e1.timetable_version_id AND e2.id > e1.id
		 WHERE e1.workspace_id = $1 AND e1.timetable_version_id = $2
		   AND e1.cohort_uuid IS NOT NULL AND e1.cohort_uuid = e2.cohort_uuid
		   AND e1.day_of_week = e2.day_of_week AND e1.start_time < e2.end_time AND e2.start_time < e1.end_time
		 ON CONFLICT DO NOTHING`,
		`INSERT INTO scheduling_conflict (id, org_id, timetable_version_id, timetable_entry_id, constraint_code, affected_room_id, blocking_entry_uuid, severity, conflict_type, description)
		 SELECT gen_random_uuid(), e1.workspace_id, e1.timetable_version_id, e1.id, 'ROOM_DOUBLE_BOOKED', e1.room_id, e2.id, 'HARD', 'ROOM_DOUBLE_BOOKED', 'Room is assigned to overlapping timetable entries.'
		 FROM timetable_entry e1
		 JOIN timetable_entry e2 ON e2.workspace_id = e1.workspace_id AND e2.timetable_version_id = e1.timetable_version_id AND e2.id > e1.id
		 WHERE e1.workspace_id = $1 AND e1.timetable_version_id = $2
		   AND e1.room_id IS NOT NULL AND e1.room_id = e2.room_id
		   AND e1.day_of_week = e2.day_of_week AND e1.start_time < e2.end_time AND e2.start_time < e1.end_time
		 ON CONFLICT DO NOTHING`,
		`INSERT INTO scheduling_conflict (id, org_id, timetable_version_id, timetable_entry_id, constraint_code, affected_teacher_uuid, affected_cohort_uuid, severity, conflict_type, description)
		 SELECT gen_random_uuid(), e.workspace_id, e.timetable_version_id, e.id, 'UNAUTHORIZED_TEACHING_ASSIGNMENT', e.teacher_uuid, e.cohort_uuid, 'HARD', 'UNAUTHORIZED_TEACHING_ASSIGNMENT', 'Teacher is not synchronized as authorized for this class subject.'
		 FROM timetable_entry e
		 LEFT JOIN external_teaching_assignment eta
		        ON eta.workspace_id = e.workspace_id
		       AND eta.teacher_uuid = e.teacher_uuid
		       AND eta.cohort_subject_uuid = e.cohort_subject_uuid
		       AND eta.status = 'ACTIVE'
		 WHERE e.workspace_id = $1 AND e.timetable_version_id = $2
		   AND e.teacher_uuid IS NOT NULL
		   AND e.cohort_subject_uuid IS NOT NULL
		   AND eta.id IS NULL
		 ON CONFLICT DO NOTHING`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement, workspaceID, versionID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func NewHandler(pool *pgxpool.Pool, calendarService *calendar.Service) *Handler {
	return &Handler{pool: pool, calendarService: calendarService}
}

func (h *Handler) Workspace(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	var workspace struct {
		ID                     uuid.UUID  `json:"workspace_uuid"`
		DisplayName            string     `json:"display_name"`
		Timezone               string     `json:"timezone"`
		Status                 string     `json:"status"`
		ProvisioningState      string     `json:"provisioning_state"`
		IntegrationHealth      string     `json:"integration_health"`
		LastSuccessfulSyncAt   *time.Time `json:"last_successful_sync_at"`
		ReconciliationRequired bool       `json:"reconciliation_required"`
	}
	err := h.pool.QueryRow(r.Context(), `
		SELECT id, display_name, timezone, status, provisioning_state,
		       integration_health, last_successful_sync_at, reconciliation_required
		FROM external_workspace
		WHERE id = $1`,
		session.WorkspaceID,
	).Scan(
		&workspace.ID,
		&workspace.DisplayName,
		&workspace.Timezone,
		&workspace.Status,
		&workspace.ProvisioningState,
		&workspace.IntegrationHealth,
		&workspace.LastSuccessfulSyncAt,
		&workspace.ReconciliationRequired,
	)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace_not_found")
		return
	}
	today := workspaceLocalDate(workspace.Timezone)
	var currentAcademicYear map[string]any
	yearRow := h.pool.QueryRow(r.Context(), `
		SELECT id, scholaroscope_academic_year_ref, name, start_date::text, end_date::text, is_current, status
		FROM external_academic_year
		WHERE workspace_id = $1
		  AND is_current = true
		  AND status IN ('CURRENT', 'ACTIVE')
		ORDER BY start_date DESC
		LIMIT 1`,
		session.WorkspaceID,
	)
	var yearID uuid.UUID
	var yearRef, yearName, yearStart, yearEnd, yearStatus string
	var yearIsCurrent bool
	if err := yearRow.Scan(&yearID, &yearRef, &yearName, &yearStart, &yearEnd, &yearIsCurrent, &yearStatus); err == nil {
		currentAcademicYear = map[string]any{
			"academic_year_uuid": yearID.String(),
			"academic_year_ref":  yearRef,
			"name":               yearName,
			"start_date":         yearStart,
			"end_date":           yearEnd,
			"is_current":         yearIsCurrent,
			"status":             yearStatus,
		}
	}
	academicYearParam := ""
	if currentAcademicYear != nil {
		academicYearParam, _ = currentAcademicYear["academic_year_uuid"].(string)
	}
	var currentTerm map[string]any
	termRow := h.pool.QueryRow(r.Context(), `
		SELECT id, name, academic_year_label, start_date::text, end_date::text, status, calendar_ready, is_frozen
		FROM external_academic_term
		WHERE workspace_id = $1
		  AND status IN ('OPEN', 'ACTIVE', 'READY')
		  AND start_date <= $2::date
		  AND end_date >= $2::date
		  AND is_frozen = false
		  AND (NULLIF($3, '')::uuid IS NULL OR academic_year_uuid = NULLIF($3, '')::uuid)
		ORDER BY start_date DESC
		LIMIT 1`,
		session.WorkspaceID,
		today,
		academicYearParam,
	)
	var termID uuid.UUID
	var termName, academicYearLabel, termStart, termEnd, termStatus string
	var termCalendarReady, termFrozen bool
	if err := termRow.Scan(&termID, &termName, &academicYearLabel, &termStart, &termEnd, &termStatus, &termCalendarReady, &termFrozen); err == nil {
		currentTerm = map[string]any{
			"term_uuid":           termID.String(),
			"name":                termName,
			"academic_year_label": academicYearLabel,
			"start_date":          termStart,
			"end_date":            termEnd,
			"status":              termStatus,
			"calendar_ready":      termCalendarReady,
			"is_current":          true,
		}
	}
	var schedulableTerm map[string]any
	schedulableRow := h.pool.QueryRow(r.Context(), `
		SELECT id, name, academic_year_label, start_date::text, end_date::text, status, calendar_ready, is_frozen
		FROM external_academic_term
		WHERE workspace_id = $1
		  AND status IN ('OPEN', 'ACTIVE', 'READY')
		  AND is_frozen = false
		  AND end_date >= $2::date
		  AND (NULLIF($3, '')::uuid IS NULL OR academic_year_uuid = NULLIF($3, '')::uuid)
		ORDER BY CASE WHEN start_date <= $2::date THEN 0 ELSE 1 END, start_date
		LIMIT 1`,
		session.WorkspaceID,
		today,
		academicYearParam,
	)
	var schedID uuid.UUID
	var schedName, schedYearLabel, schedStart, schedEnd, schedStatus string
	var schedCalendarReady, schedFrozen bool
	if err := schedulableRow.Scan(&schedID, &schedName, &schedYearLabel, &schedStart, &schedEnd, &schedStatus, &schedCalendarReady, &schedFrozen); err == nil {
		schedulableTerm = map[string]any{
			"term_uuid":           schedID.String(),
			"name":                schedName,
			"academic_year_label": schedYearLabel,
			"start_date":          schedStart,
			"end_date":            schedEnd,
			"status":              schedStatus,
			"calendar_ready":      schedCalendarReady,
			"is_current":          currentTerm != nil && currentTerm["term_uuid"] == schedID.String(),
			"is_upcoming":         schedStart > today,
		}
	}
	counts := map[string]int{}
	for key, query := range map[string]string{
		"teacher_count":             "SELECT COUNT(DISTINCT teacher_uuid) FROM external_teaching_assignment WHERE workspace_id = $1 AND status = 'ACTIVE'",
		"class_count":               "SELECT COUNT(*) FROM external_cohort WHERE workspace_id = $1 AND status = 'ACTIVE'",
		"subject_count":             "SELECT COUNT(*) FROM external_subject WHERE workspace_id = $1 AND status IN ('ACTIVE', 'OFFERED', 'REACTIVATED')",
		"cohort_subject_count":      "SELECT COUNT(*) FROM external_cohort_subject WHERE workspace_id = $1 AND status = 'ACTIVE'",
		"teaching_assignment_count": "SELECT COUNT(*) FROM external_teaching_assignment WHERE workspace_id = $1 AND status = 'ACTIVE'",
		"timetable_count":           "SELECT COUNT(*) FROM timetable WHERE workspace_id = $1",
		"published_timetable_count": "SELECT COUNT(*) FROM timetable_version WHERE workspace_id = $1 AND status = 'PUBLISHED'",
	} {
		var count int
		if err := h.pool.QueryRow(r.Context(), query, session.WorkspaceID).Scan(&count); err == nil {
			counts[key] = count
		}
	}
	var activeCalendarCount int
	_ = h.pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM org_calendar_version WHERE org_id = $1 AND is_active = true`, session.WorkspaceID).Scan(&activeCalendarCount)
	readinessStatus := "READY"
	readinessMessage := "Academic data is ready for timetable setup."
	readinessChecks := []map[string]any{
		{"code": "PLUGIN_INSTALLED", "ok": workspace.Status == "ACTIVE"},
		{"code": "WORKSPACE_PROVISIONED", "ok": workspace.ProvisioningState == "READY"},
		{"code": "ACADEMIC_YEAR_SYNCHRONIZED", "ok": currentAcademicYear != nil},
		{"code": "SCHEDULABLE_TERM_SYNCHRONIZED", "ok": schedulableTerm != nil},
		{"code": "COHORTS_SYNCHRONIZED", "ok": counts["class_count"] > 0},
		{"code": "COHORT_SUBJECTS_SYNCHRONIZED", "ok": counts["cohort_subject_count"] > 0},
		{"code": "TEACHING_ASSIGNMENTS_SYNCHRONIZED", "ok": counts["teaching_assignment_count"] > 0},
		{"code": "ELIGIBLE_TEACHERS_AVAILABLE", "ok": counts["teacher_count"] > 0},
		{"code": "BELL_PERIODS_CONFIGURED", "ok": activeCalendarCount > 0},
		{"code": "TIMETABLE_PUBLISHED", "ok": counts["published_timetable_count"] > 0},
	}
	for _, check := range readinessChecks {
		if ok, _ := check["ok"].(bool); !ok {
			readinessStatus = "SETUP_REQUIRED"
			switch check["code"] {
			case "ACADEMIC_YEAR_SYNCHRONIZED":
				readinessMessage = "This workspace does not have an active academic year. Set one up in Scholaroscope to continue."
			case "SCHEDULABLE_TERM_SYNCHRONIZED":
				readinessMessage = "No active term is available for this workspace. Create or activate a term in Scholaroscope."
			case "TEACHING_ASSIGNMENTS_SYNCHRONIZED", "ELIGIBLE_TEACHERS_AVAILABLE":
				readinessMessage = "No teachers are assigned to class subjects yet. Complete teacher assignments in Scholaroscope."
			case "BELL_PERIODS_CONFIGURED":
				readinessMessage = "Your school day has not been configured. Add teaching periods and breaks to generate a timetable."
			default:
				readinessMessage = "Timetable setup still needs academic information from Scholaroscope."
			}
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_uuid":          workspace.ID,
		"display_name":            workspace.DisplayName,
		"timezone":                workspace.Timezone,
		"status":                  workspace.Status,
		"provisioning_state":      workspace.ProvisioningState,
		"integration_health":      workspace.IntegrationHealth,
		"last_successful_sync_at": workspace.LastSuccessfulSyncAt,
		"reconciliation_required": workspace.ReconciliationRequired,
		"actor": map[string]any{
			"actor_uuid":   session.ActorID.String(),
			"display_name": session.ActorDisplayName,
			"actor_kind":   session.ActorKind,
		},
		"current_academic_year": currentAcademicYear,
		"current_term":          currentTerm,
		"schedulable_term":      schedulableTerm,
		"counts":                counts,
		"readiness": map[string]any{
			"status":  readinessStatus,
			"message": readinessMessage,
			"checks":  readinessChecks,
		},
	})
}

func workspaceLocalDate(timezoneName string) string {
	location, err := time.LoadLocation(strings.TrimSpace(timezoneName))
	if err != nil {
		location = time.UTC
	}
	return time.Now().In(location).Format("2006-01-02")
}

func (h *Handler) GetCalendar(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	version, err := h.calendarService.GetActiveCalendar(r.Context(), session.WorkspaceID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"calendar_version": nil,
			"slots":            []any{},
			"status":           "NO_ACTIVE_CALENDAR",
		})
		return
	}
	slots, _ := h.calendarService.GetSlotsForVersion(r.Context(), version.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"calendar_version": version,
		"slots":            slots,
		"status":           "ACTIVE",
	})
}

func (h *Handler) PutCalendar(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	var body struct {
		LearningDays        []string               `json:"learning_days"`
		DayStartTime        string                 `json:"day_start_time"`
		DayEndTime          string                 `json:"day_end_time"`
		SlotDurationMinutes int16                  `json:"slot_duration_minutes"`
		BreakStructure      []calendar.BreakWindow `json:"break_structure"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_body")
		return
	}
	startTime, err := time.Parse("15:04", body.DayStartTime)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_day_start_time")
		return
	}
	endTime, err := time.Parse("15:04", body.DayEndTime)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_day_end_time")
		return
	}
	version, slots, err := h.calendarService.CreateCalendarWithSlotsActivated(r.Context(), session.WorkspaceID, &calendar.CreateCalendarInput{
		LearningDays:        body.LearningDays,
		DayStartTime:        startTime,
		DayEndTime:          endTime,
		SlotDurationMinutes: body.SlotDurationMinutes,
		BreakStructure:      body.BreakStructure,
	})
	if err != nil {
		var validation *calendar.ValidationError
		if errors.As(err, &validation) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": domainError{
				Type: "validation", Code: "invalid_school_day", Message: validation.Message,
				Details: map[string]any{"field_errors": map[string][]string{validation.Field: {validation.Message}}},
				Action:  &errorAction{"Review school day", "/school-day"},
			}})
			return
		}
		writeError(w, http.StatusInternalServerError, "calendar_persistence_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"calendar_version": version,
		"slots":            slots,
		"status":           "ACTIVE",
	})
}

func (h *Handler) CalendarExceptions(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	today, _ := time.Parse("2006-01-02", workspaceLocalDate(session.WorkspaceTimezone))
	terms, err := h.academicTerms(r.Context(), session.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "calendar_exceptions_query_failed")
		return
	}
	selected, err := selectTerm(terms, r.URL.Query().Get("term_uuid"), today, true)
	if err != nil || selected == nil || selected.AcademicYearID == nil {
		code := "active_term_not_found"
		if err != nil {
			code = err.Error()
		}
		writeError(w, http.StatusBadRequest, code)
		return
	}
	effectiveStart, effectiveEnd := selected.StartDate, selected.EndDate
	if versionValue := strings.TrimSpace(r.URL.Query().Get("version_uuid")); versionValue != "" {
		versionID, parseErr := uuid.Parse(versionValue)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid_version_uuid")
			return
		}
		var boundTerm uuid.UUID
		var boundYear *uuid.UUID
		if queryErr := h.pool.QueryRow(r.Context(), `
			SELECT t.academic_term_uuid, term.academic_year_uuid, tv.effective_start, tv.effective_end
			FROM timetable_version tv
			JOIN timetable t ON t.id = tv.timetable_id AND t.workspace_id = tv.workspace_id
			JOIN external_academic_term term ON term.id = t.academic_term_uuid AND term.workspace_id = tv.workspace_id
			WHERE tv.id = $1 AND tv.workspace_id = $2`, versionID, session.WorkspaceID).Scan(&boundTerm, &boundYear, &effectiveStart, &effectiveEnd); queryErr != nil || boundYear == nil {
			writeError(w, http.StatusNotFound, "version_not_found")
			return
		}
		if boundTerm != selected.ID {
			writeDomainError(w, http.StatusConflict, "term_not_schedulable", map[string]any{"reason": "selected_term_does_not_match_draft"})
			return
		}
	}
	rows, err := h.applicableCalendarExceptions(r.Context(), session.WorkspaceID, *selected.AcademicYearID, selected.ID, effectiveStart, effectiveEnd)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "calendar_exceptions_query_failed")
		return
	}
	exceptions := []map[string]any{}
	for _, item := range rows {
		exceptions = append(exceptions, map[string]any{
			"exception_uuid":     item.ID.String(),
			"date":               item.StartDate.Format("2006-01-02"),
			"end_date":           item.EndDate.Format("2006-01-02"),
			"kind":               item.Kind,
			"title":              item.Title,
			"blocks_learning":    item.AffectsLearning,
			"academic_term_uuid": selected.ID.String(),
			"source":             item.Source,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"exceptions":       exceptions,
		"count":            len(exceptions),
		"academic_context": termPayload(*selected, today),
		"effective_start":  effectiveStart.Format("2006-01-02"),
		"effective_end":    effectiveEnd.Format("2006-01-02"),
	})
}

func (h *Handler) Teachers(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	today, _ := time.Parse("2006-01-02", workspaceLocalDate(session.WorkspaceTimezone))
	terms, err := h.academicTerms(r.Context(), session.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "teachers_query_failed")
		return
	}
	selected, err := selectTerm(terms, r.URL.Query().Get("term_uuid"), today, true)
	if err != nil || selected == nil || selected.AcademicYearID == nil {
		code := "active_term_not_found"
		if err != nil {
			code = err.Error()
		}
		writeError(w, http.StatusBadRequest, code)
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT a.id, a.scholaroscope_user_ref, a.display_name, a.status,
		       jsonb_agg(DISTINCT jsonb_build_object(
		         'cohort_uuid', eta.cohort_uuid::text,
		         'cohort_name', eta.cohort_name,
		         'subject_uuid', eta.subject_uuid::text,
		         'subject_name', eta.subject_name,
		         'cohort_subject_uuid', eta.cohort_subject_uuid::text,
		         'cohort_subject_ref', eta.cohort_subject_ref
		       )) AS assignments
		FROM external_actor a
		JOIN external_teaching_assignment eta
		  ON eta.workspace_id = a.workspace_id
		 AND eta.teacher_uuid = a.id
		 AND eta.status = 'ACTIVE'
		JOIN external_cohort cohort
		  ON cohort.id = eta.cohort_uuid
		 AND cohort.workspace_id = eta.workspace_id
		 AND cohort.status = 'ACTIVE'
		WHERE a.workspace_id = $1
		  AND a.status = 'ACTIVE'
		  AND cohort.academic_year_uuid = $2
		GROUP BY a.id, a.scholaroscope_user_ref, a.display_name, a.status
		ORDER BY a.display_name`,
		session.WorkspaceID, selected.AcademicYearID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "teachers_query_failed")
		return
	}
	defer rows.Close()
	actors := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var ref, name, status string
		var assignments []byte
		if err := rows.Scan(&id, &ref, &name, &status, &assignments); err != nil {
			writeError(w, http.StatusInternalServerError, "teachers_scan_failed")
			return
		}
		var assignmentItems []map[string]any
		_ = json.Unmarshal(assignments, &assignmentItems)
		actors = append(actors, map[string]any{
			"actor_uuid":             id,
			"scholaroscope_user_ref": ref,
			"display_name":           name,
			"actor_kind":             "TEACHER",
			"actor_kinds":            []string{"TEACHER"},
			"status":                 status,
			"assignments":            assignmentItems,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"teachers": actors, "count": len(actors)})
}

func (h *Handler) Rooms(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	if r.Method == http.MethodPost {
		h.createRoom(w, r, session)
		return
	}
	rooms, err := h.listRooms(r.Context(), session.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "rooms_query_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms, "spaces": rooms, "count": len(rooms)})
}

func (h *Handler) listRooms(ctx context.Context, workspaceID uuid.UUID) ([]map[string]any, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT id, external_ref, name, capacity, exclusive, status, room_kind
		FROM room
		WHERE workspace_id = $1
		ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rooms := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var externalRef *string
		var name, status, roomKind string
		var capacity *int
		var exclusive bool
		if err := rows.Scan(&id, &externalRef, &name, &capacity, &exclusive, &status, &roomKind); err != nil {
			return nil, err
		}
		rooms = append(rooms, map[string]any{
			"room_uuid":    id,
			"external_ref": externalRef,
			"name":         name,
			"capacity":     capacity,
			"exclusive":    exclusive,
			"status":       status,
			"kind":         roomKind,
		})
	}
	return rooms, rows.Err()
}

func (h *Handler) createRoom(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	var body struct {
		Name      string `json:"name"`
		Capacity  *int   `json:"capacity"`
		Kind      string `json:"kind"`
		Exclusive *bool  `json:"exclusive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_room")
		return
	}
	id := uuid.New()
	kind := strings.ToUpper(strings.TrimSpace(body.Kind))
	if kind == "" {
		kind = "GENERAL"
	}
	if kind != "GENERAL" && kind != "SPECIALIZED" && kind != "SHARED" {
		writeError(w, http.StatusBadRequest, "invalid_room_kind")
		return
	}
	exclusive := true
	if body.Exclusive != nil {
		exclusive = *body.Exclusive
	}
	_, err := h.pool.Exec(r.Context(), `
		INSERT INTO room (id, workspace_id, name, capacity, room_kind, exclusive)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (workspace_id, name)
		DO UPDATE SET capacity = EXCLUDED.capacity, room_kind = EXCLUDED.room_kind,
		              exclusive = EXCLUDED.exclusive, status = 'ACTIVE', updated_at = now()`,
		id,
		session.WorkspaceID,
		strings.TrimSpace(body.Name),
		body.Capacity,
		kind,
		exclusive,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "room_save_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "SAVED"})
}

func (h *Handler) RoomDetail(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	roomID, err := uuid.Parse(r.PathValue("roomId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_room_uuid")
		return
	}
	if r.Method == http.MethodDelete {
		tx, beginErr := h.pool.Begin(r.Context())
		if beginErr != nil {
			writeError(w, http.StatusInternalServerError, "room_delete_failed")
			return
		}
		defer tx.Rollback(r.Context())
		var references int
		_ = tx.QueryRow(r.Context(), `SELECT COUNT(*) FROM timetable_entry WHERE workspace_id = $1 AND room_id = $2`, session.WorkspaceID, roomID).Scan(&references)
		if references > 0 {
			writeError(w, http.StatusConflict, "room_in_use")
			return
		}
		if _, err := tx.Exec(r.Context(), `UPDATE external_cohort SET default_room_id = NULL, updated_at = now() WHERE workspace_id = $1 AND default_room_id = $2`, session.WorkspaceID, roomID); err != nil {
			writeError(w, http.StatusInternalServerError, "room_delete_failed")
			return
		}
		tag, err := tx.Exec(r.Context(), `DELETE FROM room WHERE id = $1 AND workspace_id = $2`, roomID, session.WorkspaceID)
		if err != nil || tag.RowsAffected() != 1 {
			writeError(w, http.StatusNotFound, "room_not_found")
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "room_delete_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "DELETED"})
		return
	}
	var body struct {
		Name      string `json:"name"`
		Capacity  *int   `json:"capacity"`
		Kind      string `json:"kind"`
		Exclusive *bool  `json:"exclusive"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_room")
		return
	}
	var currentName, currentKind, currentStatus string
	var currentCapacity *int
	var currentExclusive bool
	if err := h.pool.QueryRow(r.Context(), `SELECT name, capacity, room_kind, exclusive, status FROM room WHERE id = $1 AND workspace_id = $2`, roomID, session.WorkspaceID).Scan(&currentName, &currentCapacity, &currentKind, &currentExclusive, &currentStatus); err != nil {
		writeError(w, http.StatusNotFound, "room_not_found")
		return
	}
	if strings.TrimSpace(body.Name) != "" {
		currentName = strings.TrimSpace(body.Name)
	}
	if body.Capacity != nil {
		currentCapacity = body.Capacity
	}
	if strings.TrimSpace(body.Kind) != "" {
		currentKind = strings.ToUpper(strings.TrimSpace(body.Kind))
	}
	if body.Exclusive != nil {
		currentExclusive = *body.Exclusive
	}
	if strings.TrimSpace(body.Status) != "" {
		currentStatus = strings.ToUpper(strings.TrimSpace(body.Status))
	}
	if (currentKind != "GENERAL" && currentKind != "SPECIALIZED" && currentKind != "SHARED") || (currentStatus != "ACTIVE" && currentStatus != "DISABLED") {
		writeError(w, http.StatusBadRequest, "invalid_room")
		return
	}
	_, err = h.pool.Exec(r.Context(), `UPDATE room SET name = $1, capacity = $2, room_kind = $3, exclusive = $4, status = $5, updated_at = now() WHERE id = $6 AND workspace_id = $7`, currentName, currentCapacity, currentKind, currentExclusive, currentStatus, roomID, session.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "room_save_failed")
		return
	}
	if currentStatus == "DISABLED" {
		_, _ = h.pool.Exec(r.Context(), `UPDATE external_cohort SET default_room_id = NULL, updated_at = now() WHERE workspace_id = $1 AND default_room_id = $2`, session.WorkspaceID, roomID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "UPDATED"})
}

func (h *Handler) Timetables(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	if r.Method == http.MethodPost {
		h.createTimetable(w, r, session)
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT t.id, t.name, t.timetable_type, t.academic_term_uuid,
		       COALESCE(term.name, ''), COALESCE(term.academic_year_label, ''),
		       tv.id, tv.version_number, tv.status,
		       tv.effective_start, tv.effective_end, tv.published_at
		FROM timetable t
		LEFT JOIN external_academic_term term ON term.id = t.academic_term_uuid AND term.workspace_id = t.workspace_id
		LEFT JOIN timetable_version tv ON tv.timetable_id = t.id
		WHERE t.workspace_id = $1
		ORDER BY t.created_at DESC, tv.version_number DESC`,
		session.WorkspaceID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "timetables_query_failed")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var timetableID uuid.UUID
		var name, timetableType, termName, academicYearLabel string
		var termID *uuid.UUID
		var versionID *uuid.UUID
		var versionNumber *int
		var versionStatus *string
		var effectiveStart, effectiveEnd *time.Time
		var publishedAt *time.Time
		if err := rows.Scan(&timetableID, &name, &timetableType, &termID, &termName, &academicYearLabel, &versionID, &versionNumber, &versionStatus, &effectiveStart, &effectiveEnd, &publishedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "timetables_scan_failed")
			return
		}
		items = append(items, map[string]any{
			"timetable_uuid":      timetableID,
			"name":                name,
			"type":                timetableType,
			"term_uuid":           termID,
			"term_name":           termName,
			"academic_year_label": academicYearLabel,
			"version_uuid":        versionID,
			"version_number":      versionNumber,
			"status":              versionStatus,
			"effective_start":     effectiveStart,
			"effective_end":       effectiveEnd,
			"published_at":        publishedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"timetables": items, "count": len(items)})
}

func (h *Handler) createTimetable(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	var body struct {
		Name             string `json:"name"`
		TimetableType    string `json:"timetable_type"`
		AcademicTermUUID string `json:"academic_term_uuid"`
		CalendarUUID     string `json:"calendar_uuid"`
		EffectiveStart   string `json:"effective_start"`
		EffectiveEnd     string `json:"effective_end"`
		ScopeKind        string `json:"scope_kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_timetable")
		return
	}
	timetableType := body.TimetableType
	if timetableType == "" {
		timetableType = "LEARNING"
	}
	if timetableType != "LEARNING" && timetableType != "EXAMINATION" {
		writeError(w, http.StatusBadRequest, "invalid_timetable_type")
		return
	}
	if timetableType == "EXAMINATION" {
		writeError(w, http.StatusConflict, "examination_scheduling_feature_gated")
		return
	}
	start, err := time.Parse("2006-01-02", body.EffectiveStart)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_effective_start")
		return
	}
	end, err := time.Parse("2006-01-02", body.EffectiveEnd)
	if err != nil || end.Before(start) {
		writeError(w, http.StatusBadRequest, "invalid_effective_end")
		return
	}
	var termID *uuid.UUID
	var termName, academicYearLabel string
	var termStart, termEnd time.Time
	if body.AcademicTermUUID != "" {
		parsed, err := uuid.Parse(body.AcademicTermUUID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_academic_term_uuid")
			return
		}
		termID = &parsed
		err = h.pool.QueryRow(r.Context(), `
			SELECT term.name, term.academic_year_label, term.start_date, term.end_date
			FROM external_academic_term term
			JOIN external_academic_year year ON year.id = term.academic_year_uuid AND year.workspace_id = term.workspace_id
			WHERE term.id = $1
			  AND term.workspace_id = $2
			  AND term.status = 'OPEN'
			  AND term.is_frozen = false
			  AND year.is_current = true`,
			*termID,
			session.WorkspaceID,
		).Scan(&termName, &academicYearLabel, &termStart, &termEnd)
		if err != nil {
			writeError(w, http.StatusBadRequest, "active_term_not_found")
			return
		}
	} else {
		var activeTermID uuid.UUID
		localToday := workspaceLocalDate(session.WorkspaceTimezone)
		err = h.pool.QueryRow(r.Context(), `
			SELECT term.id, term.name, term.academic_year_label, term.start_date, term.end_date
			FROM external_academic_term term
			JOIN external_academic_year year ON year.id = term.academic_year_uuid AND year.workspace_id = term.workspace_id
			WHERE term.workspace_id = $1
			  AND term.status = 'OPEN' AND term.is_frozen = false
			  AND year.is_current = true
			  AND term.start_date <= $2::date
			  AND term.end_date >= $2::date
			ORDER BY term.start_date DESC
			LIMIT 1`,
			session.WorkspaceID,
			localToday,
		).Scan(&activeTermID, &termName, &academicYearLabel, &termStart, &termEnd)
		if err != nil {
			writeError(w, http.StatusBadRequest, "active_term_not_found")
			return
		}
		termID = &activeTermID
	}
	if start.Before(termStart) || end.After(termEnd) {
		writeError(w, http.StatusBadRequest, "effective_dates_outside_active_term")
		return
	}
	var calendarID *uuid.UUID
	if body.CalendarUUID != "" {
		parsed, err := uuid.Parse(body.CalendarUUID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_calendar_uuid")
			return
		}
		calendarID = &parsed
	} else if timetableType == "LEARNING" {
		version, err := h.calendarService.GetActiveCalendar(r.Context(), session.WorkspaceID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "active_calendar_required")
			return
		}
		calendarID = &version.ID
	}
	scopeKind := body.ScopeKind
	if scopeKind == "" {
		scopeKind = "WORKSPACE"
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		typeLabel := "Learning"
		if timetableType == "EXAMINATION" {
			typeLabel = "Examination"
		}
		name = fmt.Sprintf("%s %s %s Timetable", session.WorkspaceName, termName, typeLabel)
		if academicYearLabel != "" {
			name = fmt.Sprintf("%s %s %s Timetable", session.WorkspaceName, academicYearLabel+" "+termName, typeLabel)
		}
	}
	timetableID := uuid.New()
	versionID := uuid.New()
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "timetable_create_failed")
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `
		INSERT INTO timetable (id, workspace_id, calendar_id, academic_term_uuid, timetable_type, scope_kind, name, effective_start, effective_end)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		timetableID, session.WorkspaceID, calendarID, termID, timetableType, scopeKind, name, start, end,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "timetable_create_failed")
		return
	}
	_, err = tx.Exec(r.Context(), `
		INSERT INTO timetable_version (id, workspace_id, timetable_id, version_number, status, effective_start, effective_end, academic_snapshot_hash)
		SELECT $1, $2, $3, 1, 'DRAFT', $4, $5, academic_snapshot_hash
		FROM external_workspace WHERE id = $2`,
		versionID, session.WorkspaceID, timetableID, start, end,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "timetable_version_create_failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "timetable_create_failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"timetable_uuid": timetableID,
		"version_uuid":   versionID,
		"status":         "DRAFT",
	})
}

func (h *Handler) TimetableDetail(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	timetableID, err := uuid.Parse(r.PathValue("timetableId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_timetable_uuid")
		return
	}
	var item map[string]any
	row := h.pool.QueryRow(r.Context(), `
		SELECT id, name, timetable_type, effective_start, effective_end
		FROM timetable
		WHERE id = $1 AND workspace_id = $2`,
		timetableID, session.WorkspaceID,
	)
	var id uuid.UUID
	var name, timetableType string
	var start, end time.Time
	if err := row.Scan(&id, &name, &timetableType, &start, &end); err != nil {
		writeError(w, http.StatusNotFound, "timetable_not_found")
		return
	}
	item = map[string]any{"timetable_uuid": id, "name": name, "type": timetableType, "effective_start": start, "effective_end": end}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) CreateVersion(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	timetableID, err := uuid.Parse(r.PathValue("timetableId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_timetable_uuid")
		return
	}
	versionID := uuid.New()
	var versionNumber int
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "version_create_failed")
		return
	}
	defer tx.Rollback(r.Context())
	var derivedFrom *uuid.UUID
	err = tx.QueryRow(r.Context(), `
		INSERT INTO timetable_version (id, workspace_id, timetable_id, version_number, status, derived_from_version_id, effective_start, effective_end, academic_snapshot_hash)
		SELECT $1, workspace_id, id,
		       COALESCE((SELECT MAX(version_number) + 1 FROM timetable_version WHERE workspace_id = t.workspace_id AND timetable_id = t.id), 1),
		       'DRAFT',
		       (SELECT id FROM timetable_version WHERE workspace_id = t.workspace_id AND timetable_id = t.id ORDER BY version_number DESC LIMIT 1),
		       effective_start, effective_end,
		       (SELECT academic_snapshot_hash FROM external_workspace WHERE id = t.workspace_id)
		FROM timetable t
		WHERE t.id = $2 AND t.workspace_id = $3
		RETURNING version_number, derived_from_version_id`,
		versionID, timetableID, session.WorkspaceID,
	).Scan(&versionNumber, &derivedFrom)
	if err != nil {
		writeError(w, http.StatusBadRequest, "version_create_failed")
		return
	}
	if derivedFrom != nil {
		_, err = tx.Exec(r.Context(), `
			INSERT INTO timetable_entry (
				id, workspace_id, timetable_version_id, stable_entry_uuid, entry_kind,
				teacher_uuid, cohort_uuid, subject_uuid, cohort_subject_uuid, room_id, resource_id,
				day_of_week, start_period_index, duration_periods, start_time, end_time, is_pinned
			)
			SELECT gen_random_uuid(), workspace_id, $1, stable_entry_uuid, entry_kind,
			       teacher_uuid, cohort_uuid, subject_uuid, cohort_subject_uuid, room_id, resource_id,
			       day_of_week, start_period_index, duration_periods, start_time, end_time, is_pinned
			FROM timetable_entry
			WHERE workspace_id = $2 AND timetable_version_id = $3`, versionID, session.WorkspaceID, *derivedFrom)
		if err != nil {
			writeError(w, http.StatusConflict, "version_create_failed")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "version_create_failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"version_uuid": versionID, "version_number": versionNumber, "status": "DRAFT"})
}

func (h *Handler) VersionDetail(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	versionID, err := uuid.Parse(r.PathValue("versionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_version_uuid")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "version_query_failed")
		return
	}
	defer tx.Rollback(r.Context())
	var timetableID uuid.UUID
	var versionNumber int
	var versionStatus string
	var timetableName, termName, academicYearLabel string
	var termID uuid.UUID
	var validationSummary []byte
	var start, end time.Time
	err = tx.QueryRow(r.Context(), `
		SELECT tv.timetable_id, tv.version_number, tv.status, tv.effective_start, tv.effective_end,
		       t.name, t.academic_term_uuid, term.name, term.academic_year_label, tv.validation_summary
		FROM timetable_version tv
		JOIN timetable t ON t.id = tv.timetable_id AND t.workspace_id = tv.workspace_id
		JOIN external_academic_term term ON term.id = t.academic_term_uuid AND term.workspace_id = tv.workspace_id
		WHERE tv.id = $1 AND tv.workspace_id = $2`,
		versionID, session.WorkspaceID,
	).Scan(&timetableID, &versionNumber, &versionStatus, &start, &end, &timetableName, &termID, &termName, &academicYearLabel, &validationSummary)
	if err != nil {
		writeError(w, http.StatusNotFound, "version_not_found")
		return
	}
	entries, err := h.entriesForVersionTx(r.Context(), tx, session.WorkspaceID, versionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "entries_query_failed")
		return
	}
	_ = tx.Commit(r.Context())
	validation := map[string]any{}
	_ = json.Unmarshal(validationSummary, &validation)
	writeJSON(w, http.StatusOK, map[string]any{
		"version_uuid":        versionID,
		"timetable_uuid":      timetableID,
		"version_number":      versionNumber,
		"status":              versionStatus,
		"effective_start":     start,
		"effective_end":       end,
		"name":                timetableName,
		"term_uuid":           termID,
		"term_name":           termName,
		"academic_year_label": academicYearLabel,
		"validation":          validation,
		"entries":             entries,
	})
}

func (h *Handler) VersionEntries(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	if r.Method == http.MethodPost {
		h.createEntry(w, r, session)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
}

func (h *Handler) EntryDetail(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	switch r.Method {
	case http.MethodPatch:
		h.updateEntry(w, r, session)
	case http.MethodDelete:
		h.deleteEntry(w, r, session)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
	}
}

type entryRequest struct {
	StableEntryUUID   string `json:"stable_entry_uuid"`
	TeacherUUID       string `json:"teacher_uuid"`
	CohortUUID        string `json:"cohort_uuid"`
	SubjectUUID       string `json:"subject_uuid"`
	CohortSubjectUUID string `json:"cohort_subject_uuid"`
	RoomUUID          string `json:"room_uuid"`
	DayOfWeek         int    `json:"day_of_week"`
	StartPeriodIndex  int    `json:"start_period_index"`
	DurationPeriods   int    `json:"duration_periods"`
	StartTime         string `json:"start_time"`
	EndTime           string `json:"end_time"`
}

func (h *Handler) createEntry(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	versionID, err := uuid.Parse(r.PathValue("versionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_version_uuid")
		return
	}
	var body entryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_entry")
		return
	}
	if !h.versionEditable(r.Context(), session.WorkspaceID, versionID) {
		writeError(w, http.StatusConflict, "version_not_editable")
		return
	}
	entryID := uuid.New()
	stableID := uuid.New()
	if body.StableEntryUUID != "" {
		if stableID, err = uuid.Parse(body.StableEntryUUID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_stable_entry_uuid")
			return
		}
	}
	if err := h.saveEntry(r.Context(), session.WorkspaceID, versionID, entryID, stableID, body, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"entry_uuid": entryID, "stable_entry_uuid": stableID})
}

func (h *Handler) updateEntry(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	versionID, err := uuid.Parse(r.PathValue("versionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_version_uuid")
		return
	}
	entryID, err := uuid.Parse(r.PathValue("entryId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_entry_uuid")
		return
	}
	var stableID uuid.UUID
	var parallelBlockID *uuid.UUID
	err = h.pool.QueryRow(r.Context(), `
		SELECT stable_entry_uuid, parallel_block_uuid FROM timetable_entry WHERE id = $1 AND workspace_id = $2 AND timetable_version_id = $3`,
		entryID, session.WorkspaceID, versionID,
	).Scan(&stableID, &parallelBlockID)
	if err != nil {
		writeError(w, http.StatusNotFound, "entry_not_found")
		return
	}
	if parallelBlockID != nil {
		writeError(w, http.StatusConflict, "parallel_block_atomic_move_required")
		return
	}
	if !h.versionEditable(r.Context(), session.WorkspaceID, versionID) {
		writeError(w, http.StatusConflict, "version_not_editable")
		return
	}
	var body entryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_entry")
		return
	}
	if err := h.saveEntry(r.Context(), session.WorkspaceID, versionID, entryID, stableID, body, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry_uuid": entryID, "stable_entry_uuid": stableID})
}

func (h *Handler) deleteEntry(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	versionID, err := uuid.Parse(r.PathValue("versionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_version_uuid")
		return
	}
	entryID, err := uuid.Parse(r.PathValue("entryId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_entry_uuid")
		return
	}
	if !h.versionEditable(r.Context(), session.WorkspaceID, versionID) {
		writeError(w, http.StatusConflict, "version_not_editable")
		return
	}
	tag, err := h.pool.Exec(r.Context(), `
		DELETE FROM timetable_entry WHERE id = $1 AND workspace_id = $2 AND timetable_version_id = $3`,
		entryID, session.WorkspaceID, versionID,
	)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "entry_not_found")
		return
	}
	_, _ = h.pool.Exec(r.Context(), `UPDATE timetable_version SET status = 'DRAFT', validation_summary = '{}'::jsonb, updated_at = now() WHERE id = $1 AND workspace_id = $2 AND status IN ('VALIDATING', 'VALIDATED')`, versionID, session.WorkspaceID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) versionEditable(ctx context.Context, workspaceID, versionID uuid.UUID) bool {
	var status string
	err := h.pool.QueryRow(ctx, `
		SELECT status FROM timetable_version WHERE id = $1 AND workspace_id = $2`,
		versionID, workspaceID,
	).Scan(&status)
	return err == nil && status != "PUBLISHED" && status != "SUPERSEDED" && status != "ARCHIVED"
}

func (h *Handler) saveEntry(ctx context.Context, workspaceID, versionID, entryID, stableID uuid.UUID, body entryRequest, update bool) error {
	startTime, err := time.Parse("15:04", body.StartTime)
	if err != nil {
		return errCode("invalid_start_time")
	}
	endTime, err := time.Parse("15:04", body.EndTime)
	if err != nil || !endTime.After(startTime) {
		return errCode("invalid_end_time")
	}
	teacherID, err := parseRequiredUUID(body.TeacherUUID, "invalid_teacher_uuid")
	if err != nil {
		return err
	}
	cohortID, err := parseRequiredUUID(body.CohortUUID, "invalid_cohort_uuid")
	if err != nil {
		return err
	}
	subjectID, err := parseRequiredUUID(body.SubjectUUID, "invalid_subject_uuid")
	if err != nil {
		return err
	}
	cohortSubjectID, err := parseRequiredUUID(body.CohortSubjectUUID, "invalid_cohort_subject_uuid")
	if err != nil {
		return err
	}
	var roomID *uuid.UUID
	if body.RoomUUID != "" {
		parsed, err := uuid.Parse(body.RoomUUID)
		if err != nil {
			return errCode("invalid_room_uuid")
		}
		roomID = &parsed
	}
	duration := body.DurationPeriods
	if duration <= 0 {
		duration = 1
	}
	if !h.referencesBelong(ctx, workspaceID, teacherID, cohortID, subjectID, cohortSubjectID, roomID) {
		return errCode("reference_scope_mismatch")
	}
	if update {
		_, err = h.pool.Exec(ctx, `
			UPDATE timetable_entry
			SET teacher_uuid = $4, cohort_uuid = $5, subject_uuid = $6, cohort_subject_uuid = $7,
			    room_id = $8, day_of_week = $9, start_period_index = $10, duration_periods = $11,
			    start_time = $12, end_time = $13, updated_at = now()
			WHERE id = $1 AND workspace_id = $2 AND timetable_version_id = $3`,
			entryID, workspaceID, versionID, teacherID, cohortID, subjectID, cohortSubjectID,
			roomID, body.DayOfWeek, body.StartPeriodIndex, duration, startTime, endTime,
		)
	} else {
		_, err = h.pool.Exec(ctx, `
			INSERT INTO timetable_entry (
				id, workspace_id, timetable_version_id, stable_entry_uuid,
				teacher_uuid, cohort_uuid, subject_uuid, cohort_subject_uuid, room_id,
				day_of_week, start_period_index, duration_periods, start_time, end_time
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			entryID, workspaceID, versionID, stableID, teacherID, cohortID, subjectID, cohortSubjectID,
			roomID, body.DayOfWeek, body.StartPeriodIndex, duration, startTime, endTime,
		)
	}
	if err != nil {
		return errCode("entry_save_failed")
	}
	_, _ = h.pool.Exec(ctx, `UPDATE timetable_version SET status = 'DRAFT', validation_summary = '{}'::jsonb, updated_at = now() WHERE id = $1 AND workspace_id = $2 AND status IN ('VALIDATING', 'VALIDATED')`, versionID, workspaceID)
	return nil
}

func parseRequiredUUID(value string, code string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.UUID{}, errCode(code)
	}
	return parsed, nil
}

func (h *Handler) referencesBelong(ctx context.Context, workspaceID uuid.UUID, teacherID, cohortID, subjectID, cohortSubjectID uuid.UUID, roomID *uuid.UUID) bool {
	var count int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM external_teaching_assignment
		WHERE workspace_id = $1
		  AND teacher_uuid = $2
		  AND cohort_uuid = $3
		  AND subject_uuid = $4
		  AND cohort_subject_uuid = $5
		  AND status = 'ACTIVE'`,
		workspaceID, teacherID, cohortID, subjectID, cohortSubjectID,
	).Scan(&count); err != nil || count != 1 {
		return false
	}
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM external_cohort WHERE workspace_id = $1 AND id = $2 AND status = 'ACTIVE'`, workspaceID, cohortID).Scan(&count); err != nil || count != 1 {
		return false
	}
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM external_subject WHERE workspace_id = $1 AND id = $2 AND status IN ('ACTIVE', 'OFFERED', 'REACTIVATED')`, workspaceID, subjectID).Scan(&count); err != nil || count != 1 {
		return false
	}
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM external_cohort_subject WHERE workspace_id = $1 AND id = $2 AND cohort_uuid = $3 AND subject_uuid = $4 AND status = 'ACTIVE'`, workspaceID, cohortSubjectID, cohortID, subjectID).Scan(&count); err != nil || count != 1 {
		return false
	}
	if roomID != nil {
		if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM room WHERE workspace_id = $1 AND id = $2 AND status = 'ACTIVE'`, workspaceID, *roomID).Scan(&count); err != nil || count != 1 {
			return false
		}
	}
	return true
}

func (h *Handler) TeachingDemands(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	today, _ := time.Parse("2006-01-02", workspaceLocalDate(session.WorkspaceTimezone))
	terms, err := h.academicTerms(r.Context(), session.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "teaching_demands_query_failed")
		return
	}
	selected, err := selectTerm(terms, r.URL.Query().Get("term_uuid"), today, true)
	if err != nil || selected == nil || selected.AcademicYearID == nil {
		code := "active_term_not_found"
		if err != nil {
			code = err.Error()
		}
		writeError(w, http.StatusBadRequest, code)
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT eta.id, eta.teacher_uuid, COALESCE(a.display_name, ''),
		       eta.cohort_subject_uuid, eta.cohort_uuid, eta.subject_uuid,
		       eta.cohort_name, eta.subject_name, eta.teacher_ref,
		       eta.cohort_subject_ref, eta.cohort_ref, eta.subject_ref, eta.status,
		       COALESCE(override.weekly_lesson_requirement, (eta.scheduling_requirements->>'weekly_lesson_requirement')::integer),
		       COALESCE(override.required_double_lessons, (eta.scheduling_requirements->>'required_double_lessons')::integer, 0),
		       CASE
		         WHEN override.weekly_lesson_requirement IS NOT NULL THEN 'TIMETABLE_OVERRIDE'
		         WHEN eta.scheduling_requirements ? 'weekly_lesson_requirement'
		           THEN COALESCE(NULLIF(eta.scheduling_requirements->>'demand_source', ''), CASE WHEN eta.scheduling_requirements ? 'scheme_ref' THEN 'SCHOLAROSCOPE_SCHEME' ELSE 'SCHOLAROSCOPE_AUTHORITY' END)
		         ELSE 'MISSING'
		       END,
		       COALESCE(scheduled.scheduled_periods, 0)
		FROM external_teaching_assignment eta
		JOIN external_actor a ON a.workspace_id = eta.workspace_id AND a.id = eta.teacher_uuid
		JOIN external_cohort cohort ON cohort.workspace_id = eta.workspace_id AND cohort.id = eta.cohort_uuid AND cohort.status = 'ACTIVE'
		LEFT JOIN timetable_demand_override override
		  ON override.workspace_id = eta.workspace_id
		 AND override.academic_term_uuid = $3
		 AND override.teaching_assignment_uuid = eta.id
		LEFT JOIN LATERAL (
		  SELECT COALESCE(SUM(entry.duration_periods), 0)::integer AS scheduled_periods
		  FROM timetable_entry entry
		  JOIN timetable_version version ON version.id = entry.timetable_version_id AND version.workspace_id = entry.workspace_id
		  JOIN timetable timetable ON timetable.id = version.timetable_id AND timetable.workspace_id = version.workspace_id
		  WHERE entry.workspace_id = eta.workspace_id
		    AND timetable.academic_term_uuid = $3
		    AND entry.teacher_uuid = eta.teacher_uuid
		    AND entry.cohort_subject_uuid = eta.cohort_subject_uuid
		    AND version.status <> 'ARCHIVED'
		) scheduled ON TRUE
		WHERE eta.workspace_id = $1
		  AND eta.status = 'ACTIVE'
		  AND cohort.academic_year_uuid = $2
		ORDER BY eta.cohort_name, eta.subject_name, a.display_name`,
		session.WorkspaceID, selected.AcademicYearID, selected.ID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "teaching_demands_query_failed")
		return
	}
	defer rows.Close()
	demands := []map[string]any{}
	for rows.Next() {
		var id, teacherID, cohortSubjectID, cohortID, subjectID uuid.UUID
		var teacherName, cohortName, subjectName, teacherRef, cohortSubjectRef, cohortRef, subjectRef, status string
		var requiredPeriods pgtype.Int4
		var requiredDoubles, scheduledPeriods int
		var demandSource string
		if err := rows.Scan(
			&id, &teacherID, &teacherName, &cohortSubjectID, &cohortID, &subjectID,
			&cohortName, &subjectName, &teacherRef, &cohortSubjectRef, &cohortRef, &subjectRef, &status,
			&requiredPeriods, &requiredDoubles, &demandSource, &scheduledPeriods,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "teaching_demands_scan_failed")
			return
		}
		remainingPeriods := 0
		demandStatus := "UNCONFIGURED"
		var requiredPeriodsValue *int
		if requiredPeriods.Valid {
			value := int(requiredPeriods.Int32)
			requiredPeriodsValue = &value
			remainingPeriods = max(value-scheduledPeriods, 0)
			if remainingPeriods == 0 {
				demandStatus = "COMPLETE"
			} else if scheduledPeriods > 0 {
				demandStatus = "PARTIAL"
			} else {
				demandStatus = "UNSCHEDULED"
			}
		}
		demands = append(demands, map[string]any{
			"teaching_assignment_uuid":   id,
			"teacher_uuid":               teacherID,
			"teacher_name":               teacherName,
			"teacher_ref":                teacherRef,
			"cohort_subject_uuid":        cohortSubjectID,
			"cohort_subject_ref":         cohortSubjectRef,
			"cohort_uuid":                cohortID,
			"cohort_ref":                 cohortRef,
			"cohort_name":                cohortName,
			"subject_uuid":               subjectID,
			"subject_ref":                subjectRef,
			"subject_name":               subjectName,
			"status":                     status,
			"required_periods_per_cycle": requiredPeriodsValue,
			"required_double_lessons":    requiredDoubles,
			"scheduled_periods":          scheduledPeriods,
			"remaining_periods":          remainingPeriods,
			"demand_source":              demandSource,
			"demand_status":              demandStatus,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"demands": demands,
		"count":   len(demands),
		"status":  "SYNCHRONIZED",
	})
}

func (h *Handler) TeachingDemandDetail(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	assignmentID, err := uuid.Parse(r.PathValue("assignmentId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_teaching_assignment_uuid")
		return
	}
	today, _ := time.Parse("2006-01-02", workspaceLocalDate(session.WorkspaceTimezone))
	terms, err := h.academicTerms(r.Context(), session.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "teaching_demand_save_failed")
		return
	}
	selected, err := selectTerm(terms, r.URL.Query().Get("term_uuid"), today, true)
	if err != nil || selected == nil {
		writeError(w, http.StatusBadRequest, "term_not_found")
		return
	}
	var body struct {
		RequiredPeriods *int `json:"required_periods_per_cycle"`
		RequiredDoubles *int `json:"required_double_lessons"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RequiredPeriods == nil {
		writeError(w, http.StatusBadRequest, "invalid_teaching_demand")
		return
	}
	doubles := 0
	if body.RequiredDoubles != nil {
		doubles = *body.RequiredDoubles
	}
	if *body.RequiredPeriods <= 0 || doubles < 0 || doubles*2 > *body.RequiredPeriods {
		writeError(w, http.StatusBadRequest, "invalid_teaching_demand")
		return
	}
	var exists bool
	if err := h.pool.QueryRow(r.Context(), `
		SELECT EXISTS(
		  SELECT 1
		  FROM external_teaching_assignment assignment
		  JOIN external_cohort cohort ON cohort.workspace_id = assignment.workspace_id AND cohort.id = assignment.cohort_uuid
		  WHERE assignment.id = $1 AND assignment.workspace_id = $2
		    AND assignment.status = 'ACTIVE'
		    AND cohort.academic_year_uuid = $3
		)`, assignmentID, session.WorkspaceID, selected.AcademicYearID).Scan(&exists); err != nil || !exists {
		writeError(w, http.StatusNotFound, "teaching_assignment_not_found")
		return
	}
	_, err = h.pool.Exec(r.Context(), `
		INSERT INTO timetable_demand_override (
		  id, workspace_id, academic_term_uuid, teaching_assignment_uuid,
		  weekly_lesson_requirement, required_double_lessons
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (workspace_id, academic_term_uuid, teaching_assignment_uuid)
		DO UPDATE SET weekly_lesson_requirement = EXCLUDED.weekly_lesson_requirement,
		              required_double_lessons = EXCLUDED.required_double_lessons,
		              updated_at = now()`,
		uuid.New(), session.WorkspaceID, selected.ID, assignmentID, *body.RequiredPeriods, doubles)
	if err != nil {
		writeError(w, http.StatusBadRequest, "teaching_demand_save_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "UPDATED"})
}

func (h *Handler) Availability(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	writeJSON(w, http.StatusOK, map[string]any{
		"availability": []any{},
		"count":        0,
		"status":       "USE_TEACHER_DETAIL_ENDPOINT_FOR_EDITS",
	})
}

func (h *Handler) Conflicts(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT conflict_type, severity, COUNT(*)
		FROM scheduling_conflict
		WHERE org_id = $1
		  AND resolved = false
		GROUP BY conflict_type, severity`,
		session.WorkspaceID,
	)
	if err != nil && err != pgx.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "conflicts_query_failed")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var conflictType, severity string
		var count int
		if err := rows.Scan(&conflictType, &severity, &count); err != nil {
			writeError(w, http.StatusInternalServerError, "conflicts_scan_failed")
			return
		}
		items = append(items, map[string]any{
			"conflict_type": conflictType,
			"severity":      severity,
			"count":         count,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"summary": items})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeDomainError(w, status, code, nil)
}
