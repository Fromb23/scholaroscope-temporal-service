import Link from "next/link";
import type { ReactNode } from "react";
import type { PortalSession } from "../lib/api";

const nav = [
  ["Status", "/"],
  ["Terms", "/terms"],
  ["Learning timetable", "/learning"],
  ["Examinations", "/examinations"],
  ["Bell periods", "/bell-periods"],
  ["Exceptions", "/exceptions"],
  ["Teacher availability", "/availability"],
  ["Rooms", "/rooms"],
  ["Resources", "/resources"],
  ["Demand", "/demand"],
  ["Draft generation", "/generate"],
  ["Manual editor", "/editor"],
  ["Conflicts", "/conflicts"],
  ["Publication", "/publication"],
  ["Versions", "/versions"],
  ["Print", "/print"],
  ["Logout", "/logout"],
] as const;

export function PortalShell({
  session,
  children,
}: {
  session: PortalSession;
  children: ReactNode;
}) {
  return (
    <div className="shell">
      <aside className="sidebar">
        <h1>Timetable Portal</h1>
        <p className="muted">Workspace</p>
        <p>{session.workspace_uuid}</p>
        <nav className="nav">
          {nav.map(([label, href]) => (
            <Link key={href} href={href}>
              {label}
            </Link>
          ))}
        </nav>
      </aside>
      <main className="main">{children}</main>
    </div>
  );
}
