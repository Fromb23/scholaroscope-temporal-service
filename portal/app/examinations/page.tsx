"use client";
import { RequireSession } from "../../components/RequireSession";
export default function ExaminationsPage() {
  return (
    <RequireSession>
      {() => (
        <section className="card">
          <h2>Examination timetable unavailable</h2>
          <p>
            Examination scheduling is intentionally gated in this build. Learning timetables are operational; examination
            routes are hidden unless examination management permission is present and must not be treated as a ready workflow.
          </p>
        </section>
      )}
    </RequireSession>
  );
}
