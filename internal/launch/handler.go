package launch

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"scholaroscope-temporal-service/internal/protocol"

	"github.com/google/uuid"
)

const PortalSessionCookie = "temporal_portal_session"

type Handler struct {
	repo          *Repo
	launchSecret  string
	allowedSkew   time.Duration
}

func NewHandler(repo *Repo, launchSecret string, allowedSkew time.Duration) *Handler {
	return &Handler{repo: repo, launchSecret: launchSecret, allowedSkew: allowedSkew}
}

func (h *Handler) Exchange(w http.ResponseWriter, r *http.Request) {
	if h.launchSecret == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]string{"code": "launch_secret_not_configured"}})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_body"}})
		return
	}
	timestamp := r.Header.Get(protocol.TimestampHeader)
	if err := protocol.VerifyTimestamp(timestamp, time.Now().UTC(), h.allowedSkew); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "expired_timestamp"}})
		return
	}
	if err := protocol.VerifySignature(h.launchSecret, timestamp, body, r.Header.Get(protocol.SignatureHeader)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "invalid_signature"}})
		return
	}
	var payload GrantPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_payload"}})
		return
	}
	if payload.Issuer != "scholaroscope" || payload.Audience != "scholaroscope-temporal-service" || payload.Purpose != "TIMETABLE_MANAGEMENT" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "invalid_launch_claims"}})
		return
	}
	expiresAt, err := time.Parse(time.RFC3339, payload.ExpiresAt)
	if err != nil || !expiresAt.After(time.Now().UTC()) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "expired_launch_grant"}})
		return
	}
	session, err := h.repo.ConsumeGrant(r.Context(), payload, protocolHash(body))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "launch_grant_rejected"}})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     PortalSessionCookie,
		Value:    session.ID.String(),
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "launched",
		"workspace_uuid": session.WorkspaceID,
		"expires_at":     session.ExpiresAt,
	})
}

func (h *Handler) Session(w http.ResponseWriter, r *http.Request) {
	session, ok := h.currentSession(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_uuid": session.WorkspaceID,
		"actor_uuid":     session.ActorID,
		"expires_at":     session.ExpiresAt,
	})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(PortalSessionCookie)
	if err == nil {
		if sessionID, parseErr := uuid.Parse(cookie.Value); parseErr == nil {
			_ = h.repo.RevokeSession(r.Context(), sessionID)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     PortalSessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *Handler) currentSession(w http.ResponseWriter, r *http.Request) (*PortalSession, bool) {
	cookie, err := r.Cookie(PortalSessionCookie)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "portal_session_required"}})
		return nil, false
	}
	sessionID, err := uuid.Parse(cookie.Value)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "invalid_portal_session"}})
		return nil, false
	}
	session, err := h.repo.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "invalid_portal_session"}})
		return nil, false
	}
	return session, true
}

func protocolHash(body []byte) string {
	return protocol.PayloadHash(body)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
