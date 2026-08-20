package portalapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"scholaroscope-temporal-service/internal/calendar"
	"scholaroscope-temporal-service/internal/launch"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	if err := h.rebuildConflictsForVersion(r.Context(), session.WorkspaceID, versionID); err != nil {
		writeError(w, http.StatusInternalServerError, "validation_failed")
		return
	}
	summary, err := h.conflictSummaryForVersion(r.Context(), session.WorkspaceID, versionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "validation_failed")
		return
	}
	status := "VALIDATED"
	if summary.HardConflicts > 0 {
		status = "BLOCKED"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": status,
		"hard_conflicts": summary.HardConflicts,
		"soft_conflicts": summary.SoftConflicts,
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
	if err := h.rebuildConflictsForVersion(ctx, session.WorkspaceID, versionID); err != nil {
		return nil, err
	}
	summary, err := h.conflictSummaryForVersion(ctx, session.WorkspaceID, versionID)
	if err != nil {
		return nil, err
	}
	if summary.HardConflicts > 0 {
		return nil, errCode("hard_conflicts_block_publication")
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
		       ) AS previous_version_id
		FROM timetable_version tv
		WHERE tv.id = $1
		  AND tv.workspace_id = $2
		  AND tv.status IN ('DRAFT', 'VALIDATED', 'PUBLISHED')`,
		versionID,
		session.WorkspaceID,
	).Scan(&timetableID, &versionNumber, &effectiveStart, &effectiveEnd, &previousVersionID)
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
				"change_type": "ENTRY_ADDED",
				"stable_entry_uuid": entry["stable_entry_uuid"],
				"after": entry,
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
	)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]any{
		"status": "PUBLISHED",
		"version_uuid": versionID,
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
		       COALESCE(eta.subject_ref, ''), COALESCE(eta.cohort_subject_ref, '')
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
		if err := rows.Scan(
			&entryID, &stableID, &teacherID, &teacherName,
			&cohortID, &cohortName,
			&subjectID, &subjectName, &subjectCode,
			&cohortSubjectID, &roomID, &roomName,
			&day, &startTime, &endTime,
			&duration, &startPeriod,
			&teacherRef, &cohortRef, &subjectRef, &cohortSubjectRef,
		); err != nil {
			return nil, err
		}
		entries = append(entries, map[string]any{
			"entry_uuid": entryID.String(),
			"stable_entry_uuid": stableID.String(),
			"teacher_uuid": uuidString(teacherID),
			"teacher_name": teacherName,
			"teacher_ref": teacherRef,
			"cohort_uuid": uuidString(cohortID),
			"cohort_name": cohortName,
			"cohort_ref": cohortRef,
			"subject_uuid": uuidString(subjectID),
			"subject_name": subjectName,
			"subject_code": subjectCode,
			"subject_ref": subjectRef,
			"cohort_subject_uuid": uuidString(cohortSubjectID),
			"cohort_subject_ref": cohortSubjectRef,
			"room_uuid": uuidString(roomID),
			"room_name": roomName,
			"day_of_week": dayName(day),
			"start_time": startTime,
			"end_time": endTime,
			"duration_periods": duration,
			"start_period_index": startPeriod,
		})
	}
	return entries, rows.Err()
}

func uuidString(value *uuid.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func dayName(day int) string {
	names := []string{"SUNDAY", "MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY"}
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
		ID                    uuid.UUID  `json:"workspace_uuid"`
		DisplayName           string     `json:"display_name"`
		Timezone              string     `json:"timezone"`
		Status                string     `json:"status"`
		ProvisioningState     string     `json:"provisioning_state"`
		IntegrationHealth     string     `json:"integration_health"`
		LastSuccessfulSyncAt  *time.Time `json:"last_successful_sync_at"`
		ReconciliationRequired bool      `json:"reconciliation_required"`
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
	writeJSON(w, http.StatusOK, workspace)
}

func (h *Handler) GetCalendar(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	version, err := h.calendarService.GetActiveCalendar(r.Context(), session.WorkspaceID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"calendar_version": nil,
			"slots": []any{},
			"status": "NO_ACTIVE_CALENDAR",
		})
		return
	}
	slots, _ := h.calendarService.GetSlotsForVersion(r.Context(), version.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"calendar_version": version,
		"slots": slots,
		"status": "ACTIVE",
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
	version, slots, err := h.calendarService.CreateCalendarWithSlots(r.Context(), session.WorkspaceID, &calendar.CreateCalendarInput{
		LearningDays:        body.LearningDays,
		DayStartTime:        startTime,
		DayEndTime:          endTime,
		SlotDurationMinutes: body.SlotDurationMinutes,
		BreakStructure:      body.BreakStructure,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "calendar_validation_failed")
		return
	}
	_ = h.calendarService.ActivateCalendar(r.Context(), session.WorkspaceID, version.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"calendar_version": version,
		"slots": slots,
		"status": "ACTIVE",
	})
}

func (h *Handler) Teachers(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, scholaroscope_user_ref, display_name, actor_kind, status
		FROM external_actor
		WHERE workspace_id = $1
		  AND status = 'ACTIVE'
		ORDER BY display_name`,
		session.WorkspaceID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "teachers_query_failed")
		return
	}
	defer rows.Close()
	actors := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var ref, name, kind, status string
		if err := rows.Scan(&id, &ref, &name, &kind, &status); err != nil {
			writeError(w, http.StatusInternalServerError, "teachers_scan_failed")
			return
		}
		actors = append(actors, map[string]any{
			"actor_uuid": id,
			"scholaroscope_user_ref": ref,
			"display_name": name,
			"actor_kind": kind,
			"status": status,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"teachers": actors, "count": len(actors)})
}

