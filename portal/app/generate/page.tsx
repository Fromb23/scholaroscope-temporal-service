"use client";

import { useEffect, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, apiSend, type TimetableListResponse } from "../../lib/api";

type SolveResult = {
  status: string;
  feasibility: { issues?: Array<{ code: string; message: string; required_capacity: number; available_capacity: number; suggested_action: string }> };
  validation: { hard_conflict_count: number; soft_violation_count: number; unscheduled_mandatory_lessons: number };
  metrics: { seed: number; solve_duration: number; iterations: number; restarts: number; scheduled_periods: number };
};

export default function GeneratePage() {
  const [timetables, setTimetables] = useState<TimetableListResponse["timetables"]>([]);
  const [versionID, setVersionID] = useState("");
  const [seed, setSeed] = useState(23);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<SolveResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    apiGet<TimetableListResponse>("/api/v1/timetables").then((response) => {
      const drafts = response.timetables.filter((item) => item.version_uuid && item.status !== "PUBLISHED" && item.type === "LEARNING");
      setTimetables(drafts);
      setVersionID((current) => current || drafts[0]?.version_uuid || "");
    }).catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Timetable drafts could not be loaded."));
  }, []);

  return <RequireSession>{() => (
    <section className="card">
      <h2>Automatic draft generation</h2>
      <p className="muted">Generate a complete weekly learning timetable with bounded execution. Partial or conflicting results remain drafts and cannot be published.</p>
      <div className="grid">
        <label>Draft
          <select value={versionID} onChange={(event) => setVersionID(event.target.value)}>
            {timetables.map((item) => <option key={item.version_uuid ?? item.timetable_uuid} value={item.version_uuid ?? ""}>{item.name} · version {item.version_number}</option>)}
          </select>
        </label>
        <label>Deterministic seed <input type="number" value={seed} onChange={(event) => setSeed(Number(event.target.value))} /></label>
      </div>
      <button type="button" disabled={!versionID || running} onClick={() => {
        setRunning(true); setError(null); setResult(null);
        apiSend<SolveResult>(`/api/v1/versions/${versionID}/generate`, "POST", { seed, time_budget_ms: 30000, iteration_budget: 5000000, restarts: 5 })
          .then(setResult)
          .catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Generation failed unexpectedly."))
          .finally(() => setRunning(false));
      }}>{running ? "Generating…" : "Generate bounded draft"}</button>
      {error ? <div className="error">{error}</div> : null}
      {result ? <section className="card">
        <h3>{result.status.replaceAll("_", " ")}</h3>
        <div className="grid">
          <p><strong>{result.metrics.scheduled_periods}</strong><br /><span className="muted">scheduled periods</span></p>
          <p><strong>{result.validation.unscheduled_mandatory_lessons}</strong><br /><span className="muted">unscheduled mandatory</span></p>
          <p><strong>{result.validation.hard_conflict_count}</strong><br /><span className="muted">hard conflicts</span></p>
          <p><strong>{result.validation.soft_violation_count}</strong><br /><span className="muted">soft violations</span></p>
        </div>
        {result.feasibility.issues?.length ? <div><h3>Feasibility bottlenecks</h3>{result.feasibility.issues.map((issue) => <article className="card" key={`${issue.code}-${issue.message}`}><strong>{issue.message}</strong><p>Required: {issue.required_capacity}; available: {issue.available_capacity}.</p><p className="muted">{issue.suggested_action}</p></article>)}</div> : <p className="muted">Preflight found no mathematical capacity bottleneck.</p>}
      </section> : null}
    </section>
  )}</RequireSession>;
}
