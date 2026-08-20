"use client";

import { useEffect, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, type CalendarExceptionResponse } from "../../lib/api";

export default function ExceptionsPage() {
  const [data, setData] = useState<CalendarExceptionResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    apiGet<CalendarExceptionResponse>("/api/v1/exceptions")
      .then(setData)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
  }, []);

  return (
    <RequireSession>
      {() => (
        <section className="card">
          <h2>Scholaroscope calendar exceptions</h2>
          <p className="muted">These records are synchronized from Scholaroscope term calendars. Edit them in Scholaroscope, then replay synchronization.</p>
          {error ? <div className="error">{error}</div> : null}
          {!data?.exceptions.length ? (
            <p className="muted">No calendar exceptions are synchronized for the active calendar.</p>
          ) : (
            <table>
              <thead>
                <tr><th>Date</th><th>Title</th><th>Type</th><th>Affects learning</th><th>Source</th></tr>
              </thead>
              <tbody>
                {data.exceptions.map((item) => (
                  <tr key={item.exception_uuid}>
                    <td>{item.date}</td>
                    <td>{item.title}</td>
                    <td>{item.kind.replace(/_/g, " ")}</td>
                    <td>{item.blocks_learning ? "Blocks normal lessons" : "Informational"}</td>
                    <td>{item.source.replace(/_/g, " ")}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      )}
    </RequireSession>
  );
}