func (h *Handler) Rooms(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	if r.Method == http.MethodPost {
		h.createRoom(w, r, session)
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, external_ref, name, capacity, exclusive, status
		FROM room
		WHERE workspace_id = $1
		ORDER BY name`,
		session.WorkspaceID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "rooms_query_failed")
		return
	}
	defer rows.Close()
	rooms := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var externalRef *string
		var name, status string
		var capacity *int
		var exclusive bool
		if err := rows.Scan(&id, &externalRef, &name, &capacity, &exclusive, &status); err != nil {
			writeError(w, http.StatusInternalServerError, "rooms_scan_failed")
			return
		}
		rooms = append(rooms, map[string]any{
			"room_uuid": id,
			"external_ref": externalRef,
			"name": name,
			"capacity": capacity,
			"exclusive": exclusive,
			"status": status,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms, "count": len(rooms)})
}

func (h *Handler) createRoom(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	var body struct {
		Name     string `json:"name"`
		Capacity *int  `json:"capacity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_room")
		return
	}
	id := uuid.New()
	_, err := h.pool.Exec(r.Context(), `
		INSERT INTO room (id, workspace_id, name, capacity)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (workspace_id, name)
		DO UPDATE SET capacity = EXCLUDED.capacity, updated_at = now()`,
		id,
		session.WorkspaceID,
		body.Name,
		body.Capacity,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "room_save_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "saved"})
}

func (h *Handler) Timetables(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	if r.Method == http.MethodPost {
		h.createTimetable(w, r, session)
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT t.id, t.name, t.timetable_type, tv.id, tv.version_number, tv.status,
		       tv.effective_start, tv.effective_end, tv.published_at
		FROM timetable t
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
		var name, timetableType string
		var versionID *uuid.UUID
		var versionNumber *int
		var versionStatus *string
		var effectiveStart, effectiveEnd *time.Time
		var publishedAt *time.Time
		if err := rows.Scan(&timetableID, &name, &timetableType, &versionID, &versionNumber, &versionStatus, &effectiveStart, &effectiveEnd, &publishedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "timetables_scan_failed")
			return
		}
		items = append(items, map[string]any{
			"timetable_uuid": timetableID,
			"name": name,
			"type": timetableType,
			"version_uuid": versionID,
			"version_number": versionNumber,
			"status": versionStatus,
			"effective_start": effectiveStart,
			"effective_end": effectiveEnd,
			"published_at": publishedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"timetables": items, "count": len(items)})
}

