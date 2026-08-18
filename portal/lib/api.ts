export type PortalSession = {
  workspace_uuid: string;
  actor_uuid: string;
  permissions?: string[];
  expires_at: string;
};

export type CalendarVersion = {
  id: string;
  workspace_id?: string;
  version_number?: number;
  status?: string;
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
