"use client";
import { RequireSession } from "../../components/RequireSession";
export default function VersionsPage() {
  return <RequireSession>{() => <section className="card"><h2>Version history</h2><p>Published, superseded, archived, and future-effective versions remain inspectable.</p></section>}</RequireSession>;
}
