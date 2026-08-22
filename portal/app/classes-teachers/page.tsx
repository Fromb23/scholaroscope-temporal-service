"use client";

import { useEffect, useMemo, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, apiSend, type AcademicContextResponse, type TeachersResponse, type TeachingDemandResponse, type WorkflowResponse } from "../../lib/api";

export default function ClassesTeachersPage() {
  const [term, setTerm] = useState("");
  const [teachers, setTeachers] = useState<TeachersResponse | null>(null);
  const [demands, setDemands] = useState<TeachingDemandResponse | null>(null);
  const [workflow, setWorkflow] = useState<WorkflowResponse | null>(null);
  const [drafts, setDrafts] = useState<Record<string, { periods: string; doubles: string }>>({});
  const [error, setError] = useState<string | null>(null);

  const load = (termUuid = term) => {
    const query = termUuid ? `?term_uuid=${encodeURIComponent(termUuid)}` : "";
    Promise.all([
      apiGet<TeachersResponse>(`/api/v1/teachers${query}`),
      apiGet<TeachingDemandResponse>(`/api/v1/teaching-demands${query}`),
      apiGet<WorkflowResponse>(`/api/v1/workflow${query}`),
    ])
      .then(([teacherData, demandData, workflowData]) => {
        setTeachers(teacherData);
        setDemands(demandData);
        setWorkflow(workflowData);
        setError(null);
      })
      .catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Classes and teachers could not be loaded."));
  };

  useEffect(() => {
    apiGet<AcademicContextResponse>("/api/v1/academic-context")
      .then((context) => {
        const selected = context.selected_term?.term_uuid ?? "";
        setTerm(selected);
        load(selected);
      })
      .catch(() => undefined);
    // Initial academic context chooses the server-derived active term.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const groupedDemands = useMemo(() => {
    const groups = new Map<string, NonNullable<TeachingDemandResponse["demands"]>>();
    for (const demand of demands?.demands ?? []) {
      const key = `${demand.cohort_name} (${demand.cohort_uuid})`;
      groups.set(key, [...(groups.get(key) ?? []), demand]);
    }
    return [...groups.entries()];
  }, [demands?.demands]);

  const sync = workflow?.synchronization.status;

  async function saveDemand(assignmentUuid: string) {
    const draft = drafts[assignmentUuid];
    if (!draft) return;
    setError(null);
    try {
      await apiSend(`/api/v1/teaching-demands/${assignmentUuid}?term_uuid=${encodeURIComponent(term)}`, "PATCH", {
        required_periods_per_cycle: Number(draft.periods),
        required_double_lessons: Number(draft.doubles || 0),
      });
      setDrafts((current) => {
        const next = { ...current };
        delete next[assignmentUuid];
        return next;
      });
      load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Demand could not be saved.");
    }
  }

  return <RequireSession>{() => <>
    <section className="card">
      <h2>Classes & teachers</h2>
      <p className="muted">Teaching assignments are owned in Scholaroscope and synchronized automatically. Timetable-only demand overrides are scoped to this academic term.</p>
      {error ? <div className="error">{error}</div> : null}
      <div className={`sync-state ${sync === "FAILED" ? "error" : ""}`}><strong>{sync === "PENDING" ? "Synchronization pending" : sync === "FAILED" ? "Synchronization failed" : sync === "NO_ASSIGNMENTS_IN_SCHOLAROSCOPE" ? "No teachers assigned in Scholaroscope" : sync === "SUCCEEDED_NO_ELIGIBLE_ASSIGNMENTS" ? "No eligible teaching assignments" : "Teaching assignments synchronized"}</strong><p>{sync === "NO_ASSIGNMENTS_IN_SCHOLAROSCOPE" ? "Assign teachers to class subjects in Scholaroscope, then refresh this workspace." : sync === "SUCCEEDED_NO_ELIGIBLE_ASSIGNMENTS" ? "Assignments exist, but their teachers, class subjects, or curriculum setup are not currently eligible for scheduling." : sync === "FAILED" ? "Return to Scholaroscope and request an academic data refresh, or contact support if it continues." : sync === "PENDING" ? "Academic data is still being synchronized. Refresh from Scholaroscope if this does not finish shortly." : `${workflow?.progress.assignments ?? 0} active assignments are ready for scheduling.`}</p></div>
    </section>

    <section className="card">
      <h3>Weekly teaching demand</h3>
      {!demands?.demands.length ? <div className="empty">No synchronized teaching demand is available yet.</div> : groupedDemands.map(([cohort, items]) => <div className="card" key={cohort}>
        <h4>{cohort.replace(/ \([^)]+\)$/, "")}</h4>
        <table>
          <thead><tr><th>Subject</th><th>Teacher</th><th>Required</th><th>Doubles</th><th>Scheduled</th><th>Status</th><th /></tr></thead>
          <tbody>{items.map((demand) => {
            const draft = drafts[demand.teaching_assignment_uuid];
            const source = demand.demand_source === "TIMETABLE_OVERRIDE" ? "timetable override" : demand.demand_source === "SCHOLAROSCOPE_SCHEME" ? "Scholaroscope scheme" : demand.demand_source === "MISSING" ? "not configured" : "Scholaroscope";
            return <tr key={demand.teaching_assignment_uuid}>
              <td>{demand.subject_name}</td>
              <td>{demand.teacher_name}</td>
              <td>{draft ? <input type="number" min={1} value={draft.periods} onChange={(event) => setDrafts((current) => ({ ...current, [demand.teaching_assignment_uuid]: { ...draft, periods: event.target.value } }))} /> : demand.required_periods_per_cycle ?? "Not configured"}<small>{source}</small></td>
              <td>{draft ? <input type="number" min={0} value={draft.doubles} onChange={(event) => setDrafts((current) => ({ ...current, [demand.teaching_assignment_uuid]: { ...draft, doubles: event.target.value } }))} /> : demand.required_double_lessons}</td>
              <td>{demand.scheduled_periods}</td>
              <td><span className={`status-pill ${demand.demand_status === "UNCONFIGURED" ? "warning" : ""}`}>{demand.demand_status.replaceAll("_", " ").toLowerCase()}</span></td>
              <td>{draft ? <button disabled={!Number(draft.periods) || Number(draft.doubles || 0) * 2 > Number(draft.periods)} onClick={() => saveDemand(demand.teaching_assignment_uuid)}>Save</button> : <button onClick={() => setDrafts((current) => ({ ...current, [demand.teaching_assignment_uuid]: { periods: String(demand.required_periods_per_cycle ?? ""), doubles: String(demand.required_double_lessons ?? 0) } }))}>Configure</button>}</td>
            </tr>;
          })}</tbody>
        </table>
      </div>)}
    </section>

    <section className="card">
      <h3>Teachers</h3>
      {!teachers?.teachers.length ? <div className="empty">No assigned teachers are available. Managers are not listed unless they also hold an active teaching assignment.</div> : <div className="teacher-list">{teachers.teachers.map((teacher) => <article key={teacher.actor_uuid}><strong>{teacher.display_name}</strong><ul>{teacher.assignments.map((assignment) => <li key={assignment.cohort_subject_uuid}>{assignment.cohort_name} · {assignment.subject_name}</li>)}</ul></article>)}</div>}
    </section>
  </>}</RequireSession>;
}
