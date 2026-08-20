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
	err = tx.QueryRow(ctx, `
		SELECT timetable_id, version_number, effective_start, effective_end
		FROM timetable_version
		WHERE id = $1
		  AND workspace_id = $2
		  AND status IN ('DRAFT', 'VALIDATED', 'PUBLISHED')`,
		versionID,
		session.WorkspaceID,
	).Scan(&timetableID, &versionNumber, &effectiveStart, &effectiveEnd)
	if err != nil {
		return nil, errCode("version_not_publishable")
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
		VALUES ($1, $2, $3, 'temporal.timetable.learning.published.v1', '1.0',
		        'TIMETABLE_VERSION', $4, $5, $6, $7,
		        jsonb_build_object(
		          'version_uuid', $4::text,
		          'timetable_uuid', $8::text,
		          'version_label', $9::text,
		          'effective_from', $10::text,
		          'effective_until', $11::text,
		          'publication_reason', $12::text,
		          'entries', '[]'::jsonb,
		          'diff', '[]'::jsonb
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

func (h *Handler) TeachingDemands(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	writeJSON(w, http.StatusOK, map[string]any{
		"demands": []any{},
		"count": 0,
		"status": "AWAITING_ACADEMIC_SYNC",
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
