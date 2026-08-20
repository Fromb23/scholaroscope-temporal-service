"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, apiSend, type TimetableListResponse } from "../../lib/api";

export default function PublicationPage() {
  return (
    <Suspense fallback={<section className="card">Loading publication workflow…</section>}>
      <PublicationContent />
    </Suspense>
  );
}

function PublicationContent() {
  const search = useSearchParams();
  const [versions, setVersions] = useState<TimetableListResponse | null>(null);
  const [versionId, setVersionId] = useState(search.get("version") ?? "");
  const [reason, setReason] = useState("Published from timetable portal");
  const [result, setResult] = useState<unknown>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    apiGet<TimetableListResponse>("/api/v1/timetables")
      .then(setVersions)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
  }, []);

  return (
    <RequireSession>
      {() => (
        <section className="card">
          <h2>Publication preview and confirmation</h2>
          <p className="muted">Validation rebuilds hard conflicts. Publication writes an immutable published version and durable signed event.</p>
          <div className="toolbar">
            <select value={versionId} onChange={(event) => setVersionId(event.target.value)}>
              <option value="">Select version</option>
              {versions?.timetables.filter((item) => item.version_uuid).map((item) => (
                <option key={item.version_uuid ?? item.timetable_uuid} value={item.version_uuid ?? ""}>
                  {item.name} v{item.version_number} ({item.status})
                </option>
              ))}
            </select>
            <input value={reason} onChange={(event) => setReason(event.target.value)} placeholder="Publication reason" />
            <button
              type="button"
              disabled={!versionId}
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
              disabled={!versionId}
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
          {result ? <pre>{JSON.stringify(result, null, 2)}</pre> : null}
        </section>
      )}
    </RequireSession>
  );
}
