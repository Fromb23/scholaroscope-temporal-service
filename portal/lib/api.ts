export type PortalSession = {
  workspace_uuid: string;
  actor_uuid: string;
  permissions?: string[];
  expires_at: string;
};

export type WorkspaceStatus = {
  workspace_uuid: string;
  display_name: string;
  timezone: string;
  status: string;
  provisioning_state: string;
  integration_health: string;
  last_successful_sync_at: string | null;
  reconciliation_required: boolean;
};

export type CalendarVersion = {
  id: string;
  workspace_id?: string;
  org_id?: string;
  version_number?: number;
  learning_days?: string[];
  day_start_time?: string;
  day_end_time?: string;
  slot_duration_minutes?: number;
  status?: string;
};

export type CalendarResponse = {
  status: string;
  calendar_version: CalendarVersion | null;
  slots: unknown[];
};

const apiBase = process.env.NEXT_PUBLIC_TEMPORAL_API_BASE_URL ?? "http://localhost:8081";

export async function apiGet<T>(path: string): Promise<T> {
  const response = await fetch(`${apiBase}${path}`, {
    credentials: "include",
    cache: "no-store",
  });
  if (!response.ok) {
    throw new Error(`${response.status} ${response.statusText}`);
  }
  return response.json() as Promise<T>;
}

export async function apiSend<T>(path: string, method: string, body?: unknown): Promise<T> {
  const response = await fetch(`${apiBase}${path}`, {
    method,
    credentials: "include",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) {
    throw new Error(`${response.status} ${response.statusText}`);
  }
  return response.json() as Promise<T>;
}
