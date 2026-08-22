package portalapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"scholaroscope-temporal-service/internal/launch"

	"github.com/google/uuid"
)

type academicTerm struct {
	ID                uuid.UUID
	AcademicYearID    *uuid.UUID
	AcademicYearRef   string
	Name              string
	AcademicYearLabel string
	StartDate         time.Time
	EndDate           time.Time
	Status            string
	CalendarReady     bool
	Frozen            bool
}

type applicableException struct {
	ID              uuid.UUID
	AcademicYearID  *uuid.UUID
	TermID          *uuid.UUID
	Title           string
	Kind            string
	StartDate       time.Time
	EndDate         time.Time
	AffectsLearning bool
	Source          string
}

func exceptionApplies(item applicableException, workspaceYearID, termID uuid.UUID, effectiveStart, effectiveEnd time.Time) bool {
	return item.AcademicYearID != nil && *item.AcademicYearID == workspaceYearID &&
		item.TermID != nil && *item.TermID == termID &&
		!item.StartDate.After(effectiveEnd) && !item.EndDate.Before(effectiveStart)
}

func (h *Handler) applicableCalendarExceptions(ctx context.Context, workspaceID, academicYearID, termID uuid.UUID, effectiveStart, effectiveEnd time.Time) ([]applicableException, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT id, academic_year_uuid, term_uuid, title, event_kind, starts_on,
		       ends_on, affects_learning, source
		FROM external_calendar_event
		WHERE workspace_id = $1
		  AND academic_year_uuid = $2
		  AND term_uuid = $3
		  AND status = 'ACTIVE'
		  AND starts_on <= $5
		  AND ends_on >= $4
		ORDER BY starts_on, ends_on, title`, workspaceID, academicYearID, termID, effectiveStart, effectiveEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []applicableException{}
	for rows.Next() {
		var item applicableException
		if err := rows.Scan(&item.ID, &item.AcademicYearID, &item.TermID, &item.Title, &item.Kind, &item.StartDate, &item.EndDate, &item.AffectsLearning, &item.Source); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func termLifecycle(term academicTerm, today time.Time) string {
	status := strings.ToUpper(strings.TrimSpace(term.Status))
	day := today.UTC().Truncate(24 * time.Hour)
	if term.Frozen {
		return "UNAVAILABLE"
	}
	if status == "CLOSED_HISTORICAL" || status == "ENDED_GRACE_PERIOD" || status == "CLOSING" || term.EndDate.Before(day) {
		return "ENDED"
	}
	if term.StartDate.After(day) {
		if status == "OPEN" {
			return "UPCOMING"
		}
		return "UNAVAILABLE"
	}
	if !term.EndDate.Before(day) && status == "OPEN" {
		return "ACTIVE"
	}
	return "UNAVAILABLE"
}

func termPayload(term academicTerm, today time.Time) map[string]any {
	lifecycle := termLifecycle(term, today)
	return map[string]any{
		"term_uuid":            term.ID.String(),
		"academic_year_uuid":   uuidString(term.AcademicYearID),
		"name":                 term.Name,
		"academic_year_label":  term.AcademicYearLabel,
		"start_date":           term.StartDate.Format("2006-01-02"),
		"end_date":             term.EndDate.Format("2006-01-02"),
		"lifecycle":            lifecycle,
		"scheduling_permitted": lifecycle == "ACTIVE" || lifecycle == "UPCOMING",
		"calendar_ready":       term.CalendarReady,
		"is_current":           lifecycle == "ACTIVE",
	}
}

func (h *Handler) academicTerms(ctx context.Context, workspaceID uuid.UUID) ([]academicTerm, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT id, academic_year_uuid, scholaroscope_academic_year_ref, name,
		       academic_year_label, start_date, end_date, status, calendar_ready, is_frozen
		FROM external_academic_term
		WHERE workspace_id = $1
		ORDER BY start_date, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	terms := []academicTerm{}
	for rows.Next() {
		var term academicTerm
		if err := rows.Scan(&term.ID, &term.AcademicYearID, &term.AcademicYearRef, &term.Name, &term.AcademicYearLabel, &term.StartDate, &term.EndDate, &term.Status, &term.CalendarReady, &term.Frozen); err != nil {
			return nil, err
		}
		terms = append(terms, term)
	}
	return terms, rows.Err()
}

func selectTerm(terms []academicTerm, selected string, today time.Time, allowHistorical bool) (*academicTerm, error) {
	if strings.TrimSpace(selected) != "" {
		selectedID, err := uuid.Parse(selected)
		if err != nil {
			return nil, errCode("term_not_found")
		}
		for index := range terms {
			if terms[index].ID == selectedID {
				lifecycle := termLifecycle(terms[index], today)
				if !allowHistorical && lifecycle != "ACTIVE" && lifecycle != "UPCOMING" {
					return nil, errCode("term_not_schedulable")
				}
				return &terms[index], nil
			}
		}
		return nil, errCode("term_not_found")
	}
	for index := range terms {
		if termLifecycle(terms[index], today) == "ACTIVE" {
			return &terms[index], nil
		}
	}
	return nil, errCode("active_term_not_found")
}

func (h *Handler) AcademicContext(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	today, _ := time.Parse("2006-01-02", workspaceLocalDate(session.WorkspaceTimezone))
	terms, err := h.academicTerms(r.Context(), session.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "academic_context_query_failed")
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, name, start_date, end_date, is_current, status, curriculum_name
		FROM external_academic_year
		WHERE workspace_id = $1
		ORDER BY start_date, id`, session.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "academic_context_query_failed")
		return
	}
	defer rows.Close()
	years := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, status, curriculum string
		var start, end time.Time
		var current bool
		if err := rows.Scan(&id, &name, &start, &end, &current, &status, &curriculum); err != nil {
			writeError(w, http.StatusInternalServerError, "academic_context_scan_failed")
			return
		}
		lifecycle := "AVAILABLE"
		if current {
			lifecycle = "CURRENT"
		} else if end.Before(today) {
			lifecycle = "ENDED"
		} else if start.After(today) {
			lifecycle = "UPCOMING"
		}
		yearTerms := []map[string]any{}
		for _, term := range terms {
			if term.AcademicYearID != nil && *term.AcademicYearID == id {
				yearTerms = append(yearTerms, termPayload(term, today))
			}
		}
		years = append(years, map[string]any{
			"academic_year_uuid": id.String(), "name": name,
			"start_date": start.Format("2006-01-02"), "end_date": end.Format("2006-01-02"),
			"is_current": current, "lifecycle": lifecycle, "status": status,
			"curriculum_name": curriculum, "terms": yearTerms,
		})
	}
	selected, selectErr := selectTerm(terms, r.URL.Query().Get("term_uuid"), today, true)
	if selectErr != nil && strings.TrimSpace(r.URL.Query().Get("term_uuid")) != "" {
		writeError(w, http.StatusBadRequest, selectErr.Error())
		return
	}
	var selectedPayload map[string]any
	if selectErr == nil && selected != nil {
		selectedPayload = termPayload(*selected, today)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"academic_years":  years,
		"selected_term":   selectedPayload,
		"has_active_term": selected != nil && termLifecycle(*selected, today) == "ACTIVE",
	})
}

