"use client";

import { useEffect, useState } from "react";
import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, apiSend, type TeachingDemandResponse, type TimetableListResponse } from "../../lib/api";

type VersionDetail = {
  version_uuid: string;
  status: string;
  entries: Array<Record<string, string | number>>;
};

type RoomResponse = {
  rooms: Array<{ room_uuid: string; name: string }>;
};

export default function EditorPage() {
  return (
    <Suspense fallback={<section className="card">Loading editor…</section>}>
      <EditorContent />
    </Suspense>
  );
}

function EditorContent() {
  const search = useSearchParams();
  const [versionId, setVersionId] = useState(search.get("version") ?? "");
  const [versions, setVersions] = useState<TimetableListResponse | null>(null);
  const [detail, setDetail] = useState<VersionDetail | null>(null);
  const [demands, setDemands] = useState<TeachingDemandResponse | null>(null);
  const [rooms, setRooms] = useState<RoomResponse | null>(null);
  const [selectedDemand, setSelectedDemand] = useState("");
  const [roomUuid, setRoomUuid] = useState("");
  const [day, setDay] = useState("1");
  const [startTime, setStartTime] = useState("08:00");
  const [endTime, setEndTime] = useState("08:40");
  const [error, setError] = useState<string | null>(null);

  function load(selected = versionId) {
    apiGet<TimetableListResponse>("/api/v1/timetables").then(setVersions).catch(() => undefined);
    apiGet<TeachingDemandResponse>("/api/v1/teaching-demands").then(setDemands).catch(() => undefined);
    apiGet<RoomResponse>("/api/v1/rooms").then(setRooms).catch(() => undefined);
    if (selected) {
      apiGet<VersionDetail>(`/api/v1/timetable-versions/${selected}`)
        .then(setDetail)
        .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
    }
  }

  useEffect(() => {
    load(versionId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const demand = demands?.demands.find((item) => item.teaching_assignment_uuid === selectedDemand);

  return (
    <RequireSession>
      {() => (
        <section className="card">
          <h2>Manual draft editor</h2>
          <p className="muted">Select a draft version, choose a synchronized teaching demand, and place it in the weekly timetable.</p>
          <div className="toolbar">
            <select value={versionId} onChange={(event) => { setVersionId(event.target.value); load(event.target.value); }}>
              <option value="">Select draft version</option>
              {versions?.timetables.filter((item) => item.version_uuid).map((item) => (
                <option key={item.version_uuid ?? item.timetable_uuid} value={item.version_uuid ?? ""}>
                  {item.name} v{item.version_number} ({item.status})
                </option>
              ))}
            </select>
          </div>
          {versionId ? (
            <>
              <h3>Add lesson entry</h3>
              <div className="toolbar">
                <select value={selectedDemand} onChange={(event) => setSelectedDemand(event.target.value)}>
                  <option value="">Select teaching demand</option>
                  {demands?.demands.map((item) => (
                    <option key={item.teaching_assignment_uuid} value={item.teaching_assignment_uuid}>
                      {item.teacher_name} · {item.cohort_name} · {item.subject_name}
                    </option>
                  ))}
                </select>
                <select value={roomUuid} onChange={(event) => setRoomUuid(event.target.value)}>
                  <option value="">No room</option>
                  {rooms?.rooms.map((room) => <option key={room.room_uuid} value={room.room_uuid}>{room.name}</option>)}
                </select>
                <select value={day} onChange={(event) => setDay(event.target.value)}>
                  <option value="1">Monday</option>
                  <option value="2">Tuesday</option>
                  <option value="3">Wednesday</option>
                  <option value="4">Thursday</option>
                  <option value="5">Friday</option>
                  <option value="6">Saturday</option>
                  <option value="0">Sunday</option>
                </select>
                <input type="time" value={startTime} onChange={(event) => setStartTime(event.target.value)} />
                <input type="time" value={endTime} onChange={(event) => setEndTime(event.target.value)} />
                <button
                  type="button"
                  disabled={!demand}
                  onClick={() => {
                    if (!demand) return;
                    setError(null);
                    apiSend(`/api/v1/timetable-versions/${versionId}/entries`, "POST", {
                      teacher_uuid: demand.teacher_uuid,
                      cohort_uuid: demand.cohort_uuid,
                      subject_uuid: demand.subject_uuid,
                      cohort_subject_uuid: demand.cohort_subject_uuid,
                      room_uuid: roomUuid || undefined,
                      day_of_week: Number(day),
                      start_period_index: 0,
                      duration_periods: 1,
                      start_time: startTime,
                      end_time: endTime,
                    })
                      .then(() => load(versionId))
                      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
                  }}
                >
                  Add entry
                </button>
              </div>
            </>
          ) : null}
          {error ? <div className="error">{error}</div> : null}
          {detail ? (
            <>
              <h3>Entries ({detail.status})</h3>
              {!detail.entries.length ? <p>No draft entries yet.</p> : (
                <table>
                  <thead>
                    <tr><th>Day</th><th>Time</th><th>Teacher</th><th>Class</th><th>Subject</th><th>Room</th><th /></tr>
                  </thead>
                  <tbody>
                    {detail.entries.map((entry) => (
                      <tr key={String(entry.entry_uuid)}>
                        <td>{entry.day_of_week}</td>
                        <td>{entry.start_time}–{entry.end_time}</td>
                        <td>{entry.teacher_name}</td>
                        <td>{entry.cohort_name}</td>
                        <td>{entry.subject_name}</td>
                        <td>{entry.room_name}</td>
                        <td>
                          <button
                            type="button"
                            onClick={() => {
                              setError(null);
                              apiSend(`/api/v1/timetable-versions/${versionId}/entries/${entry.entry_uuid}`, "DELETE")
                                .then(() => load(versionId))
                                .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
                            }}
                          >
                            Remove
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </>
          ) : null}
        </section>
      )}
    </RequireSession>
  );
}
