package outbox

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	StatusPending    = "PENDING"
	StatusDelivering = "DELIVERING"
	StatusDelivered  = "DELIVERED"
	StatusDeadLetter = "DEAD_LETTER"
)

type Worker struct {
	pool          *pgxpool.Pool
	fallbackURL   string
	httpClient    *http.Client
	maxAttempts   int
	batchSize     int
	pollInterval  time.Duration
}

type Config struct {
	FallbackCallbackURL string
	RequestTimeout      time.Duration
	MaxAttempts         int
	BatchSize           int
	PollInterval        time.Duration
}

type Event struct {
	ID                    uuid.UUID
	WorkspaceID           uuid.UUID
	InstallationID        uuid.UUID
	EventType             string
	SchemaVersion         string
	AggregateType         string
	AggregateUUID         uuid.UUID
	AggregateVersion      *int64
	CorrelationID         uuid.UUID
	IdempotencyKey        string
	Payload               map[string]any
	Attempts              int
	ScholaroscopeRef      string
	SigningSecret         string
	SigningKeyID          string
	CallbackURL           *string
}

func NewWorker(pool *pgxpool.Pool, cfg Config) *Worker {
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 10 * time.Second
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 8
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 25
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	return &Worker{
		pool: pool,
		fallbackURL: cfg.FallbackCallbackURL,
		httpClient: &http.Client{Timeout: cfg.RequestTimeout},
		maxAttempts: cfg.MaxAttempts,
		batchSize: cfg.BatchSize,
		pollInterval: cfg.PollInterval,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		if _, err := w.DispatchOnce(ctx); err != nil {
			log.Printf("temporal_outbox_dispatch_error err=%v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *Worker) DispatchOnce(ctx context.Context) (int, error) {
	events, err := w.claim(ctx)
	if err != nil {
		return 0, err
	}
	for _, event := range events {
		if err := w.deliver(ctx, event); err != nil {
			log.Printf("temporal_outbox_event_failed event_id=%s correlation_id=%s err=%v", event.ID, event.CorrelationID, err)
		}
	}
	return len(events), nil
}

func (w *Worker) claim(ctx context.Context) ([]Event, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		SELECT oe.id, oe.workspace_id, oe.installation_id, oe.event_type, oe.schema_version,
		       oe.aggregate_type, oe.aggregate_uuid, oe.aggregate_version, oe.correlation_id,
		       oe.idempotency_key, oe.payload, oe.attempts,
		       ii.scholaroscope_installation_ref, ii.signing_secret, ii.signing_key_id, ii.callback_url
		FROM outbox_event oe
		JOIN integration_installation ii ON ii.id = oe.installation_id
		WHERE oe.status = 'PENDING'
		  AND (oe.next_retry_at IS NULL OR oe.next_retry_at <= now())
		  AND ii.status = 'ACTIVE'
		ORDER BY oe.workspace_id, oe.created_at
		FOR UPDATE SKIP LOCKED
		LIMIT $1`,
		w.batchSize,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var event Event
		var payloadBytes []byte
		if err := rows.Scan(
			&event.ID, &event.WorkspaceID, &event.InstallationID, &event.EventType, &event.SchemaVersion,
			&event.AggregateType, &event.AggregateUUID, &event.AggregateVersion, &event.CorrelationID,
			&event.IdempotencyKey, &payloadBytes, &event.Attempts,
			&event.ScholaroscopeRef, &event.SigningSecret, &event.SigningKeyID, &event.CallbackURL,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payloadBytes, &event.Payload); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	for _, event := range events {
		if _, err := tx.Exec(ctx, `
			UPDATE outbox_event
			SET status = 'DELIVERING', attempts = attempts + 1
			WHERE id = $1`,
			event.ID,
		); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return events, nil
}

func (w *Worker) deliver(ctx context.Context, event Event) error {
	callbackURL := w.fallbackURL
	if event.CallbackURL != nil && *event.CallbackURL != "" {
		callbackURL = *event.CallbackURL
	}
	if callbackURL == "" {
		w.deadLetter(ctx, event, "callback_url_not_configured")
		return fmt.Errorf("callback URL is not configured")
	}
	body, err := json.Marshal(map[string]any{
		"event_id": event.ID.String(),
		"event_type": event.EventType,
		"schema_version": event.SchemaVersion,
		"occurred_at": time.Now().UTC().Format(time.RFC3339Nano),
		"workspace_uuid": event.WorkspaceID.String(),
		"plugin_installation_ref": event.ScholaroscopeRef,
		"aggregate_type": event.AggregateType,
		"aggregate_uuid": event.AggregateUUID.String(),
		"aggregate_version": event.AggregateVersion,
		"correlation_id": event.CorrelationID.String(),
		"idempotency_key": event.IdempotencyKey,
		"payload": event.Payload,
	})
	if err != nil {
		w.retry(ctx, event, err.Error())
		return err
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURL, bytes.NewReader(body))
	if err != nil {
		w.retry(ctx, event, err.Error())
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Scholaroscope-Timestamp", timestamp)
	req.Header.Set("X-Scholaroscope-Signature", sign(event.SigningSecret, timestamp, body))
	req.Header.Set("X-Scholaroscope-Key-Id", event.SigningKeyID)
	req.Header.Set("X-Scholaroscope-Correlation-Id", event.CorrelationID.String())
	resp, err := w.httpClient.Do(req)
	if err != nil {
		w.retry(ctx, event, err.Error())
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, err = w.pool.Exec(ctx, `
			UPDATE outbox_event
			SET status = 'DELIVERED', delivered_at = now(), last_error = NULL, next_retry_at = NULL
			WHERE id = $1`,
			event.ID,
		)
		return err
	}
	message := fmt.Sprintf("http_%d", resp.StatusCode)
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized ||
		resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound ||
		resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusUnprocessableEntity {
		w.deadLetter(ctx, event, message)
		return fmt.Errorf(message)
	}
	w.retry(ctx, event, message)
	return fmt.Errorf(message)
}

func (w *Worker) retry(ctx context.Context, event Event, message string) {
	event.Attempts++
	if event.Attempts >= w.maxAttempts {
		w.deadLetter(ctx, event, message)
		return
	}
	delay := time.Duration(30*(1<<max(0, event.Attempts-1))) * time.Second
	if delay > time.Hour {
		delay = time.Hour
	}
	_, _ = w.pool.Exec(ctx, `
		UPDATE outbox_event
		SET status = 'PENDING', last_error = $2, next_retry_at = now() + $3::interval
		WHERE id = $1`,
		event.ID,
		safe(message),
		fmt.Sprintf("%d seconds", int(delay.Seconds())),
	)
}

func (w *Worker) deadLetter(ctx context.Context, event Event, message string) {
	_, _ = w.pool.Exec(ctx, `
		UPDATE outbox_event
		SET status = 'DEAD_LETTER', last_error = $2, dead_lettered_at = now(), next_retry_at = NULL
		WHERE id = $1`,
		event.ID,
		safe(message),
	)
}

func sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func safe(message string) string {
	if len(message) > 400 {
		return message[:397] + "..."
	}
	return message
}

func Replay(ctx context.Context, pool *pgxpool.Pool, ids []uuid.UUID) (int64, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE outbox_event
		SET status = 'PENDING', next_retry_at = now(), dead_lettered_at = NULL, last_error = 'replay_requested'
		WHERE id = ANY($1)
		  AND status = 'DEAD_LETTER'`,
		ids,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func PendingSummary(ctx context.Context, pool *pgxpool.Pool) (map[string]int, error) {
	rows, err := pool.Query(ctx, `
		SELECT status, COUNT(*)
		FROM outbox_event
		GROUP BY status`)
	if err != nil {
		if err == pgx.ErrNoRows {
			return map[string]int{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	result := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		result[status] = count
	}
	return result, rows.Err()
}
