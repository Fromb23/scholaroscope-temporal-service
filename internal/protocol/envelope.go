package protocol

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	SignatureHeader          = "X-Scholaroscope-Signature"
	TimestampHeader          = "X-Scholaroscope-Timestamp"
	Algorithm                = "sha256"
	DefaultAllowedSkew       = 5 * time.Minute
	SupportedSchemaMajor int = 1
)

var (
	ErrInvalidSignature        = errors.New("invalid signature")
	ErrExpiredTimestamp        = errors.New("expired timestamp")
	ErrUnsupportedSchemaMajor  = errors.New("unsupported schema major version")
	ErrMissingAggregateVersion = errors.New("aggregate_version or sequence is required")
)

type Envelope struct {
	EventID               string         `json:"event_id"`
	EventType             string         `json:"event_type"`
	SchemaVersion         string         `json:"schema_version"`
	OccurredAt            string         `json:"occurred_at"`
	WorkspaceUUID         string         `json:"workspace_uuid"`
	PluginInstallationRef string         `json:"plugin_installation_ref"`
	AggregateType         string         `json:"aggregate_type"`
	AggregateUUID         string         `json:"aggregate_uuid"`
	AggregateVersion      *int64         `json:"aggregate_version,omitempty"`
	Sequence              *int64         `json:"sequence,omitempty"`
	CorrelationID         string         `json:"correlation_id"`
	IdempotencyKey        string         `json:"idempotency_key"`
	Payload               map[string]any `json:"payload"`
}

func (e Envelope) Validate() error {
	for label, value := range map[string]string{
		"event_id":       e.EventID,
		"workspace_uuid": e.WorkspaceUUID,
		"aggregate_uuid": e.AggregateUUID,
		"correlation_id": e.CorrelationID,
	} {
		if _, err := uuid.Parse(value); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
	}
	major, err := schemaMajor(e.SchemaVersion)
	if err != nil {
		return err
	}
	if major != SupportedSchemaMajor {
		return ErrUnsupportedSchemaMajor
	}
	if e.AggregateVersion == nil && e.Sequence == nil {
		return ErrMissingAggregateVersion
	}
	return nil
}

func ParseEnvelope(body []byte) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	if err := env.Validate(); err != nil {
		return nil, err
	}
	return &env, nil
}

func SignBody(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return Algorithm + "=" + hex.EncodeToString(mac.Sum(nil))
}

func VerifySignature(secret, timestamp string, body []byte, signature string) error {
	expected := SignBody(secret, timestamp, body)
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return ErrInvalidSignature
	}
	return nil
}

func VerifyTimestamp(timestamp string, now time.Time, allowedSkew time.Duration) error {
	observed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return err
	}
	delta := now.Sub(observed)
	if delta < 0 {
		delta = -delta
	}
	if delta > allowedSkew {
		return ErrExpiredTimestamp
	}
	return nil
}

func CanonicalJSON(value map[string]any) ([]byte, error) {
	var builder strings.Builder
	if err := writeCanonicalValue(&builder, value); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}

func schemaMajor(version string) (int, error) {
	majorText := strings.SplitN(version, ".", 2)[0]
	return strconv.Atoi(majorText)
}

func writeCanonicalValue(builder *strings.Builder, value any) error {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		builder.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				builder.WriteByte(',')
			}
			keyBytes, err := json.Marshal(key)
			if err != nil {
				return err
			}
			builder.Write(keyBytes)
			builder.WriteByte(':')
			if err := writeCanonicalValue(builder, typed[key]); err != nil {
				return err
			}
		}
		builder.WriteByte('}')
	case []any:
		builder.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				builder.WriteByte(',')
			}
			if err := writeCanonicalValue(builder, item); err != nil {
				return err
			}
		}
		builder.WriteByte(']')
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		builder.Write(encoded)
	}
	return nil
}
