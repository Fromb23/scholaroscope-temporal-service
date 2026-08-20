"use client";

import { useEffect, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, apiSend } from "../../lib/api";

export default function ConflictsPage() {
  const [calendarVersionId, setCalendarVersionId] = useState("");
  const [conflictId, setConflictId] = useState("");
  const [result, setResult] = useState<unknown>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    apiGet("/api/v1/conflicts")
      .then(setResult)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
  }, []);

  return (
    <RequireSession>
      {(session) => (
        <>
          <h2>Conflict inspection and resolution</h2>
          <p className="muted">Summary is loaded from workspace-implicit portal APIs. Legacy calendar-specific inspection remains protected below.</p>
          <div className="toolbar">
            <input value={calendarVersionId} onChange={(event) => setCalendarVersionId(event.target.value)} placeholder="Calendar version UUID" />
            <button
              type="button"
              onClick={() => {
                setError(null);
                apiGet(`/orgs/${session.workspace_uuid}/calendar/${calendarVersionId}/conflicts`)
                  .then(setResult)
                  .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
              }}
            >
              Load conflicts
            </button>
          </div>
          <div className="toolbar">
            <input value={conflictId} onChange={(event) => setConflictId(event.target.value)} placeholder="Conflict UUID" />
            <button
              type="button"
              onClick={() => {
                setError(null);
                apiSend(`/orgs/${session.workspace_uuid}/conflicts/${conflictId}/resolve`, "POST")
                  .then(setResult)
                  .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
              }}
            >
              Resolve selected conflict
            </button>
          </div>
          {error ? <div className="error">{error}</div> : null}
          <pre className="card">{JSON.stringify(result, null, 2)}</pre>
        </>
      )}
    </RequireSession>
  );
}
