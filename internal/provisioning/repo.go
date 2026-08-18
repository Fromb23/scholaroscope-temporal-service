package provisioning

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

func (r *Repo) BootstrapWorkspace(ctx context.Context, payload BootstrapPayload) (*WorkspaceProvisioningResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("provisioning repo: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	workspaceID := uuid.New()
	err = tx.QueryRow(ctx, `
		INSERT INTO external_workspace (
			id, scholaroscope_workspace_ref, scholaroscope_organization_ref,
			display_name, timezone, status, provisioning_state, integration_health
		)
		VALUES ($1, $2, $3, $4, $5, 'ACTIVE', 'READY', 'HEALTHY')
		ON CONFLICT (scholaroscope_workspace_ref, scholaroscope_organization_ref)
		DO UPDATE SET
			display_name = EXCLUDED.display_name,
			timezone = EXCLUDED.timezone,
			status = 'ACTIVE',
			provisioning_state = 'READY',
			integration_health = 'HEALTHY',
			last_successful_sync_at = now(),
			last_error = NULL,
			updated_at = now()
		RETURNING id`,
		workspaceID,
		payload.ScholaroscopeWorkspaceRef,
		payload.ScholaroscopeOrganizationRef,
		payload.DisplayName,
		payload.Timezone,
	).Scan(&workspaceID)
	if err != nil {
		return nil, fmt.Errorf("provisioning repo: upsert workspace: %w", err)
	}

	installationID := uuid.New()
	err = tx.QueryRow(ctx, `
		INSERT INTO integration_installation (
			id, workspace_id, scholaroscope_installation_ref, signing_key_id,
			status, enabled_at
		)
		VALUES ($1, $2, $3, $4, 'ACTIVE', now())
		ON CONFLICT (scholaroscope_installation_ref)
		DO UPDATE SET
			workspace_id = EXCLUDED.workspace_id,
			signing_key_id = EXCLUDED.signing_key_id,
			status = 'ACTIVE',
			enabled_at = now(),
			disabled_at = NULL,
			revoked_at = NULL,
			updated_at = now()
		RETURNING id`,
		installationID,
		workspaceID,
		payload.PluginInstallationRef,
		"scholaroscope-managed",
	).Scan(&installationID)
	if err != nil {
		return nil, fmt.Errorf("provisioning repo: upsert installation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("provisioning repo: commit: %w", err)
	}
	return &WorkspaceProvisioningResult{
		WorkspaceID:    workspaceID,
		InstallationID: installationID,
		Status:         "ready",
	}, nil
}

func (r *Repo) DisableWorkspace(ctx context.Context, scholaroscopeInstallationRef string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE integration_installation
		SET status = 'DISABLED',
		    disabled_at = now(),
		    updated_at = now()
		WHERE scholaroscope_installation_ref = $1`,
		scholaroscopeInstallationRef,
	)
	if err != nil {
		return fmt.Errorf("provisioning repo: disable installation: %w", err)
	}
	return nil
}
