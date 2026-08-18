"use client";
import { RequireSession } from "../../components/RequireSession";
export default function ResourcesPage() {
  return <RequireSession>{() => <section className="card"><h2>Resource management</h2><p>Exclusive and shared scheduling resources.</p></section>}</RequireSession>;
}
