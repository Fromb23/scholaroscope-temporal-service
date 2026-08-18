"use client";

import { useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet } from "../../lib/api";

export default function LearningTimetablePage() {
  const [calendarVersionId, setCalendarVersionId] = useState("");
  const [payload, setPayload] = useState<unknown>(null);
  const [error, setError] = useState<string | null>(null);

  return (
    <RequireSession>
      {(session) => (
        <>
          <h2>Learning timetable</h2>
          <div className="toolbar">
            <input
              value={calendarVersionId}
              onChange={(event) => setCalendarVersionId(event.target.value)}
              placeholder="Calendar version UUID"
            />
            <button
              type="button"
              onClick={() => {
                setError(null);
                apiGet(`/orgs/${session.workspace_uuid}/calendar/${calendarVersionId}/timetable`)
                  .then(setPayload)
                  .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
              }}
            >
              Load timetable
            </button>
          </div>
          {error ? <div className="error">{error}</div> : null}
          <pre className="card">{JSON.stringify(payload, null, 2)}</pre>
        </>
      )}
    </RequireSession>
  );
}
