"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { apiGet, apiSend, PortalApiError, type AcademicContextResponse, type CalendarResponse, type ClassesSpacesResponse, type TeachersResponse, type TeachingDemandResponse, type TimetableListResponse, type VersionDetail, type WorkflowResponse } from "../lib/api";
import { configuredDays, dayLabels, lessonCardClass, periodRows, unconfiguredDemandCount, unscheduledPeriods, visibleEntries, type GridView } from "../lib/timetable-grid";

type WorkspaceData = { workflow: WorkflowResponse; calendar: CalendarResponse; classesSpaces: ClassesSpacesResponse; teachers: TeachersResponse; demands: TeachingDemandResponse; timetables: TimetableListResponse; version: VersionDetail | null };

function clock(value: string) { return value.match(/T?(\d{2}:\d{2})/)?.[1] ?? value; }
function errorPresentation(reason: unknown) {
  if (reason instanceof PortalApiError) return reason.contract;
  return { type: "internal", code: "timetable_update_failed", message: "Something went wrong while updating the timetable. Please try again.", details: {}, action: { label: "Try again", target: "/timetable" } };
}

export function TimetableWorkspace() {
  const [academic, setAcademic] = useState<AcademicContextResponse | null>(null);
  const [selectedTerm, setSelectedTerm] = useState("");
  const [data, setData] = useState<WorkspaceData | null>(null);
  const [view, setView] = useState<GridView>({ kind: "CLASS" });
  const [busy, setBusy] = useState("");
  const [error, setError] = useState<ReturnType<typeof errorPresentation> | null>(null);

  const loadAcademic = useCallback(async () => {
    const response = await apiGet<AcademicContextResponse>("/api/v1/academic-context");
    setAcademic(response);
    const terms = response.academic_years.flatMap((year) => year.terms);
    const requested = new URLSearchParams(window.location.search).get("term_uuid");
    const active = terms.find((term) => term.lifecycle === "ACTIVE");
    const initial = terms.some((term) => term.term_uuid === requested && term.scheduling_permitted) ? requested : active?.term_uuid;
    setSelectedTerm((current) => current || initial || "");
  }, []);

  const loadWorkspace = useCallback(async (termUuid: string) => {
    if (!termUuid) return;
    setError(null);
    const query = `?term_uuid=${encodeURIComponent(termUuid)}`;
    try {
      const [workflow, calendar, classesSpaces, teachers, demands, timetables] = await Promise.all([
        apiGet<WorkflowResponse>(`/api/v1/workflow${query}`), apiGet<CalendarResponse>("/api/v1/calendar"),
        apiGet<ClassesSpacesResponse>(`/api/v1/classes-spaces${query}`), apiGet<TeachersResponse>(`/api/v1/teachers${query}`),
        apiGet<TeachingDemandResponse>(`/api/v1/teaching-demands${query}`), apiGet<TimetableListResponse>("/api/v1/timetables"),
      ]);
      const version = workflow.relevant_timetable.version_uuid ? await apiGet<VersionDetail>(`/api/v1/timetable-versions/${workflow.relevant_timetable.version_uuid}`) : null;
      setData({ workflow, calendar, classesSpaces, teachers, demands, timetables, version });
      setView((current) => current.entityUuid ? current : classesSpaces.classes[0] ? { kind: "CLASS", entityUuid: classesSpaces.classes[0].cohort_uuid } : { kind: "WORKSPACE" });
    } catch (reason) { setError(errorPresentation(reason)); }
  }, []);

  useEffect(() => {
    // Network completion, rather than the effect body, owns these state updates.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadAcademic().catch((reason) => setError(errorPresentation(reason)));
  }, [loadAcademic]);
  useEffect(() => {
    if (selectedTerm) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      loadWorkspace(selectedTerm);
    }
  }, [loadWorkspace, selectedTerm]);

  const entries = useMemo(() => visibleEntries(data?.version?.entries ?? [], view), [data?.version?.entries, view]);
  const demands = useMemo(() => {
    const all = data?.demands.demands ?? [];
    if (view.kind === "CLASS") return all.filter((item) => item.cohort_uuid === view.entityUuid);
    if (view.kind === "TEACHER") return all.filter((item) => item.teacher_uuid === view.entityUuid);
    return all;
  }, [data?.demands.demands, view]);
  const remaining = unscheduledPeriods(demands, entries);
  const unconfigured = unconfiguredDemandCount(demands);
  const terms = academic?.academic_years.flatMap((year) => year.terms) ?? [];

  async function runAction(action: "create" | "generate" | "validate" | "publish" | "revision") {
    if (!data || !selectedTerm || busy) return;
    setBusy(action); setError(null);
    try {
      let versionId = data.workflow.relevant_timetable.version_uuid;
      if (action === "create") {
        const term = terms.find((item) => item.term_uuid === selectedTerm);
        if (!term) throw new Error("term unavailable");
        const created = await apiSend<{ version_uuid: string }>("/api/v1/timetables", "POST", { timetable_type: "LEARNING", academic_term_uuid: selectedTerm, effective_start: term.start_date, effective_end: term.end_date });
        versionId = created.version_uuid;
      } else if (action === "revision") {
        const timetableId = data.workflow.relevant_timetable.timetable_uuid;
        if (!timetableId) throw new Error("timetable unavailable");
        const revision = await apiSend<{ version_uuid: string }>(`/api/v1/timetables/${timetableId}/versions`, "POST");
        versionId = revision.version_uuid;
      } else if (action === "generate" && versionId) await apiSend(`/api/v1/versions/${versionId}/generate`, "POST", { time_budget_ms: 30000, iteration_budget: 5000000, restarts: 5 });
      else if (action === "validate" && versionId) await apiSend(`/api/v1/timetable-versions/${versionId}/validate`, "POST");
      else if (action === "publish" && versionId) await apiSend(`/api/v1/timetable-versions/${versionId}/publish`, "POST", { reason: "Published after timetable validation" });
      await loadWorkspace(selectedTerm);
    } catch (reason) { setError(errorPresentation(reason)); } finally { setBusy(""); }
  }

  const workflow = data?.workflow;
  const currentTerm = terms.find((term) => term.term_uuid === selectedTerm);
  return <div className="timetable-workspace">
    <header className="workspace-header"><div><p className="eyebrow">Learning timetable</p><h2>{currentTerm ? `${currentTerm.academic_year_label} · ${currentTerm.name}` : "Choose an academic term"}</h2><p className="muted">{workflow?.explanation ?? (academic && !academic.academic_years.length ? "This workspace does not have an active academic year. Set one up in Scholaroscope to continue." : academic && !academic.has_active_term ? "No active term is available. Create or activate a term in Scholaroscope to continue." : "Loading your timetable workspace…")}</p></div>
      <label className="context-select">Academic term<select value={selectedTerm} onChange={(event) => { const termUuid = event.target.value; setSelectedTerm(termUuid); setView({ kind: "CLASS" }); const url = new URL(window.location.href); if (termUuid) url.searchParams.set("term_uuid", termUuid); else url.searchParams.delete("term_uuid"); window.history.replaceState({}, "", url); }}><option value="">Choose a term</option>{academic?.academic_years.map((year) => <optgroup key={year.academic_year_uuid} label={year.name}>{year.terms.map((term) => <option key={term.term_uuid} value={term.term_uuid} disabled={!term.scheduling_permitted}>{term.name} · {term.lifecycle.toLowerCase()}</option>)}</optgroup>)}</select></label>
    </header>
    {error ? <div className="error" role="alert"><strong>{error.message}</strong>{error.action ? <a href={error.action.target}>{error.action.label}</a> : null}</div> : null}
    {selectedTerm ? <section className="workflow-strip" aria-label="Timetable progress"><div><span className="status-pill">{workflow?.state.replaceAll("_", " ").toLowerCase() ?? "loading"}</span><span>{workflow?.progress.completed ?? 0} of {workflow?.progress.total ?? 5} setup areas ready</span></div><div className="workflow-actions">
      {workflow?.state === "READY_TO_GENERATE" ? <button onClick={() => runAction("create")} disabled={!!busy}>{busy === "create" ? "Creating…" : "Create term timetable"}</button> : null}
      {["DRAFT_IN_PROGRESS", "DRAFT_HAS_CONFLICTS"].includes(workflow?.state ?? "") ? <button onClick={() => runAction("generate")} disabled={!!busy}>{busy === "generate" ? "Generating…" : "Generate timetable"}</button> : null}
      {workflow?.state === "DRAFT_READY_FOR_VALIDATION" ? <button onClick={() => runAction("validate")} disabled={!!busy}>{busy === "validate" ? "Validating…" : "Validate timetable"}</button> : null}
      {workflow?.state === "READY_TO_PUBLISH" ? <button onClick={() => runAction("publish")} disabled={!!busy}>{busy === "publish" ? "Publishing…" : "Publish timetable"}</button> : null}
      {workflow?.state === "PUBLISHED" ? <button onClick={() => runAction("revision")} disabled={!!busy}>{busy === "revision" ? "Creating…" : "Create revision"}</button> : null}
    </div></section> : null}
    {selectedTerm ? <section className="grid-toolbar" aria-label="Timetable view controls"><div className="segmented"><button className={view.kind === "CLASS" ? "active" : ""} onClick={() => setView({ kind: "CLASS", entityUuid: data?.classesSpaces.classes[0]?.cohort_uuid })}>Class</button><button className={view.kind === "TEACHER" ? "active" : ""} onClick={() => setView({ kind: "TEACHER", entityUuid: data?.teachers.teachers[0]?.actor_uuid })}>Teacher</button><button className={view.kind === "WORKSPACE" ? "active" : ""} onClick={() => setView({ kind: "WORKSPACE" })}>Whole school</button></div>
      {view.kind === "CLASS" ? <select aria-label="Select class" value={view.entityUuid ?? ""} onChange={(event) => setView({ kind: "CLASS", entityUuid: event.target.value })}>{data?.classesSpaces.classes.map((item) => <option key={item.cohort_uuid} value={item.cohort_uuid}>{item.name} · {item.enrollment_count} learners</option>)}</select> : null}
      {view.kind === "TEACHER" ? <select aria-label="Select teacher" value={view.entityUuid ?? ""} onChange={(event) => setView({ kind: "TEACHER", entityUuid: event.target.value })}>{data?.teachers.teachers.map((item) => <option key={item.actor_uuid} value={item.actor_uuid}>{item.display_name}</option>)}</select> : null}
      <div className={remaining > 0 || unconfigured > 0 ? "demand-badge warning" : "demand-badge"}>{unconfigured > 0 ? `${unconfigured} teaching demands not configured` : remaining > 0 ? `${remaining} lesson periods unscheduled` : "All configured required lesson periods scheduled"}</div>
    </section> : null}
    {selectedTerm ? <WeeklyGrid calendar={data?.calendar ?? null} entries={entries} /> : <section className="empty"><h3>{academic && !academic.academic_years.length ? "No active academic year" : "No active term available"}</h3><p>{academic && !academic.academic_years.length ? "Set up an academic year in Scholaroscope, then refresh this workspace." : "Create or activate an academic term in Scholaroscope, then refresh this workspace."}</p></section>}
  </div>;
}

