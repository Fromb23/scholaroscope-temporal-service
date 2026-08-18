CREATE UNIQUE INDEX IF NOT EXISTS external_workspace_unique_scholaroscope_ref_idx
    ON external_workspace(scholaroscope_workspace_ref, scholaroscope_organization_ref);
