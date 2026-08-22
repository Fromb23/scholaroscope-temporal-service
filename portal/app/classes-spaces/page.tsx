"use client";

import { useEffect, useMemo, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, apiSend, type AcademicContextResponse, type ClassesSpacesResponse, type SpaceSummary } from "../../lib/api";

type RoomDraft = {
  name: string;
  kind: SpaceSummary["kind"];
  capacity: string;
  exclusive: boolean;
  status: string;
};

const emptyDraft: RoomDraft = { name: "", kind: "SPECIALIZED", capacity: "", exclusive: true, status: "ACTIVE" };

function draftFromSpace(space: SpaceSummary): RoomDraft {
  return {
    name: space.name,
    kind: space.kind,
    capacity: space.capacity == null ? "" : String(space.capacity),
    exclusive: space.exclusive,
    status: space.status,
  };
}

function roomPayload(draft: RoomDraft) {
  return {
    name: draft.name.trim(),
    kind: draft.kind,
    capacity: draft.capacity.trim() ? Number(draft.capacity) : null,
    exclusive: draft.exclusive,
    status: draft.status,
  };
}

export default function ClassesSpacesPage() {
  const [term, setTerm] = useState("");
  const [data, setData] = useState<ClassesSpacesResponse | null>(null);
  const [createDraft, setCreateDraft] = useState<RoomDraft>(emptyDraft);
  const [editing, setEditing] = useState<Record<string, RoomDraft>>({});
  const [error, setError] = useState<string | null>(null);

  const load = (termUuid = term) => {
    if (!termUuid) return;
    apiGet<ClassesSpacesResponse>(`/api/v1/classes-spaces?term_uuid=${termUuid}`)
      .then((response) => {
        setData(response);
        setError(null);
      })
      .catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Classes and spaces could not be loaded."));
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

  const assignedExclusiveRooms = useMemo(() => {
    const assigned = new Map<string, string>();
    for (const item of data?.classes ?? []) {
      if (item.default_room_uuid) assigned.set(item.default_room_uuid, item.cohort_uuid);
    }
    return assigned;
  }, [data?.classes]);

  async function saveNewRoom() {
    setError(null);
    try {
      await apiSend("/api/v1/rooms", "POST", roomPayload(createDraft));
      setCreateDraft(emptyDraft);
      load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Space could not be saved.");
    }
  }

  async function saveRoom(roomUuid: string) {
    setError(null);
    try {
      await apiSend(`/api/v1/rooms/${roomUuid}`, "PATCH", roomPayload(editing[roomUuid]));
      setEditing((current) => {
        const next = { ...current };
        delete next[roomUuid];
        return next;
      });
      load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Space could not be updated.");
    }
  }

  async function deleteRoom(roomUuid: string) {
    setError(null);
    try {
      await apiSend(`/api/v1/rooms/${roomUuid}`, "DELETE");
      load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Space could not be deleted. If it is used by timetable history, deactivate it instead.");
    }
  }

  return (
    <RequireSession>
      {() => (
        <>
          <section className="card">
            <h2>Classes & spaces</h2>
            <p className="muted">Classes are synchronized Scholaroscope cohorts. Physical spaces are optional timetable resources and never replace classes.</p>
            {error ? <div className="error">{error}</div> : null}

            <h3>Physical spaces ({data?.space_count ?? 0})</h3>
            <p className="muted">Create rooms, labs, fields, or shared facilities before assigning them as class defaults. A class can remain schedulable with no physical room.</p>
            <div className="toolbar">
              <input value={createDraft.name} onChange={(event) => setCreateDraft((draft) => ({ ...draft, name: event.target.value }))} placeholder="e.g. Science Laboratory" />
              <select value={createDraft.kind} onChange={(event) => setCreateDraft((draft) => ({ ...draft, kind: event.target.value as RoomDraft["kind"] }))}>
                <option value="SPECIALIZED">Specialized facility</option>
                <option value="SHARED">Shared room</option>
                <option value="GENERAL">General classroom</option>
              </select>
              <input type="number" min={1} value={createDraft.capacity} onChange={(event) => setCreateDraft((draft) => ({ ...draft, capacity: event.target.value }))} placeholder="Capacity" />
              <label className="check-row"><input type="checkbox" checked={createDraft.exclusive} onChange={(event) => setCreateDraft((draft) => ({ ...draft, exclusive: event.target.checked }))} /> Exclusive</label>
              <button disabled={!createDraft.name.trim()} onClick={saveNewRoom}>Add space</button>
            </div>
            {!data?.spaces.length ? <div className="empty">No physical spaces configured. This is valid.</div> : <div className="space-list">{data.spaces.map((space) => {
              const draft = editing[space.room_uuid];
              return <article key={space.room_uuid}>
                {draft ? <div className="toolbar">
                  <input value={draft.name} onChange={(event) => setEditing((current) => ({ ...current, [space.room_uuid]: { ...draft, name: event.target.value } }))} />
                  <select value={draft.kind} onChange={(event) => setEditing((current) => ({ ...current, [space.room_uuid]: { ...draft, kind: event.target.value as RoomDraft["kind"] } }))}>
                    <option value="SPECIALIZED">Specialized</option><option value="SHARED">Shared</option><option value="GENERAL">General</option>
                  </select>
                  <input type="number" min={1} value={draft.capacity} onChange={(event) => setEditing((current) => ({ ...current, [space.room_uuid]: { ...draft, capacity: event.target.value } }))} placeholder="Capacity" />
                  <label className="check-row"><input type="checkbox" checked={draft.exclusive} onChange={(event) => setEditing((current) => ({ ...current, [space.room_uuid]: { ...draft, exclusive: event.target.checked } }))} /> Exclusive</label>
                  <select value={draft.status} onChange={(event) => setEditing((current) => ({ ...current, [space.room_uuid]: { ...draft, status: event.target.value } }))}><option value="ACTIVE">Active</option><option value="DISABLED">Disabled</option></select>
                  <button disabled={!draft.name.trim()} onClick={() => saveRoom(space.room_uuid)}>Save</button>
                  <button type="button" onClick={() => setEditing((current) => { const next = { ...current }; delete next[space.room_uuid]; return next; })}>Cancel</button>
                </div> : <>
                  <div><strong>{space.name}</strong><span>{space.kind.toLowerCase()} · {space.capacity ? `capacity ${space.capacity}` : "capacity not set"} · {space.exclusive ? "exclusive" : "shared"} · {space.status.toLowerCase()}</span></div>
                  <button onClick={() => setEditing((current) => ({ ...current, [space.room_uuid]: draftFromSpace(space) }))}>Edit</button>
                  <button onClick={() => apiSend(`/api/v1/rooms/${space.room_uuid}`, "PATCH", { status: space.status === "ACTIVE" ? "DISABLED" : "ACTIVE" }).then(() => load()).catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Space could not be updated."))}>{space.status === "ACTIVE" ? "Deactivate" : "Reactivate"}</button>
                  <button onClick={() => deleteRoom(space.room_uuid)}>Delete</button>
                </>}
              </article>;
            })}</div>}
          </section>

          <section className="card">
            <h3>Synchronized classes ({data?.class_count ?? 0})</h3>
            <p className="muted">Assign an optional default physical classroom. Leaving “No default physical room” is a valid configuration.</p>
            {!data?.classes.length ? <div className="empty">No active classes are synchronized for this term.</div> : <div className="class-list">{data.classes.map((item) => (
              <article key={item.cohort_uuid}>
                <div>
                  <strong>{item.name}</strong>
                  <span>{item.enrollment_count} enrolled learners</span>
                  {item.capacity_mismatch ? <span className="warning-text">Default room capacity is below enrollment: {item.default_room_capacity} seats for {item.enrollment_count} learners.</span> : null}
                </div>
                <label>Default physical room
                  <select value={item.default_room_uuid} onChange={(event) => apiSend(`/api/v1/classes/${item.cohort_uuid}/default-room`, "PATCH", { room_uuid: event.target.value }).then(() => load()).catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Classroom could not be updated."))}>
                    <option value="">No default physical room</option>
                    {data.spaces.filter((space) => space.status === "ACTIVE").map((space) => {
                      const assignedTo = assignedExclusiveRooms.get(space.room_uuid);
                      const assignedElsewhere = space.exclusive && assignedTo && assignedTo !== item.cohort_uuid;
                      const capacityWarning = space.capacity != null && space.capacity < item.enrollment_count ? ` — capacity ${space.capacity} < ${item.enrollment_count}` : "";
                      return <option key={space.room_uuid} value={space.room_uuid} disabled={!!assignedElsewhere}>{space.name}{space.exclusive ? " (exclusive)" : ""}{assignedElsewhere ? " — assigned to another class" : ""}{capacityWarning}</option>;
                    })}
                  </select>
                </label>
              </article>
            ))}</div>}
          </section>
        </>
      )}
    </RequireSession>
  );
}
