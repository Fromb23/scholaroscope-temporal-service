"use client";
import { RequireSession } from "../../components/RequireSession";
import { TimetableWorkspace } from "../../components/TimetableWorkspace";
export default function TimetablePage() { return <RequireSession>{() => <TimetableWorkspace />}</RequireSession>; }
