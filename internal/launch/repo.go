package launch

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

func (r *Repo) InstallationSecret(ctx context.Context, installationRef string) (string, error) {
	var secret string
	err := r.pool.QueryRow(ctx, `
		SELECT signing_secret
		FROM integration_installation
		WHERE scholaroscope_installation_ref = $1
		  AND status = 'ACTIVE'`,
		installationRef,
	).Scan(&secret)
	if err != nil {
		return "", fmt.Errorf("launch repo: installation secret: %w", err)
	}
	return secret, nil
}

func (r *Repo) ConsumeGrant(ctx context.Context, payload GrantPayload, payloadHash string, sessionExpiresAt time.Time) (*PortalSession, error) {
	workspaceID, err := uuid.Parse(payload.WorkspaceUUID)
	if err != nil {
		return nil, err
	}
	actorID, err := uuid.Parse(payload.ActorUUID)
	if err != nil {
		return nil, err
	}
	grantID, err := uuid.Parse(payload.GrantID)
	if err != nil {
		return nil, err
	}
	correlationID, err := uuid.Parse(payload.CorrelationID)
	if err != nil {
		return nil, err
	}
	issuedAt, err := time.Parse(time.RFC3339, payload.IssuedAt)
	if err != nil {
		return nil, err
	}
	expiresAt, err := time.Parse(time.RFC3339, payload.ExpiresAt)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("launch repo: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var installationID uuid.UUID
	var workspaceName string
	var workspaceTimezone string
	err = tx.QueryRow(ctx, `
		SELECT ii.id, ew.display_name, ew.timezone
		FROM integration_installation ii
		JOIN external_workspace ew ON ew.id = ii.workspace_id
		WHERE ii.workspace_id = $1
		  AND ii.scholaroscope_installation_ref = $2
		  AND ii.status = 'ACTIVE'
		  AND ew.status = 'ACTIVE'`,
		workspaceID,
		payload.PluginInstallationRef,
	).Scan(&installationID, &workspaceName, &workspaceTimezone)
	if err != nil {
		return nil, fmt.Errorf("launch repo: active installation: %w", err)
	}

	var actorDisplayName string
	var actorKind string
	err = tx.QueryRow(ctx, `
		SELECT display_name, actor_kind
		FROM external_actor
		WHERE id = $1
		  AND workspace_id = $2
		  AND status = 'ACTIVE'`,
		actorID,
		workspaceID,
	).Scan(&actorDisplayName, &actorKind)
	if err != nil {
		return nil, fmt.Errorf("launch repo: active synchronized actor: %w", err)
	}

	commandTag, err := tx.Exec(ctx, `
		INSERT INTO portal_launch_grant (
			id, workspace_id, installation_id, actor_id, purpose,
			permission_snapshot, nonce, correlation_id, issued_at, expires_at,
			consumed_at, payload_hash
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now(), $11)
		ON CONFLICT (installation_id, nonce) DO NOTHING`,
		grantID,
		workspaceID,
		installationID,
		actorID,
		payload.Purpose,
		payload.PermissionSnapshot,
		payload.Nonce,
		correlationID,
		issuedAt,
		expiresAt,
		payloadHash,
	)
	if err != nil {
		return nil, fmt.Errorf("launch repo: insert grant: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return nil, fmt.Errorf("launch repo: grant already consumed")
	}

	sessionID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO portal_session (
			id, workspace_id, installation_id, actor_id, launch_grant_id,
			permission_snapshot, purpose, issued_at, expires_at, last_seen_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), $8, now())`,
		sessionID,
		workspaceID,
		installationID,
		actorID,
		grantID,
		payload.PermissionSnapshot,
		payload.Purpose,
		sessionExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("launch repo: insert portal session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("launch repo: commit: %w", err)
	}
	return &PortalSession{
		ID:               sessionID,
		WorkspaceID:      workspaceID,
		WorkspaceName:    workspaceName,
		WorkspaceTimezone: workspaceTimezone,
		InstallationID:   installationID,
		ActorID:          actorID,
		ActorDisplayName: actorDisplayName,
		ActorKind:        actorKind,
		PermissionSnapshot: payload.PermissionSnapshot,
		ExpiresAt:        sessionExpiresAt,
	}, nil
}

func (r *Repo) GetSession(ctx context.Context, sessionID uuid.UUID) (*PortalSession, error) {
	var session PortalSession
	err := r.pool.QueryRow(ctx, `
		SELECT ps.id, ps.workspace_id, ew.display_name, ew.timezone,
		       ps.installation_id, ps.actor_id, ea.display_name, ea.actor_kind,
		       ps.permission_snapshot, ps.expires_at
		FROM portal_session ps
		JOIN integration_installation ii ON ii.id = ps.installation_id
		JOIN external_workspace ew ON ew.id = ps.workspace_id
		JOIN external_actor ea ON ea.id = ps.actor_id AND ea.workspace_id = ps.workspace_id
		WHERE ps.id = $1
		  AND ps.revoked_at IS NULL
		  AND ps.expires_at > now()
		  AND ii.status = 'ACTIVE'
		  AND ew.status = 'ACTIVE'
		  AND ea.status = 'ACTIVE'`,
		sessionID,
	).Scan(
		&session.ID,
		&session.WorkspaceID,
		&session.WorkspaceName,
		&session.WorkspaceTimezone,
		&session.InstallationID,
		&session.ActorID,
		&session.ActorDisplayName,
		&session.ActorKind,
		&session.PermissionSnapshot,
		&session.ExpiresAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("launch repo: get session: %w", err)
	}
	return &session, nil
}

func (r *Repo) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE portal_session
		SET revoked_at = now()
		WHERE id = $1`,
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("launch repo: revoke session: %w", err)
	}
	return nil
}
