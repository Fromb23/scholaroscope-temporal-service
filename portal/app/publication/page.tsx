"use client";
import { useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiSend } from "../../lib/api";
export default function PublicationPage() {
  const [versionId, setVersionId] = useState("");
  const [reason, setReason] = useState("");
  const [result, setResult] = useState<unknown>(null);
  const [error, setError] = useState<string | null>(null);
  return (
    <RequireSession>
      {() => (
        <section className="card">
          <h2>Publication preview and confirmation</h2>
          <p className="muted">Publication validates hard conflicts and writes a durable signed-delivery outbox event.</p>
          <div className="toolbar">
            <input value={versionId} onChange={(event) => setVersionId(event.target.value)} placeholder="Timetable version UUID" />
            <input value={reason} onChange={(event) => setReason(event.target.value)} placeholder="Publication reason" />
            <button
              type="button"
              onClick={() => {
                setError(null);
                apiSend(`/api/v1/timetable-versions/${versionId}/validate`, "POST")
                  .then(setResult)
                  .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
              }}
            >
              Validate
            </button>
            <button
              type="button"
              onClick={() => {
                setError(null);
                apiSend(`/api/v1/timetable-versions/${versionId}/publish`, "POST", { reason })
                  .then(setResult)
                  .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
              }}
            >
              Publish
            </button>
          </div>
          {error ? <div className="error">{error}</div> : null}
          <pre>{JSON.stringify(result, null, 2)}</pre>
        </section>
      )}
    </RequireSession>
  );
}
