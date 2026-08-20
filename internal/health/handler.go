package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"scholaroscope-temporal-service/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	pool *pgxpool.Pool
	cfg  *config.Config
}

func NewHandler(pool *pgxpool.Pool, cfg *config.Config) *Handler {
	return &Handler{pool: pool, cfg: cfg}
}

func (h *Handler) Live(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "live",
	})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	checks := map[string]string{}
	status := http.StatusOK
	if err := h.pool.Ping(ctx); err != nil {
		checks["postgres"] = "failed"
		status = http.StatusServiceUnavailable
	} else {
		checks["postgres"] = "ok"
	}
	for _, table := range []string{
		"external_workspace",
		"integration_installation",
		"portal_session",
		"timetable_version",
		"exam_timetable_version",
	} {
		if err := h.tableExists(ctx, table); err != nil {
			checks["table:"+table] = "missing"
			status = http.StatusServiceUnavailable
		} else {
			checks["table:"+table] = "ok"
		}
	}
	if h.cfg.ScholaroscopeWebhookSecret == "" {
		checks["integration_secret"] = "missing"
		status = http.StatusServiceUnavailable
	} else {
		checks["integration_secret"] = "configured"
	}

	writeJSON(w, status, map[string]any{
		"status": checksStatus(status),
		"checks": checks,
	})
}

func (h *Handler) tableExists(ctx context.Context, table string) error {
	var exists bool
	err := h.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name = $1
		 )`,
		table,
	).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return errMissingTable
	}
	return nil
}

func checksStatus(status int) string {
	if status == http.StatusOK {
		return "ready"
	}
	return "not_ready"
}

var errMissingTable = &missingTableError{}

type missingTableError struct{}

func (e *missingTableError) Error() string {
	return "missing table"
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
