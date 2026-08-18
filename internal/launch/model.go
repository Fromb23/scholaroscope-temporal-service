package launch

import (
	"time"

	"github.com/google/uuid"
)

type GrantPayload struct {
	GrantID               string         `json:"grant_id"`
	Issuer                string         `json:"issuer"`
	Audience              string         `json:"audience"`
	ActorUUID             string         `json:"actor_uuid"`
	WorkspaceUUID         string         `json:"workspace_uuid"`
	PluginInstallationRef string         `json:"plugin_installation_ref"`
	Purpose               string         `json:"purpose"`
	PermissionSnapshot    map[string]any `json:"permission_snapshot"`
	IssuedAt              string         `json:"issued_at"`
	ExpiresAt             string         `json:"expires_at"`
	Nonce                 string         `json:"nonce"`
	CorrelationID         string         `json:"correlation_id"`
}

type PortalSession struct {
	ID             uuid.UUID
	WorkspaceID    uuid.UUID
	InstallationID uuid.UUID
	ActorID        uuid.UUID
	ExpiresAt      time.Time
}
