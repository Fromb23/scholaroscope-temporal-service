"use client";
import { useEffect, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet } from "../../lib/api";
export default function DemandPage() {
  const [payload, setPayload] = useState<unknown>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    apiGet("/api/v1/teaching-demands")
      .then(setPayload)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
  }, []);
  return (
    <RequireSession>
      {() => (
        <section className="card">
          <h2>Timetable demand definition</h2>
          <p className="muted">Demand comes from synchronized Scholaroscope teaching assignments.</p>
          {error ? <div className="error">{error}</div> : null}
          <pre>{JSON.stringify(payload, null, 2)}</pre>
        </section>
      )}
    </RequireSession>
  );
}
