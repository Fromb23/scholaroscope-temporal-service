"use client";

import { useEffect, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet } from "../../lib/api";

export default function LearningTimetablePage() {
  const [payload, setPayload] = useState<unknown>(null);
  const [error, setError] = useState<string | null>(null);

  return (
    <RequireSession>
      {() => <LearningContent payload={payload} error={error} setPayload={setPayload} setError={setError} />}
    </RequireSession>
  );
}

function LearningContent({
  payload,
  error,
  setPayload,
  setError,
}: {
  payload: unknown;
  error: string | null;
  setPayload: (payload: unknown) => void;
  setError: (error: string | null) => void;
}) {
  useEffect(() => {
    setError(null);
    apiGet("/api/v1/timetables")
      .then(setPayload)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
  }, [setError, setPayload]);
  return (
    <>
      <h2>Learning timetable</h2>
      <p className="muted">Published and draft timetable records for this workspace session.</p>
      {error ? <div className="error">{error}</div> : null}
      <pre className="card">{JSON.stringify(payload, null, 2)}</pre>
    </>
  );
}
