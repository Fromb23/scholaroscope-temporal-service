"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { RequireSession } from "../../../components/RequireSession";
import { apiSend } from "../../../lib/api";

export default function NewTimetablePage() {
  const router = useRouter();
  const [name, setName] = useState("Learning timetable");
  const [effectiveStart, setEffectiveStart] = useState("");
  const [effectiveEnd, setEffectiveEnd] = useState("");
  const [error, setError] = useState<string | null>(null);

  return (
    <RequireSession>
      {() => (
        <section className="card">
          <h2>Create timetable</h2>
          <p className="muted">Creates a learning timetable and an editable draft version in this workspace.</p>
          <div className="toolbar">
            <input value={name} onChange={(event) => setName(event.target.value)} placeholder="Timetable name" />
            <input type="date" value={effectiveStart} onChange={(event) => setEffectiveStart(event.target.value)} />
            <input type="date" value={effectiveEnd} onChange={(event) => setEffectiveEnd(event.target.value)} />
            <button
              type="button"
              onClick={() => {
                setError(null);
                apiSend<{ version_uuid: string }>("/api/v1/timetables", "POST", {
                  name,
                  effective_start: effectiveStart,
                  effective_end: effectiveEnd,
                  scope_kind: "WORKSPACE",
                })
                  .then((created) => router.push(`/editor?version=${created.version_uuid}`))
                  .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
              }}
            >
              Create draft
            </button>
          </div>
          {error ? <div className="error">{error}</div> : null}
        </section>
      )}
    </RequireSession>
  );
}
