package conflict

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Log writes a scheduling conflict record.
// Always runs in its own transaction — never inside a failed scheduling tx.
func (r *Repo) Log(ctx context.Context, c *SchedulingConflict) error {
	query := `
		INSERT INTO scheduling_conflict (
			id, org_id, calendar_version_id, session_id,
			conflict_type, description, resolved
		) VALUES ($1, $2, $3, $4, $5, $6, FALSE)`

	_, err := r.pool.Exec(ctx, query,
		uuid.New(),
		c.OrgID,
		c.CalendarVersionID,
		c.SessionID,
		string(c.ConflictType),
		c.Description,
	)
	if err != nil {
		return fmt.Errorf("conflict repo: log: %w", err)
	}
	return nil
}

// ListUnresolved returns all unresolved conflicts for an org and calendar version.
func (r *Repo) ListUnresolved(ctx context.Context, orgID, calendarVersionID uuid.UUID) ([]SchedulingConflict, error) {
	query := `
		SELECT id, org_id, calendar_version_id, session_id,
		       conflict_type, description, resolved, detected_at, resolved_at
		FROM scheduling_conflict
		WHERE org_id = $1
		  AND calendar_version_id = $2
		  AND resolved = FALSE
		ORDER BY detected_at DESC`

	rows, err := r.pool.Query(ctx, query, orgID, calendarVersionID)
	if err != nil {
		return nil, fmt.Errorf("conflict repo: list unresolved: %w", err)
	}
	defer rows.Close()

	var conflicts []SchedulingConflict
	for rows.Next() {
		var c SchedulingConflict
		err := rows.Scan(
			&c.ID, &c.OrgID, &c.CalendarVersionID, &c.SessionID,
			&c.ConflictType, &c.Description, &c.Resolved,
			&c.DetectedAt, &c.ResolvedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("conflict repo: scan: %w", err)
		}
		conflicts = append(conflicts, c)
	}
	return conflicts, rows.Err()
}

// MarkResolved marks a conflict as resolved.
func (r *Repo) MarkResolved(ctx context.Context, conflictID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE scheduling_conflict
		 SET resolved = TRUE, resolved_at = NOW()
		 WHERE id = $1`,
		conflictID,
	)
	if err != nil {
		return fmt.Errorf("conflict repo: mark resolved: %w", err)
	}
	return nil
}

// SummaryByType returns unresolved conflict counts grouped by conflict_type for an org.
func (r *Repo) SummaryByType(ctx context.Context, orgID uuid.UUID) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT conflict_type, COUNT(*) AS total
		FROM scheduling_conflict
		WHERE org_id   = $1
		  AND resolved = FALSE
		GROUP BY conflict_type
		ORDER BY total DESC`,
		orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("conflict repo: summary by type: %w", err)
	}
	defer rows.Close()

	summary := make(map[string]int)
	for rows.Next() {
		var conflictType string
		var total int
		if err := rows.Scan(&conflictType, &total); err != nil {
			return nil, fmt.Errorf("conflict repo: scan summary: %w", err)
		}
		summary[conflictType] = total
	}
	return summary, rows.Err()
}
