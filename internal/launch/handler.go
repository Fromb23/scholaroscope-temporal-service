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
	sessionTTL    time.Duration
	cookieSecure  bool
}

func NewHandler(repo *Repo, launchSecret string, allowedSkew time.Duration, sessionTTL time.Duration, cookieSecure bool) *Handler {
	return &Handler{
		repo: repo,
		launchSecret: launchSecret,
		allowedSkew: allowedSkew,
		sessionTTL: sessionTTL,
		cookieSecure: cookieSecure,
	}
}

func (h *Handler) Exchange(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_body"}})
		return
	}
	var payload GrantPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_payload"}})
		return
	}
	secret, err := h.repo.InstallationSecret(r.Context(), payload.PluginInstallationRef)
	if err != nil || secret == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "unknown_or_inactive_installation"}})
		return
	}
	timestamp := r.Header.Get(protocol.TimestampHeader)
	if err := protocol.VerifyTimestamp(timestamp, time.Now().UTC(), h.allowedSkew); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "expired_timestamp"}})
		return
	}
	if err := protocol.VerifySignature(secret, timestamp, body, r.Header.Get(protocol.SignatureHeader)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "invalid_signature"}})
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
	sessionExpiresAt := time.Now().UTC().Add(h.sessionTTL)
	session, err := h.repo.ConsumeGrant(r.Context(), payload, protocolHash(body), sessionExpiresAt)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "launch_grant_rejected"}})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     PortalSessionCookie,
		Value:    session.ID.String(),
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"status":             "launched",
		"workspace_uuid":     session.WorkspaceID,
		"workspace_name":     session.WorkspaceName,
		"workspace_timezone": session.WorkspaceTimezone,
		"actor_uuid":         session.ActorID,
		"actor_display_name": session.ActorDisplayName,
		"actor_kind":         session.ActorKind,
		"expires_at":         session.ExpiresAt,
	})
}

func (h *Handler) Session(w http.ResponseWriter, r *http.Request) {
	session, ok := h.currentSession(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_uuid":     session.WorkspaceID,
		"workspace_name":     session.WorkspaceName,
		"workspace_timezone": session.WorkspaceTimezone,
		"actor_uuid":         session.ActorID,
		"actor_display_name": session.ActorDisplayName,
		"actor_kind":         session.ActorKind,
		"permissions":        permissionKeys(session.PermissionSnapshot),
		"expires_at":         session.ExpiresAt,
	})
}

func (h *Handler) RequirePortalPermission(permission string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := h.currentSession(w, r)
		if !ok {
			return
		}
		orgID, err := uuid.Parse(r.PathValue("orgId"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_workspace_uuid"}})
			return
		}
		if orgID != session.WorkspaceID {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]string{"code": "workspace_scope_mismatch"}})
			return
		}
		if !hasPermission(session.PermissionSnapshot, permission) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]string{"code": "permission_denied"}})
			return
		}
		next(w, r)
	}
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
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *Handler) RequirePortalSession(permission string, next func(http.ResponseWriter, *http.Request, *PortalSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := h.currentSession(w, r)
		if !ok {
			return
		}
		if permission != "" && !hasPermission(session.PermissionSnapshot, permission) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]string{"code": "permission_denied"}})
			return
		}
		next(w, r, session)
	}
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

func hasPermission(snapshot map[string]any, required string) bool {
	for _, key := range permissionKeys(snapshot) {
		if key == required {
			return true
		}
	}
	return false
}

func permissionKeys(snapshot map[string]any) []string {
	raw, ok := snapshot["permission_keys"]
	if !ok {
		return nil
	}
	keys := []string{}
	switch typed := raw.(type) {
	case []any:
		for _, item := range typed {
			if key, ok := item.(string); ok {
				keys = append(keys, key)
			}
		}
	case []string:
		keys = append(keys, typed...)
	}
	return keys
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
