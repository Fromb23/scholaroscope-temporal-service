"use client";

import { useEffect, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, type TeachingDemandResponse } from "../../lib/api";

export default function DemandPage() {
  const [payload, setPayload] = useState<TeachingDemandResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    apiGet<TeachingDemandResponse>("/api/v1/teaching-demands")
      .then(setPayload)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
  }, []);
  return (
    <RequireSession>
      {() => (
        <section className="card">
          <h2>Synchronized teaching demand</h2>
          <p className="muted">Demand comes from Scholaroscope teaching assignments synchronized during provisioning/reconciliation.</p>
          {error ? <div className="error">{error}</div> : null}
          {!payload?.demands?.length ? (
            <p>No synchronized teaching demand is available yet.</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Teacher</th>
                  <th>Class</th>
                  <th>Subject</th>
                  <th>Periods/cycle</th>
                  <th>Double lessons</th>
                </tr>
              </thead>
              <tbody>
                {payload.demands.map((demand) => (
                  <tr key={demand.teaching_assignment_uuid}>
                    <td>{demand.teacher_name}</td>
                    <td>{demand.cohort_name}</td>
                    <td>{demand.subject_name}</td>
                    <td>{demand.required_periods_per_cycle}</td>
                    <td>{demand.required_double_lessons}</td>
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
