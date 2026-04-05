package scheduling

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// POST /orgs/{orgId}/sessions/{sessionId}/schedule
func (h *Handler) ScheduleSession(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(r.PathValue("orgId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org id")
		return
	}
	sessionID, err := uuid.Parse(r.PathValue("sessionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}

	var body struct {
		CalendarVersionID string `json:"calendar_version_id"`
		TeacherID         string `json:"teacher_id"`
		CohortSubjectID   string `json:"cohort_subject_id"`
		DurationSlots     int16  `json:"duration_slots"`
		ScheduleMode      string `json:"schedule_mode"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	calendarVersionID, err := uuid.Parse(body.CalendarVersionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid calendar_version_id")
		return
	}
	teacherID, err := uuid.Parse(body.TeacherID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid teacher_id")
		return
	}
	cohortSubjectID, err := uuid.Parse(body.CohortSubjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cohort_subject_id")
		return
	}

	mode := ScheduleMode(body.ScheduleMode)
	if mode == "" {
		mode = ScheduleModeLearning
	}
	if body.DurationSlots <= 0 {
		body.DurationSlots = 1
	}

	ss, err := h.service.Schedule(r.Context(), &ScheduleRequest{
		OrgID:             orgID,
		SessionID:         sessionID,
		CalendarVersionID: calendarVersionID,
		TeacherID:         teacherID,
		CohortSubjectID:   cohortSubjectID,
		DurationSlots:     body.DurationSlots,
		ScheduleMode:      mode,
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, ss)
}

// DELETE /orgs/{orgId}/sessions/{sessionId}/schedule
func (h *Handler) UnscheduleSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(r.PathValue("sessionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}

	if err := h.service.Unschedule(r.Context(), sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "unscheduled"})
}

// GET /orgs/{orgId}/calendar/{versionId}/timetable
func (h *Handler) GetTimetable(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(r.PathValue("orgId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org id")
		return
	}
	versionID, err := uuid.Parse(r.PathValue("versionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid version id")
		return
	}

	sessions, err := h.service.repo.ListScheduledSessions(r.Context(), orgID, versionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
