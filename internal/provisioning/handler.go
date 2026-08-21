package provisioning

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"scholaroscope-temporal-service/internal/protocol"
)

const BootstrapSignatureHeader = "X-Scholaroscope-Bootstrap-Signature"

type Handler struct {
	repo              *Repo
	webhookSecret     string
	allowedSkew       time.Duration
}

func NewHandler(repo *Repo, webhookSecret string, allowedSkewSeconds string) *Handler {
	seconds, err := strconv.Atoi(allowedSkewSeconds)
	if err != nil || seconds <= 0 {
		seconds = 300
	}
	return &Handler{
		repo:          repo,
		webhookSecret: webhookSecret,
		allowedSkew:   time.Duration(seconds) * time.Second,
	}
}

func (h *Handler) HandleScholaroscopeEvent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_body"}})
		return
	}
	var hint struct {
		EventType             string `json:"event_type"`
		PluginInstallationRef string `json:"plugin_installation_ref"`
	}
	if err := json.Unmarshal(body, &hint); err != nil || hint.PluginInstallationRef == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_envelope"}})
		return
	}
	timestamp := r.Header.Get(protocol.TimestampHeader)
	if err := protocol.VerifyTimestamp(timestamp, time.Now().UTC(), h.allowedSkew); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "expired_timestamp"}})
		return
	}
	secret, exists, err := h.repo.InstallationSecret(r.Context(), hint.PluginInstallationRef)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]string{"code": "installation_lookup_failed"}})
		return
	}
	if exists {
		if err := protocol.VerifySignature(secret, timestamp, body, r.Header.Get(protocol.SignatureHeader)); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "invalid_signature"}})
			return
		}
	} else {
		if hint.EventType != "scholaroscope.timetable.workspace.bootstrap_requested.v1" || h.webhookSecret == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "unknown_installation"}})
			return
		}
		if err := protocol.VerifySignature(h.webhookSecret, timestamp, body, r.Header.Get(BootstrapSignatureHeader)); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "invalid_bootstrap_signature"}})
			return
		}
	}
	envelope, err := protocol.ParseEnvelope(body)
	if err != nil {
		status := http.StatusBadRequest
		code := "invalid_envelope"
		if errors.Is(err, protocol.ErrUnsupportedSchemaMajor) {
			status = http.StatusUnprocessableEntity
			code = "unsupported_schema_version"
		}
		writeJSON(w, status, map[string]any{"error": map[string]string{"code": code}})
		return
	}

	switch envelope.EventType {
	case "scholaroscope.timetable.workspace.bootstrap_requested.v1",
		"scholaroscope.timetable.workspace.academic_sync_requested.v1":
		h.handleBootstrap(w, r, envelope)
	case "scholaroscope.timetable.workspace.disabled.v1":
		if err := h.repo.DisableWorkspace(r.Context(), envelope.PluginInstallationRef); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]string{"code": "disable_failed"}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
	default:
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
	}
}

func (h *Handler) handleBootstrap(w http.ResponseWriter, r *http.Request, envelope *protocol.Envelope) {
	raw, err := json.Marshal(envelope.Payload)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_payload"}})
		return
	}
	var payload BootstrapPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_payload"}})
		return
	}
	result, err := h.repo.BootstrapWorkspace(r.Context(), payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]string{"code": "bootstrap_failed"}})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
