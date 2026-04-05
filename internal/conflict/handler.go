package conflict

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

// GET /orgs/{orgId}/calendar/{versionId}/conflicts
func (h *Handler) ListUnresolved(w http.ResponseWriter, r *http.Request) {
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

	conflicts, err := h.repo.ListUnresolved(r.Context(), orgID, versionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"conflicts": conflicts,
		"count":     len(conflicts),
	})
}

// POST /orgs/{orgId}/conflicts/{conflictId}/resolve
func (h *Handler) Resolve(w http.ResponseWriter, r *http.Request) {
	conflictID, err := uuid.Parse(r.PathValue("conflictId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid conflict id")
		return
	}

	if err := h.repo.MarkResolved(r.Context(), conflictID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "resolved"})
}

// GET /orgs/{orgId}/conflicts/summary
// Returns conflict counts grouped by type for dashboard use.
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(r.PathValue("orgId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org id")
		return
	}

	summary, err := h.repo.SummaryByType(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
