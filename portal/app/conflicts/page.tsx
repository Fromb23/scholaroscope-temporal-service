"use client";

import { useEffect, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet } from "../../lib/api";

type ConflictSummary = { summary: Array<{ conflict_type: string; severity: string; count: number }> };

export default function ConflictsPage() {
  const [result, setResult] = useState<ConflictSummary | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => { apiGet<ConflictSummary>("/api/v1/conflicts").then(setResult).catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Conflicts could not be loaded.")); }, []);
  return <RequireSession>{() => <section className="card">
    <h2>Conflict inspection</h2>
    <p className="muted">Unresolved hard and soft conflicts for the active workspace. Run validation after changing a draft.</p>
    {error ? <div className="error">{error}</div> : null}
    {!result?.summary.length ? <p>No unresolved conflicts.</p> : <table><thead><tr><th>Constraint</th><th>Severity</th><th>Affected entries</th></tr></thead><tbody>{result.summary.map((item) => <tr key={`${item.conflict_type}-${item.severity}`}><td>{item.conflict_type.replaceAll("_", " ")}</td><td>{item.severity}</td><td>{item.count}</td></tr>)}</tbody></table>}
  </section>}</RequireSession>;
}
