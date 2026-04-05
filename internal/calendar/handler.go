package calendar

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// POST /orgs/{orgId}/calendar
func (h *Handler) CreateCalendar(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(r.PathValue("orgId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org id")
		return
	}

	var body struct {
		LearningDays        []string      `json:"learning_days"`
		DayStartTime        string        `json:"day_start_time"`  // "08:00"
		DayEndTime          string        `json:"day_end_time"`    // "17:00"
		SlotDurationMinutes int16         `json:"slot_duration_minutes"`
		BreakStructure      []BreakWindow `json:"break_structure"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	startTime, err := time.Parse("15:04", body.DayStartTime)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid day_start_time, use HH:MM")
		return
	}
	endTime, err := time.Parse("15:04", body.DayEndTime)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid day_end_time, use HH:MM")
		return
	}

	version, slots, err := h.service.CreateCalendarWithSlots(r.Context(), orgID, &CreateCalendarInput{
		LearningDays:        body.LearningDays,
		DayStartTime:        startTime,
		DayEndTime:          endTime,
		SlotDurationMinutes: body.SlotDurationMinutes,
		BreakStructure:      body.BreakStructure,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"calendar_version": version,
		"slots_generated":  len(slots),
	})
}

// GET /orgs/{orgId}/calendar/active
func (h *Handler) GetActiveCalendar(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(r.PathValue("orgId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org id")
		return
	}

	version, err := h.service.repo.GetActiveCalendarVersion(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no active calendar found")
		return
	}

	writeJSON(w, http.StatusOK, version)
}

// POST /orgs/{orgId}/calendar/{versionId}/activate
func (h *Handler) ActivateCalendar(w http.ResponseWriter, r *http.Request) {
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

	if err := h.service.repo.ActivateCalendarVersion(r.Context(), orgID, versionID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "activated"})
}

// GET /orgs/{orgId}/calendar/{versionId}/slots
func (h *Handler) GetTimeSlots(w http.ResponseWriter, r *http.Request) {
	versionID, err := uuid.Parse(r.PathValue("versionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid version id")
		return
	}

	slots, err := h.service.repo.GetTimeSlotsForVersion(r.Context(), versionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"slots": slots,
		"count": len(slots),
	})
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
