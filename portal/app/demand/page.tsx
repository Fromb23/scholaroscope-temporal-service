"use client";
import { RequireSession } from "../../components/RequireSession";
export default function DemandPage() {
  return <RequireSession>{() => <section className="card"><h2>Timetable demand definition</h2><p>Demand is generated from synchronized teaching assignments and can be refined before draft generation.</p></section>}</RequireSession>;
}
