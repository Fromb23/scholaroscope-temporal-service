package protocol

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testEnvelope(schemaVersion string) map[string]any {
	return map[string]any{
		"event_id":                uuid.NewString(),
		"event_type":              "temporal.timetable.learning.published.v1",
		"schema_version":          schemaVersion,
		"occurred_at":             "2026-08-18T10:00:00Z",
		"workspace_uuid":          uuid.NewString(),
		"plugin_installation_ref": uuid.NewString(),
		"aggregate_type":          "TIMETABLE_VERSION",
		"aggregate_uuid":          uuid.NewString(),
		"aggregate_version":       float64(1),
		"correlation_id":          uuid.NewString(),
		"idempotency_key":         "publication-1",
		"payload": map[string]any{
			"changes": []any{},
		},
	}
}

func TestSignedEnvelopeRoundTrip(t *testing.T) {
	body, err := CanonicalJSON(testEnvelope("1.0"))
	if err != nil {
		t.Fatal(err)
	}
	timestamp := "2026-08-18T10:00:00Z"
	signature := SignBody("secret", timestamp, body)

	if err := VerifySignature("secret", timestamp, body, signature); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseEnvelope(body)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.EventType != "temporal.timetable.learning.published.v1" {
		t.Fatalf("unexpected event type: %s", parsed.EventType)
	}
}

func TestInvalidSignatureIsRejected(t *testing.T) {
	body, err := CanonicalJSON(testEnvelope("1.0"))
	if err != nil {
		t.Fatal(err)
	}
	err = VerifySignature("secret", "2026-08-18T10:00:00Z", body, "sha256=bad")
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected invalid signature, got %v", err)
	}
}

func TestExpiredTimestampIsRejected(t *testing.T) {
	err := VerifyTimestamp(
		"2026-08-18T10:00:00Z",
		time.Date(2026, 8, 18, 10, 10, 1, 0, time.UTC),
		5*time.Minute,
	)
	if !errors.Is(err, ErrExpiredTimestamp) {
		t.Fatalf("expected expired timestamp, got %v", err)
	}
}

func TestCurrentTimestampIsAccepted(t *testing.T) {
	now := time.Now().UTC()
	if err := VerifyTimestamp(now.Add(-30*time.Second).Format(time.RFC3339), now, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestUnsupportedMajorVersionIsRejected(t *testing.T) {
	body, err := CanonicalJSON(testEnvelope("2.0"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParseEnvelope(body)
	if !errors.Is(err, ErrUnsupportedSchemaMajor) {
		t.Fatalf("expected unsupported schema major, got %v", err)
	}
}
