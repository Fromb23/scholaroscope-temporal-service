"use client";
import { useEffect, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, type AcademicContextResponse } from "../../lib/api";
export default function TermsPage() {
  const [context, setContext] = useState<AcademicContextResponse | null>(null);
  useEffect(() => { apiGet<AcademicContextResponse>("/api/v1/academic-context").then(setContext).catch(() => undefined); }, []);
  return <RequireSession>{() => <section className="card"><h2>Academic context</h2><p className="muted">Academic years and terms are synchronized from Scholaroscope and remain read-only here.</p>{context?.academic_years.map((year) => <article className="context-year" key={year.academic_year_uuid}><h3>{year.name} {year.is_current ? <span className="status-pill">Current academic year</span> : null}</h3><p className="muted">{year.curriculum_name}</p><div className="term-cards">{year.terms.map((term) => <a className="term-card" href={term.scheduling_permitted ? `/?term_uuid=${term.term_uuid}` : `/exceptions?term_uuid=${term.term_uuid}`} key={term.term_uuid}><strong>{term.name}</strong><span>{term.start_date} to {term.end_date}</span><span className={`term-state ${term.lifecycle.toLowerCase()}`}>{term.lifecycle.toLowerCase()}</span></a>)}</div></article>) ?? <p>Loading academic context…</p>}</section>}</RequireSession>;
}
