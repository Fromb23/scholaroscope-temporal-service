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
  session: { workspace_uuid: string; workspace_name?: string; actor_uuid: string; actor_display_name?: string; expires_at: string };
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
          <h2>{workspace?.display_name ?? session.workspace_name ?? "Timetable management"}</h2>
          <p className="muted">
            Signed in as {workspace?.actor?.display_name ?? session.actor_display_name ?? "Timetable manager"}.
            {workspace?.timezone ? ` Workspace timezone: ${workspace.timezone}.` : ""}
          </p>
          {error ? <div className="error">{error}</div> : null}
          <div className="grid">
            <section className="card">
              <h3>Academic context</h3>
              <p>{workspace?.current_academic_year?.name ?? "No synchronized academic year"}</p>
              <p className="muted">
                {workspace?.current_term
                  ? `Current term: ${workspace.current_term.name} (${workspace.current_term.start_date} to ${workspace.current_term.end_date})`
                  : workspace?.schedulable_term
                    ? `Upcoming schedulable term: ${workspace.schedulable_term.name} (${workspace.schedulable_term.start_date} to ${workspace.schedulable_term.end_date})`
                    : "No current or schedulable term is synchronized."}
              </p>
            </section>
            <section className="card">
              <h3>Teaching demand</h3>
              <p>{workspace?.counts?.teaching_assignment_count ?? 0} assignments</p>
              <p className="muted">
                {(workspace?.counts?.class_count ?? 0)} classes · {(workspace?.counts?.teacher_count ?? 0)} teachers · {(workspace?.counts?.subject_count ?? 0)} subjects
              </p>
            </section>
            <section className="card">
              <h3>Timetables</h3>
              <p>{workspace?.counts?.published_timetable_count ?? 0} published</p>
              <p className="muted">{workspace?.counts?.timetable_count ?? 0} total drafts/published records</p>
            </section>
            <section className="card">
              <h3>Integration</h3>
              <p>{workspace?.readiness?.status ?? workspace?.provisioning_state ?? "Loading"}</p>
              <p className="muted">
                Health: {workspace?.integration_health ?? "UNKNOWN"}
                {workspace?.reconciliation_required ? " · reconciliation required" : ""}
              </p>
            </section>
            <section className="card">
              <h3>Session</h3>
              <p>Portal session active</p>
              <p className="muted">Expires {new Date(session.expires_at).toLocaleString()}</p>
            </section>
          </div>
        </>
  );
}
