"use client";
import { useEffect, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, type CalendarResponse, type TeachersResponse, type WorkspaceStatus } from "../../lib/api";

const dayNames = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

export default function AvailabilityPage() {
  const [teachers, setTeachers] = useState<TeachersResponse | null>(null);
  const [calendar, setCalendar] = useState<CalendarResponse | null>(null);
  const [workspace, setWorkspace] = useState<WorkspaceStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    Promise.all([
      apiGet<TeachersResponse>("/api/v1/teachers"),
      apiGet<CalendarResponse>("/api/v1/calendar"),
      apiGet<WorkspaceStatus>("/api/v1/workspace"),
    ])
      .then(([teacherPayload, calendarPayload, workspacePayload]) => {
        setTeachers(teacherPayload);
        setCalendar(calendarPayload);
        setWorkspace(workspacePayload);
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
  }, []);
  const teacherItems = teachers?.teachers ?? [];
  const slots = calendar?.slots ?? [];
  return (
    <RequireSession>
      {() => (
        <section className="card stack">
          <h2>Teacher availability</h2>
          <p className="muted">
            Only teachers with synchronized active cohort-subject assignments are shown.
          </p>
          {error ? <div className="error">{error}</div> : null}

          {!workspace?.current_academic_year ? (
            <div className="empty">No academic year has been synchronized yet.</div>
          ) : !workspace.schedulable_term ? (
            <div className="empty">No schedulable term is available for this academic year.</div>
          ) : teacherItems.length === 0 ? (
            <div className="empty">
              No valid teaching assignments are synchronized. Assign teachers to cohort subjects in Scholaroscope.
            </div>
          ) : slots.length === 0 ? (
            <div className="empty">No bell periods exist yet. Configure bell periods before setting availability.</div>
          ) : (
            <div className="stack">
              <div className="grid">
                {teacherItems.map((teacher) => (
                  <article className="card" key={teacher.actor_uuid}>
                    <h3>{teacher.display_name}</h3>
                    <p className="muted">{teacher.assignments.length} assignment{teacher.assignments.length === 1 ? "" : "s"}</p>
                    <ul>
                      {teacher.assignments.map((assignment) => (
                        <li key={assignment.cohort_subject_uuid}>
                          {assignment.cohort_name} · {assignment.subject_name}
                        </li>
                      ))}
                    </ul>
                  </article>
                ))}
              </div>

              <section>
                <h3>Bell-period availability grid</h3>
                <p className="muted">
                  Availability editing will use these committed bell periods. Existing API support currently exposes the grid for management review.
                </p>
                <div className="table-wrap">
                  <table>
                    <thead>
                      <tr>
                        <th>Day</th>
                        <th>Period</th>
                        <th>Time</th>
                        <th>Kind</th>
                      </tr>
                    </thead>
                    <tbody>
                      {slots.map((slot) => (
                        <tr key={slot.id}>
                          <td>{dayNames[slot.day_of_week] ?? `Day ${slot.day_of_week}`}</td>
                          <td>{slot.slot_index + 1}</td>
                          <td>{slot.start_time}–{slot.end_time}</td>
                          <td>{slot.slot_type}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </section>
            </div>
          )}
        </section>
      )}
    </RequireSession>
  );
}
