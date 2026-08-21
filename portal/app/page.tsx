"use client";

import { RequireSession } from "../components/RequireSession";
import { TimetableWorkspace } from "../components/TimetableWorkspace";

export default function Home() {
  return <RequireSession>{() => <TimetableWorkspace />}</RequireSession>;
}
