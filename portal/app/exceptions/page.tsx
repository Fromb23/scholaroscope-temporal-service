"use client";
import { RequireSession } from "../../components/RequireSession";
export default function ExceptionsPage() {
  return <RequireSession>{() => <section className="card"><h2>Calendar exceptions</h2><p>Holiday, closure, special event, shortened day, examination period, and teacher leave workflows.</p></section>}</RequireSession>;
}
