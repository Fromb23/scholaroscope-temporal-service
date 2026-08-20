"use client";
import { useEffect, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, apiSend } from "../../lib/api";
export default function RoomsPage() {
  const [rooms, setRooms] = useState<unknown>(null);
  const [name, setName] = useState("");
  const [capacity, setCapacity] = useState("");
  const [error, setError] = useState<string | null>(null);
  const load = () => {
    setError(null);
    apiGet("/api/v1/rooms")
      .then(setRooms)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
  };
  useEffect(() => {
    load();
  }, []);
  return (
    <RequireSession>
      {() => (
        <section className="card">
          <h2>Room management</h2>
          <p className="muted">Rooms are scoped to the authenticated workspace session.</p>
          <div className="toolbar">
            <input value={name} onChange={(event) => setName(event.target.value)} placeholder="Room name" />
            <input value={capacity} onChange={(event) => setCapacity(event.target.value)} placeholder="Capacity" />
            <button
              type="button"
              onClick={() => {
                setError(null);
                apiSend("/api/v1/rooms", "POST", {
                  name,
                  capacity: capacity ? Number(capacity) : null,
                })
                  .then(load)
                  .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
              }}
            >
              Save room
            </button>
          </div>
          {error ? <div className="error">{error}</div> : null}
          <pre>{JSON.stringify(rooms, null, 2)}</pre>
        </section>
      )}
    </RequireSession>
  );
}
