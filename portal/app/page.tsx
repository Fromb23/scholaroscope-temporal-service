"use client";

import { useEffect, useState } from "react";
import { RequireSession } from "../components/RequireSession";
import { apiGet, type WorkspaceStatus } from "../lib/api";

export default function Home() {
  const [workspace, setWorkspace] = useState<WorkspaceStatus | null>(null);
  const [error, setError] = useState<string | null>(null);

  return (
    <RequireSession>
      {(session) => (
        <DashboardContent
          session={session}
          workspace={workspace}
          error={error}
          setWorkspace={setWorkspace}
          setError={setError}
        />
      )}
    </RequireSession>
  );
}

function DashboardContent({
  session,
  workspace,
  error,
  setWorkspace,
  setError,
}: {
  session: { workspace_uuid: string; actor_uuid: string; expires_at: string };
  workspace: WorkspaceStatus | null;
  error: string | null;
  setWorkspace: (workspace: WorkspaceStatus) => void;
  setError: (error: string | null) => void;
}) {
  useEffect(() => {
    setError(null);
    apiGet<WorkspaceStatus>("/api/v1/workspace")
      .then(setWorkspace)
      .catch(() => setError("Workspace status is unavailable."));
  }, [setError, setWorkspace]);
  return (
    <>
          <h2>Workspace identity and integration status</h2>
          {error ? <div className="error">{error}</div> : null}
          <div className="grid">
            <section className="card">
              <h3>Workspace</h3>
              <p>{workspace?.display_name ?? session.workspace_uuid}</p>
              <p className="muted">{workspace?.timezone ?? "Timezone unavailable"}</p>
            </section>
            <section className="card">
              <h3>Actor</h3>
              <p>{session.actor_uuid}</p>
            </section>
            <section className="card">
              <h3>Session expiry</h3>
              <p>{new Date(session.expires_at).toLocaleString()}</p>
            </section>
            <section className="card">
              <h3>Provisioning</h3>
              <p>{workspace?.provisioning_state ?? "Loading"}</p>
              <p className="muted">Health: {workspace?.integration_health ?? "UNKNOWN"}</p>
            </section>
          </div>
        </>
  );
}
