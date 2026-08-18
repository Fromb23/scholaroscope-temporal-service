"use client";

import { RequireSession } from "../components/RequireSession";

export default function Home() {
  return (
    <RequireSession>
      {(session) => (
        <>
          <h2>Workspace identity and integration status</h2>
          <div className="grid">
            <section className="card">
              <h3>Workspace</h3>
              <p>{session.workspace_uuid}</p>
            </section>
            <section className="card">
              <h3>Actor</h3>
              <p>{session.actor_uuid}</p>
            </section>
            <section className="card">
              <h3>Session expiry</h3>
              <p>{new Date(session.expires_at).toLocaleString()}</p>
            </section>
          </div>
        </>
      )}
    </RequireSession>
  );
}
