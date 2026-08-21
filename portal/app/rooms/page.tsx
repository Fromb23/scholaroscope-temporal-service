"use client";
import { useEffect, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, apiSend } from "../../lib/api";
type Room = { room_uuid: string; name: string; capacity: number | null; exclusive: boolean; status: string };
export default function RoomsPage() {
  const [rooms, setRooms] = useState<Room[]>([]); const [name, setName] = useState(""); const [capacity, setCapacity] = useState(""); const [error, setError] = useState<string | null>(null);
  const load = () => { apiGet<{ rooms: Room[] }>("/api/v1/rooms").then((response) => setRooms(response.rooms)).catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Rooms could not be loaded.")); };
  useEffect(() => { load(); }, []);
  return <RequireSession>{() => <section className="card"><h2>Optional rooms</h2><p className="muted">Rooms are optional resources. Learning timetables remain valid without room allocation.</p><div className="toolbar"><input value={name} onChange={(event) => setName(event.target.value)} placeholder="Room name" /><input type="number" min={1} value={capacity} onChange={(event) => setCapacity(event.target.value)} placeholder="Capacity" /><button type="button" disabled={!name.trim()} onClick={() => { setError(null); apiSend("/api/v1/rooms", "POST", { name, capacity: capacity ? Number(capacity) : null }).then(() => { setName(""); setCapacity(""); load(); }).catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Room could not be saved.")); }}>Save room</button></div>{error ? <div className="error">{error}</div> : null}{!rooms.length ? <p>No rooms configured. This is valid when room allocation is not required.</p> : <table><thead><tr><th>Room</th><th>Capacity</th><th>Exclusive</th><th>Status</th></tr></thead><tbody>{rooms.map((room) => <tr key={room.room_uuid}><td>{room.name}</td><td>{room.capacity ?? "Not set"}</td><td>{room.exclusive ? "Yes" : "No"}</td><td>{room.status}</td></tr>)}</tbody></table>}</section>}</RequireSession>;
}
