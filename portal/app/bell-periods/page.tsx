"use client";

import { useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiSend } from "../../lib/api";

const defaultCalendar = {
  learning_days: ["MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY"],
  day_start_time: "08:00",
  day_end_time: "10:20",
  slot_duration_minutes: 40,
  break_structure: [{ label: "Break", start_time: "09:20", end_time: "09:40" }],
};

export default function BellPeriodsPage() {
  const [body, setBody] = useState(JSON.stringify(defaultCalendar, null, 2));
  const [result, setResult] = useState<unknown>(null);
  const [error, setError] = useState<string | null>(null);

  return (
    <RequireSession>
      {(session) => (
        <>
          <h2>Bell-period configuration</h2>
          <p className="muted">Creates a real calendar version through the Go API.</p>
          <textarea rows={16} style={{ width: "100%" }} value={body} onChange={(event) => setBody(event.target.value)} />
          <div className="toolbar">
            <button
              type="button"
              onClick={() => {
                setError(null);
                apiSend(`/orgs/${session.workspace_uuid}/calendar`, "POST", JSON.parse(body))
                  .then(setResult)
                  .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
              }}
            >
              Save calendar draft
            </button>
          </div>
          {error ? <div className="error">{error}</div> : null}
          <pre className="card">{JSON.stringify(result, null, 2)}</pre>
        </>
      )}
    </RequireSession>
  );
}