func (h *Handler) ClassesSpaces(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	today, _ := time.Parse("2006-01-02", workspaceLocalDate(session.WorkspaceTimezone))
	terms, err := h.academicTerms(r.Context(), session.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "classes_query_failed")
		return
	}
	selected, err := selectTerm(terms, r.URL.Query().Get("term_uuid"), today, true)
	if err != nil && strings.TrimSpace(r.URL.Query().Get("term_uuid")) != "" {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var academicYearID *uuid.UUID
	if err == nil && selected != nil {
		academicYearID = selected.AcademicYearID
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT c.id, c.name, c.level, c.stream, c.enrollment_count, c.status,
		       c.default_room_id, COALESCE(room.name, ''), room.capacity,
		       COALESCE(room.exclusive, false), COALESCE(room.status, '')
		FROM external_cohort c
		LEFT JOIN room ON room.id = c.default_room_id AND room.workspace_id = c.workspace_id
		WHERE c.workspace_id = $1
		  AND c.status = 'ACTIVE'
		  AND ($2::uuid IS NULL OR c.academic_year_uuid = $2::uuid)
		ORDER BY c.name`, session.WorkspaceID, academicYearID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "classes_query_failed")
		return
	}
	defer rows.Close()
	classes := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var roomID *uuid.UUID
		var name, level, stream, status, roomName, roomStatus string
		var enrollment int
		var roomCapacity *int
		var roomExclusive bool
		if err := rows.Scan(&id, &name, &level, &stream, &enrollment, &status, &roomID, &roomName, &roomCapacity, &roomExclusive, &roomStatus); err != nil {
			writeError(w, http.StatusInternalServerError, "classes_scan_failed")
			return
		}
		capacityMismatch := roomCapacity != nil && enrollment > *roomCapacity
		classes = append(classes, map[string]any{
			"cohort_uuid": id.String(), "name": name, "level": level, "stream": stream,
			"enrollment_count": enrollment, "status": status,
			"default_room_uuid": uuidString(roomID), "default_room_name": roomName,
			"default_room_capacity": roomCapacity, "default_room_exclusive": roomExclusive,
			"default_room_status": roomStatus, "capacity_mismatch": capacityMismatch,
		})
	}
	spaces, spaceErr := h.listRooms(r.Context(), session.WorkspaceID)
	if spaceErr != nil {
		writeError(w, http.StatusInternalServerError, "rooms_query_failed")
		return
	}
	var contextPayload map[string]any
	if selected != nil {
		contextPayload = termPayload(*selected, today)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"academic_context": contextPayload,
		"classes":          classes, "class_count": len(classes),
		"spaces": spaces, "rooms": spaces, "space_count": len(spaces),
	})
}

func (h *Handler) ClassDefaultRoom(w http.ResponseWriter, r *http.Request, session *launch.PortalSession) {
	cohortID, err := uuid.Parse(r.PathValue("cohortId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_cohort_uuid")
		return
	}
	var body struct {
		RoomUUID string `json:"room_uuid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_room_uuid")
		return
	}
	var roomID *uuid.UUID
	if strings.TrimSpace(body.RoomUUID) != "" {
		parsed, err := uuid.Parse(body.RoomUUID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_room_uuid")
			return
		}
		var exclusive bool
		if h.pool.QueryRow(r.Context(), `SELECT exclusive FROM room WHERE id = $1 AND workspace_id = $2 AND status = 'ACTIVE'`, parsed, session.WorkspaceID).Scan(&exclusive) != nil {
			writeError(w, http.StatusBadRequest, "reference_scope_mismatch")
			return
		}
		if exclusive {
			var assignedTo string
			err := h.pool.QueryRow(r.Context(), `
				SELECT name
				FROM external_cohort
				WHERE workspace_id = $1
				  AND default_room_id = $2
				  AND id <> $3
				  AND status = 'ACTIVE'
				ORDER BY name
				LIMIT 1`, session.WorkspaceID, parsed, cohortID).Scan(&assignedTo)
			if err == nil {
				writeDomainError(w, http.StatusConflict, "exclusive_room_already_assigned", map[string]any{"assigned_class": assignedTo})
				return
			}
		}
		roomID = &parsed
	}
	tag, err := h.pool.Exec(r.Context(), `UPDATE external_cohort SET default_room_id = $1, updated_at = now() WHERE id = $2 AND workspace_id = $3 AND status = 'ACTIVE'`, roomID, cohortID, session.WorkspaceID)
	if err != nil || tag.RowsAffected() != 1 {
		writeError(w, http.StatusNotFound, "class_not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "UPDATED"})
}
