"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, apiSend, type TimetableListResponse } from "../../lib/api";

type PublicationResult = { status?: string; hard_conflicts?: number; soft_conflicts?: number; version_uuid?: string; changed_entries?: number };

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
  const [result, setResult] = useState<PublicationResult | null>(null);
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
                apiSend<PublicationResult>(`/api/v1/timetable-versions/${versionId}/validate`, "POST")
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
                apiSend<PublicationResult>(`/api/v1/timetable-versions/${versionId}/publish`, "POST", { reason })
                  .then(setResult)
                  .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
              }}
            >
              Publish
            </button>
          </div>
          {error ? <div className="error">{error}</div> : null}
          {result ? <section className="card"><h3>{(result.status ?? "Publication completed").replaceAll("_", " ")}</h3><p>Hard conflicts: {result.hard_conflicts ?? 0}</p><p>Soft conflicts: {result.soft_conflicts ?? 0}</p>{typeof result.changed_entries === "number" ? <p>Changed entries: {result.changed_entries}</p> : null}</section> : null}
        </section>
      )}
    </RequireSession>
  );
}
