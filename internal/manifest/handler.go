package manifest

import (
	"encoding/json"
	"net/http"
)

type Config struct {
	PortalPublicURL         string
	ScholaroscopeWebhookURL string
}

type Handler struct {
	cfg Config
}

func NewHandler(cfg Config) *Handler {
	return &Handler{cfg: cfg}
}

func (h *Handler) PluginManifest(w http.ResponseWriter, r *http.Request) {
	manifest := map[string]any{
		"plugin_key":  "timetable",
		"name":        "External Timetable",
		"version":     "2.0.0",
		"description": "External timetable management integration for learning and examination scheduling.",
		"requires": []string{
			"notifications",
		},
		"capabilities": []string{
			"timetable.enabled",
			"timetable.learning",
			"timetable.examinations",
			"timetable.print",
			"timetable.lesson_automation",
		},
		"contributes": map[string]any{},
		"config_schema": map[string]any{
			"type": "object",
			"required": []string{
				"temporal_api_base_url",
				"temporal_launch_exchange_url",
				"temporal_webhook_url",
				"signing_key_id",
			},
			"properties": map[string]any{
				"temporal_api_base_url": map[string]any{
					"type":   "string",
					"format": "uri",
				},
				"temporal_launch_exchange_url": map[string]any{
					"type":   "string",
					"format": "uri",
				},
				"temporal_webhook_url": map[string]any{
					"type":   "string",
					"format": "uri",
				},
				"signing_key_id": map[string]any{
					"type":      "string",
					"minLength": 1,
				},
				"materialization_horizon_days": map[string]any{
					"type":    "integer",
					"minimum": 1,
					"maximum": 370,
					"default": 90,
				},
			},
			"additionalProperties": true,
		},
		"integration_protocol": map[string]any{
			"protocol":                "scholaroscope.external-plugin",
			"version":                 "1.0",
			"signature_algorithm":     "hmac-sha256",
			"timestamp_header":        "X-Scholaroscope-Timestamp",
			"signature_header":        "X-Scholaroscope-Signature",
			"signing_key_scope":       "installation",
			"replay_protection":       "idempotency_key",
			"supported_schema_major":  1,
			"supported_event_schemas": []string{"1.0"},
		},
		"service": map[string]any{
			"portal_public_url": h.cfg.PortalPublicURL,
			"endpoints": map[string]any{
				"launch_exchange": "/portal/launch/exchange",
				"inbound_events":  "/integration/scholaroscope/events",
				"manifest":        "/plugin/manifest.json",
			},
		},
	}
	if h.cfg.ScholaroscopeWebhookURL != "" {
		manifest["scholaroscope_callback"] = map[string]any{
			"publication_events_url": h.cfg.ScholaroscopeWebhookURL,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(manifest)
}
