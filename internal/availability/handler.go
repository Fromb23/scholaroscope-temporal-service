package availability

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

type Handler struct {
	repo *Repo
}

func NewHandler(repo *Repo) *Handler {
	return &Handler{repo: repo}
}

// PUT /orgs/{orgId}/teachers/{teacherId}/availability
// Body: [{"timeslot_id": "...", "available": true}, ...]
func (h *Handler) SetAvailability(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(r.PathValue("orgId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org id")
		return
	}
	teacherID, err := uuid.Parse(r.PathValue("teacherId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid teacher id")
		return
	}

	var body []struct {
		TimeslotID string `json:"timeslot_id"`
		Available  bool   `json:"available"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	inputs := make([]SetAvailabilityInput, 0, len(body))
	for _, item := range body {
		timeslotID, err := uuid.Parse(item.TimeslotID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid timeslot_id: "+item.TimeslotID)
			return
		}
		inputs = append(inputs, SetAvailabilityInput{
			OrgID:      orgID,
			TeacherID:  teacherID,
			TimeslotID: timeslotID,
			Available:  item.Available,
		})
	}

	if err := h.repo.BulkUpsert(r.Context(), inputs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "updated",
		"records": len(inputs),
	})
}

// GET /orgs/{orgId}/teachers/{teacherId}/availability
func (h *Handler) GetAvailability(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(r.PathValue("orgId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org id")
		return
	}
	teacherID, err := uuid.Parse(r.PathValue("teacherId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid teacher id")
		return
	}

	records, err := h.repo.ListForTeacher(r.Context(), orgID, teacherID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"availability": records,
		"count":        len(records),
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
