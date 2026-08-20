"use client";
import { RequireSession } from "../../components/RequireSession";
export default function GeneratePage() {
  return <RequireSession>{() => <section className="card"><h2>Automatic draft generation</h2><p>Constraint-aware generation jobs will execute through bounded Go worker APIs.</p></section>}</RequireSession>;
}
