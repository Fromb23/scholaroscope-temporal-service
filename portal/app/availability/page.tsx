"use client";
import { useEffect, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet } from "../../lib/api";
export default function AvailabilityPage() {
  const [teachers, setTeachers] = useState<unknown>(null);
  const [availability, setAvailability] = useState<unknown>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    Promise.all([
      apiGet("/api/v1/teachers"),
      apiGet("/api/v1/availability"),
    ])
      .then(([teacherPayload, availabilityPayload]) => {
        setTeachers(teacherPayload);
        setAvailability(availabilityPayload);
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
  }, []);
  return (
    <RequireSession>
      {() => (
        <section className="card">
          <h2>Teacher availability</h2>
          <p className="muted">Teacher identity and availability are scoped to the portal session workspace.</p>
          {error ? <div className="error">{error}</div> : null}
          <h3>Teachers</h3>
          <pre>{JSON.stringify(teachers, null, 2)}</pre>
          <h3>Availability</h3>
          <pre>{JSON.stringify(availability, null, 2)}</pre>
        </section>
      )}
    </RequireSession>
  );
}
