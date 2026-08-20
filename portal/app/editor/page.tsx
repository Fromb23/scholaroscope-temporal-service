"use client";
import { RequireSession } from "../../components/RequireSession";
export default function EditorPage() {
  return <RequireSession>{() => <section className="card"><h2>Manual timetable editor</h2><p>Manual placement, move, pin, unschedule, and stale draft recovery operations.</p></section>}</RequireSession>;
}
