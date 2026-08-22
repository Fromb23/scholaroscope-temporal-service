import Link from "next/link";
import type { ReactNode } from "react";
import type { PortalSession } from "../lib/api";

const nav = [
  ["Timetable", "/"],
  ["School day", "/school-day"],
  ["Classes & teachers", "/classes-teachers"],
  ["Calendar", "/exceptions"],
  ["Classes & spaces", "/classes-spaces"],
  ["Logout", "/logout"],
] as const;

export function PortalShell({
  session,
  children,
}: {
  session: PortalSession;
  children: ReactNode;
}) {
  const visibleNav = nav;
  return (
    <div className="shell">
      <aside className="sidebar">
        <h1>Scholaroscope Timetable</h1>
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
