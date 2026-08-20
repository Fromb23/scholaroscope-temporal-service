"use client";

import { useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { RequireSession } from "../../../components/RequireSession";
import { apiGet, apiSend, type WorkspaceStatus } from "../../../lib/api";

type TimetableType = "LEARNING" | "EXAMINATION";

export default function NewTimetablePage() {
  const router = useRouter();
  const [workspace, setWorkspace] = useState<WorkspaceStatus | null>(null);
  const [timetableType, setTimetableType] = useState<TimetableType>("LEARNING");
  const [effectiveStart, setEffectiveStart] = useState("");
  const [effectiveEnd, setEffectiveEnd] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    apiGet<WorkspaceStatus>("/api/v1/workspace")
      .then((data) => {
        setWorkspace(data);
        if (data.current_term) {
          setEffectiveStart(data.current_term.start_date);
          setEffectiveEnd(data.current_term.end_date);
        }
      })
      .catch(() => setError("Workspace academic context is unavailable."));
  }, []);

  const defaultName = useMemo(() => {
    const term = workspace?.current_term;
    const label = timetableType === "LEARNING" ? "Learning" : "Examination";
    if (!workspace || !term) return `${label} timetable`;
    return `${workspace.display_name} ${term.academic_year_label} ${term.name} ${label} Timetable`;
  }, [timetableType, workspace]);

  const canCreate = Boolean(workspace?.current_term && effectiveStart && effectiveEnd);
  const counts = workspace?.counts ?? {};

  return (
    <RequireSession>
      {() => (
        <section className="card">
          <h2>Create timetable</h2>
          <p className="muted">
            Timetables are created only for the synchronized active term and use Scholaroscope academic dates.
          </p>

          {workspace?.current_term ? (
            <div className="grid">
              <section className="card">
                <h3>{workspace.display_name}</h3>
                <p>{workspace.current_term.academic_year_label} · {workspace.current_term.name}</p>
                <p className="muted">{workspace.current_term.start_date} to {workspace.current_term.end_date}</p>
              </section>
              <section className="card">
                <h3>Available demand</h3>
                <p>{counts.teaching_assignment_count ?? 0} teaching assignments</p>
                <p className="muted">{counts.class_count ?? 0} classes · {counts.teacher_count ?? 0} teachers · {counts.subject_count ?? 0} subjects</p>
              </section>
            </div>
          ) : (
            <div className="error">No eligible active term is synchronized. Complete the academic year, active term, and term calendar setup in Scholaroscope, then replay synchronization.</div>
          )}

          <div className="toolbar">
            <label>
              Timetable type
              <select value={timetableType} onChange={(event) => setTimetableType(event.target.value as TimetableType)}>
                <option value="LEARNING">Learning Timetable</option>
                <option value="EXAMINATION">Examination Timetable</option>
              </select>
            </label>
            <label>
              Effective start
              <input
                type="date"
                min={workspace?.current_term?.start_date}
                max={workspace?.current_term?.end_date}
                value={effectiveStart}
                onChange={(event) => setEffectiveStart(event.target.value)}
              />
            </label>
            <label>
              Effective end
              <input
                type="date"
                min={workspace?.current_term?.start_date}
                max={workspace?.current_term?.end_date}
                value={effectiveEnd}
                onChange={(event) => setEffectiveEnd(event.target.value)}
              />
            </label>
            <button
              type="button"
              disabled={!canCreate}
              onClick={() => {
                setError(null);
                apiSend<{ version_uuid: string }>("/api/v1/timetables", "POST", {
                  name: defaultName,
                  timetable_type: timetableType,
                  academic_term_uuid: workspace?.current_term?.term_uuid,
                  effective_start: effectiveStart,
                  effective_end: effectiveEnd,
                  scope_kind: "WORKSPACE",
                })
                  .then((created) => router.push(`/editor?version=${created.version_uuid}`))
                  .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
              }}
            >
              Create draft
            </button>
          </div>
          <p className="muted">Default name: {defaultName}</p>
          {timetableType === "EXAMINATION" ? (
            <div className="error">Examination drafts require synchronized examination windows and assessments. The API will reject invalid academic windows.</div>
          ) : null}
          {error ? <div className="error">{error}</div> : null}
        </section>
      )}
    </RequireSession>
  );
}
