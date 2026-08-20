"use client";
import { RequireSession } from "../../components/RequireSession";
export default function TermsPage() {
  return <RequireSession>{() => <section className="card"><h2>Academic term selection</h2><p>Term projections are loaded from synchronized Scholaroscope data when the API exposes them.</p></section>}</RequireSession>;
}
