"use client";

import { useState } from "react";
import { apiSend } from "../../lib/api";

export default function LogoutPage() {
  const [status, setStatus] = useState("Ready to logout.");
  return (
    <main className="main">
      <section className="card">
        <h2>Logout</h2>
        <p>{status}</p>
        <button
          type="button"
          onClick={() => {
            apiSend("/portal/logout", "POST")
              .then(() => setStatus("Logged out. Launch again from Scholaroscope to continue."))
              .catch(() => setStatus("Logout request failed."));
          }}
        >
          Logout
        </button>
      </section>
    </main>
  );
}
