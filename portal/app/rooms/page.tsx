"use client";
import { RequireSession } from "../../components/RequireSession";
export default function RoomsPage() {
  return <RequireSession>{() => <section className="card"><h2>Room management</h2><p>Room capacity, suitability, and occupancy management.</p></section>}</RequireSession>;
}
