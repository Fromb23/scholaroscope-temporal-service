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

func (r *Repo) ConsumeGrant(ctx context.Context, payload GrantPayload, payloadHash string) (*PortalSession, error) {
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
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM integration_installation
		WHERE workspace_id = $1
		  AND scholaroscope_installation_ref = $2
		  AND status = 'ACTIVE'`,
		workspaceID,
		payload.PluginInstallationRef,
	).Scan(&installationID)
	if err != nil {
		return nil, fmt.Errorf("launch repo: active installation: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO external_actor (
			id, workspace_id, scholaroscope_user_ref, display_name, actor_kind, status
		)
		VALUES ($1, $2, $3, $4, 'MANAGER', 'ACTIVE')
		ON CONFLICT (workspace_id, scholaroscope_user_ref)
		DO UPDATE SET status = 'ACTIVE', updated_at = now()`,
		actorID,
		workspaceID,
		payload.ActorUUID,
		payload.ActorUUID,
	)
	if err != nil {
		return nil, fmt.Errorf("launch repo: upsert actor: %w", err)
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
		expiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("launch repo: insert portal session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("launch repo: commit: %w", err)
	}
	return &PortalSession{
		ID:             sessionID,
		WorkspaceID:    workspaceID,
		InstallationID: installationID,
		ActorID:        actorID,
		ExpiresAt:      expiresAt,
	}, nil
}

func (r *Repo) GetSession(ctx context.Context, sessionID uuid.UUID) (*PortalSession, error) {
	var session PortalSession
	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_id, installation_id, actor_id, expires_at
		FROM portal_session
		WHERE id = $1
		  AND revoked_at IS NULL
		  AND expires_at > now()`,
		sessionID,
	).Scan(&session.ID, &session.WorkspaceID, &session.InstallationID, &session.ActorID, &session.ExpiresAt)
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
