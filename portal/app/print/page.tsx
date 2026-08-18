"use client";
import { RequireSession } from "../../components/RequireSession";
export default function PrintPage() {
  return <RequireSession>{() => <section className="card"><h2>Printable timetable previews</h2><p>Printable projections are generated from real timetable API responses with explicit filters.</p></section>}</RequireSession>;
}