func (h *Handler) createTimetable(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	var body struct {
		Name             string `json:"name"`
		AcademicTermUUID string `json:"academic_term_uuid"`
		CalendarUUID     string `json:"calendar_uuid"`
		EffectiveStart   string `json:"effective_start"`
		EffectiveEnd     string `json:"effective_end"`
		ScopeKind        string `json:"scope_kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_timetable")
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
	if body.AcademicTermUUID != "" {
		parsed, err := uuid.Parse(body.AcademicTermUUID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_academic_term_uuid")
			return
		}
		termID = &parsed
	}
	var calendarID *uuid.UUID
	if body.CalendarUUID != "" {
		parsed, err := uuid.Parse(body.CalendarUUID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_calendar_uuid")
			return
		}
		calendarID = &parsed
	}
	scopeKind := body.ScopeKind
	if scopeKind == "" {
		scopeKind = "WORKSPACE"
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
		VALUES ($1, $2, $3, $4, 'LEARNING', $5, $6, $7, $8)`,
		timetableID, session.WorkspaceID, calendarID, termID, scopeKind, body.Name, start, end,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "timetable_create_failed")
		return
	}
	_, err = tx.Exec(r.Context(), `
		INSERT INTO timetable_version (id, workspace_id, timetable_id, version_number, status, effective_start, effective_end)
		VALUES ($1, $2, $3, 1, 'DRAFT', $4, $5)`,
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
		"version_uuid": versionID,
		"status": "DRAFT",
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
	err = h.pool.QueryRow(r.Context(), `
		INSERT INTO timetable_version (id, workspace_id, timetable_id, version_number, status, derived_from_version_id, effective_start, effective_end)
		SELECT $1, workspace_id, id,
		       COALESCE((SELECT MAX(version_number) + 1 FROM timetable_version WHERE workspace_id = t.workspace_id AND timetable_id = t.id), 1),
		       'DRAFT',
		       (SELECT id FROM timetable_version WHERE workspace_id = t.workspace_id AND timetable_id = t.id ORDER BY version_number DESC LIMIT 1),
		       effective_start, effective_end
		FROM timetable t
		WHERE t.id = $2 AND t.workspace_id = $3
		RETURNING version_number`,
		versionID, timetableID, session.WorkspaceID,
	).Scan(&versionNumber)
	if err != nil {
		writeError(w, http.StatusBadRequest, "version_create_failed")
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
	var start, end time.Time
	err = tx.QueryRow(r.Context(), `
		SELECT timetable_id, version_number, status, effective_start, effective_end
		FROM timetable_version
		WHERE id = $1 AND workspace_id = $2`,
		versionID, session.WorkspaceID,
	).Scan(&timetableID, &versionNumber, &versionStatus, &start, &end)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"version_uuid": versionID,
		"timetable_uuid": timetableID,
		"version_number": versionNumber,
		"status": versionStatus,
		"effective_start": start,
		"effective_end": end,
		"entries": entries,
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
	err = h.pool.QueryRow(r.Context(), `
		SELECT stable_entry_uuid FROM timetable_entry WHERE id = $1 AND workspace_id = $2 AND timetable_version_id = $3`,
		entryID, session.WorkspaceID, versionID,
	).Scan(&stableID)
	if err != nil {
		writeError(w, http.StatusNotFound, "entry_not_found")
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
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM external_actor WHERE workspace_id = $1 AND id = $2 AND status = 'ACTIVE'`, workspaceID, teacherID).Scan(&count); err != nil || count != 1 {
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
	rows, err := h.pool.Query(r.Context(), `
		SELECT eta.id, eta.teacher_uuid, COALESCE(a.display_name, ''),
		       eta.cohort_subject_uuid, eta.cohort_uuid, eta.subject_uuid,
		       eta.cohort_name, eta.subject_name, eta.teacher_ref,
		       eta.cohort_subject_ref, eta.cohort_ref, eta.subject_ref, eta.status
		FROM external_teaching_assignment eta
		JOIN external_actor a ON a.workspace_id = eta.workspace_id AND a.id = eta.teacher_uuid
		WHERE eta.workspace_id = $1
		  AND eta.status = 'ACTIVE'
		ORDER BY eta.cohort_name, eta.subject_name, a.display_name`,
		session.WorkspaceID,
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
		if err := rows.Scan(
			&id, &teacherID, &teacherName, &cohortSubjectID, &cohortID, &subjectID,
			&cohortName, &subjectName, &teacherRef, &cohortSubjectRef, &cohortRef, &subjectRef, &status,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "teaching_demands_scan_failed")
			return
		}
		demands = append(demands, map[string]any{
			"teaching_assignment_uuid": id,
			"teacher_uuid": teacherID,
			"teacher_name": teacherName,
			"teacher_ref": teacherRef,
			"cohort_subject_uuid": cohortSubjectID,
			"cohort_subject_ref": cohortSubjectRef,
			"cohort_uuid": cohortID,
			"cohort_ref": cohortRef,
			"cohort_name": cohortName,
			"subject_uuid": subjectID,
			"subject_ref": subjectRef,
			"subject_name": subjectName,
			"status": status,
			"required_periods_per_cycle": 1,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"demands": demands,
		"count": len(demands),
		"status": "SYNCHRONIZED",
	})
}

func (h *Handler) Availability(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	writeJSON(w, http.StatusOK, map[string]any{
		"availability": []any{},
		"count": 0,
		"status": "USE_TEACHER_DETAIL_ENDPOINT_FOR_EDITS",
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
			"severity": severity,
			"count": count,
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
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code}})
}
