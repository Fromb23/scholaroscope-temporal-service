import Link from "next/link";
import type { ReactNode } from "react";
import type { PortalSession } from "../lib/api";

const nav = [
  ["Status", "/"],
  ["Terms", "/terms"],
  ["Learning timetable", "/learning"],
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

const examNav = ["Examinations", "/examinations"] as const;

export function PortalShell({
  session,
  children,
}: {
  session: PortalSession;
  children: ReactNode;
}) {
  const permissions = new Set(session.permissions ?? []);
  const visibleNav = permissions.has("timetable.examinations.manage")
    ? [...nav.slice(0, 3), examNav, ...nav.slice(3)]
    : nav;
  return (
    <div className="shell">
      <aside className="sidebar">
        <h1>Timetable Portal</h1>
        <p className="muted">Workspace</p>
        <p>{session.workspace_name ?? "Workspace"}</p>
        <p className="muted">{session.workspace_timezone ?? "Timezone unavailable"}</p>
        <p className="muted">Signed in as</p>
        <p>{session.actor_display_name ?? "Timetable manager"}</p>
        {session.actor_kind ? <p className="muted">{session.actor_kind.toLowerCase()}</p> : null}
        <nav className="nav">
          {visibleNav.map(([label, href]) => (
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
