"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, type TimetableListResponse } from "../../lib/api";

export default function LearningTimetablePage() {
  const [payload, setPayload] = useState<TimetableListResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    apiGet<TimetableListResponse>("/api/v1/timetables")
      .then(setPayload)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
  }, []);

  return (
    <RequireSession>
      {() => (
        <>
          <div className="toolbar">
            <div>
              <h2>Learning timetable</h2>
              <p className="muted">Draft and published timetable versions for this workspace.</p>
            </div>
            <Link href="/timetables/new"><button type="button">Create timetable</button></Link>
          </div>
          {error ? <div className="error">{error}</div> : null}
          <section className="card">
            {!payload?.timetables?.length ? (
              <p>No timetables exist yet.</p>
            ) : (
              <table>
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Version</th>
                    <th>Status</th>
                    <th>Effective dates</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {payload.timetables.map((item) => (
                    <tr key={`${item.timetable_uuid}-${item.version_uuid ?? "none"}`}>
                      <td>{item.name}</td>
                      <td>{item.version_number ?? "No version"}</td>
                      <td>{item.status ?? "—"}</td>
                      <td>{item.effective_start} to {item.effective_end}</td>
                      <td>
                        {item.version_uuid ? (
                          <>
                            <Link href={`/editor?version=${item.version_uuid}`}>Edit</Link>{" "}
                            <Link href={`/publication?version=${item.version_uuid}`}>Publish</Link>
                          </>
                        ) : null}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>
        </>
      )}
    </RequireSession>
  );
}
