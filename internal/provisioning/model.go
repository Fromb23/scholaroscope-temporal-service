package provisioning

import "github.com/google/uuid"

type BootstrapPayload struct {
	ScholaroscopeWorkspaceRef    string `json:"scholaroscope_workspace_ref"`
	ScholaroscopeOrganizationRef string `json:"scholaroscope_organization_ref"`
	DisplayName                  string `json:"display_name"`
	Timezone                     string `json:"timezone"`
	PluginInstallationRef        string `json:"plugin_installation_ref"`
}

type WorkspaceProvisioningResult struct {
	WorkspaceID    uuid.UUID `json:"workspace_uuid"`
	InstallationID uuid.UUID `json:"installation_uuid"`
	Status         string    `json:"status"`
}
