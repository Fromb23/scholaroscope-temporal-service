"use client";

import { useEffect, useState } from "react";
import { apiGet, type PortalSession } from "../lib/api";
import { PortalShell } from "./PortalShell";

export function RequireSession({ children }: { children: (session: PortalSession) => React.ReactNode }) {
  const [session, setSession] = useState<PortalSession | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    apiGet<PortalSession>("/portal/session")
      .then(setSession)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Session check failed"));
  }, []);

  if (error) {
    return (
      <main className="main">
        <div className="error">Your timetable portal session is expired or invalid. Launch again from Scholaroscope.</div>
      </main>
    );
  }
  if (!session) {
    return <main className="main">Loading session…</main>;
  }
  return <PortalShell session={session}>{children(session)}</PortalShell>;
}
