package manifest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPluginManifestShape(t *testing.T) {
	handler := NewHandler(Config{
		PortalPublicURL:         "https://temporal.example.test",
		ScholaroscopeWebhookURL: "https://school.example.test/api/plugins/timetable/webhooks/",
	})
	req := httptest.NewRequest(http.MethodGet, "/plugin/manifest.json", nil)
	rec := httptest.NewRecorder()

	handler.PluginManifest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("manifest is not JSON: %v", err)
	}
	if body["plugin_key"] != "timetable" {
		t.Fatalf("plugin_key = %v", body["plugin_key"])
	}
	if body["name"] == "" || body["version"] == "" {
		t.Fatalf("name and version are required")
	}
	if _, ok := body["contributes"].(map[string]any); !ok {
		t.Fatalf("contributes must be an object")
	}
	requires, ok := body["requires"].([]any)
	if !ok || len(requires) != 1 || requires[0] != "notifications" {
		t.Fatalf("requires = %#v", body["requires"])
	}
	capabilities, ok := body["capabilities"].([]any)
	if !ok || len(capabilities) != 5 {
		t.Fatalf("capabilities = %#v", body["capabilities"])
	}
	if _, ok := body["config_schema"].(map[string]any); !ok {
		t.Fatalf("config_schema must be an object")
	}
	if _, ok := body["integration_protocol"].(map[string]any); !ok {
		t.Fatalf("integration_protocol must be an object")
	}
}