function WeeklyGrid({ calendar, entries }: { calendar: CalendarResponse | null; entries: VersionDetail["entries"] }) {
  const days = configuredDays(calendar); const rows = periodRows(calendar);
  if (!calendar?.calendar_version) return <section className="empty"><h3>Set up your school day</h3><p>Add teaching periods, breaks, lunch, and assembly before generating a timetable.</p><a href="/school-day">Set up school day</a></section>;
  if (!rows.length || !days.length) return <section className="empty">No configured timetable periods are available.</section>;
  return <div className="weekly-grid-wrap" tabIndex={0} aria-label="Weekly timetable grid"><div className="weekly-grid" role="grid" style={{ gridTemplateColumns: `150px repeat(${days.length}, minmax(180px, 1fr))`, gridTemplateRows: `52px repeat(${rows.length}, 92px)` }}>
    <div className="grid-corner" role="columnheader">Period</div>{days.map((day, column) => <div className="day-header" role="columnheader" key={day} style={{ gridColumn: column + 2, gridRow: 1 }}>{dayLabels[day]}</div>)}
    {rows.map((row, rowIndex) => <div className="period-label" role="rowheader" key={`period-${row.slot_index}`} style={{ gridColumn: 1, gridRow: rowIndex + 2 }}><strong>{row.slot_type === "LESSON" ? `Period ${row.slot_index + 1}` : row.slot_type.replaceAll("_", " ")}</strong><span>{clock(row.start_time)}–{clock(row.end_time)}</span></div>)}
    {rows.flatMap((row, rowIndex) => days.map((day, column) => { const slot = calendar.slots.find((item) => item.day_of_week === day && item.slot_index === row.slot_index); const nonTeaching = slot?.slot_type !== "LESSON"; return <div role="gridcell" tabIndex={0} aria-label={`${dayLabels[day]} ${clock(row.start_time)} ${slot?.slot_type ?? "unavailable"}`} className={`grid-cell ${nonTeaching ? "non-teaching" : ""}`} key={`${day}-${row.slot_index}`} style={{ gridColumn: column + 2, gridRow: rowIndex + 2 }}>{nonTeaching ? <span>{slot?.slot_type.replaceAll("_", " ")}</span> : null}</div>; }))}
    {entries.map((entry) => { const day = dayLabels.findIndex((label) => label.toUpperCase() === entry.day_of_week); const column = days.indexOf(day); const rowIndex = rows.findIndex((row) => row.slot_index === entry.start_period_index); if (column < 0 || rowIndex < 0) return null; return <article tabIndex={0} aria-label={`${entry.subject_name}, ${entry.teacher_name}, ${entry.cohort_name}, ${entry.start_time} to ${entry.end_time}`} className={lessonCardClass(entry)} key={entry.entry_uuid} style={{ gridColumn: column + 2, gridRow: `${rowIndex + 2} / span ${entry.duration_periods}` }}><strong>{entry.subject_name}</strong><span>{entry.teacher_name}</span><span>{entry.cohort_name}</span>{entry.room_name ? <span>{entry.room_name}</span> : null}<small>{clock(entry.start_time)}–{clock(entry.end_time)}{entry.duration_periods > 1 ? " · Double lesson" : ""}</small>{entry.has_hard_conflict ? <b>Conflict</b> : null}</article>; })}
  </div></div>;
}
