"use client";
import { RequireSession } from "../../components/RequireSession";
export default function AvailabilityPage() {
  return <RequireSession>{() => <section className="card"><h2>Teacher availability</h2><p>Availability edits call tenant-protected teacher availability APIs.</p></section>}</RequireSession>;
}
