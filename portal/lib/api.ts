export type PortalSession = {
  workspace_uuid: string;
  workspace_name?: string;
  workspace_timezone?: string;
  actor_uuid: string;
  actor_display_name?: string;
  actor_kind?: string;
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
  actor?: {
    actor_uuid: string;
    display_name: string;
    actor_kind: string;
  };
  current_academic_year?: {
    academic_year_uuid: string;
    academic_year_ref: string;
    name: string;
    start_date: string;
    end_date: string;
    is_current: boolean;
    status: string;
  } | null;
  current_term?: {
    term_uuid: string;
    name: string;
    academic_year_label: string;
    start_date: string;
    end_date: string;
    status: string;
    calendar_ready?: boolean;
    is_current?: boolean;
  } | null;
  schedulable_term?: {
    term_uuid: string;
    name: string;
    academic_year_label: string;
    start_date: string;
    end_date: string;
    status: string;
    calendar_ready?: boolean;
    is_current?: boolean;
    is_upcoming?: boolean;
  } | null;
  counts?: Record<string, number>;
  readiness?: {
    status: string;
    checks: Array<{ code: string; ok: boolean }>;
  };
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
  is_active?: boolean;
  break_structure?: Array<{ label: string; start_time: string; end_time: string; kind?: string }>;
};

export type CalendarResponse = {
  status: string;
  calendar_version: CalendarVersion | null;
  slots: Array<{
    id: string;
    day_of_week: number;
    start_time: string;
    end_time: string;
    slot_index: number;
    slot_type: string;
  }>;
};

export type TeacherAssignmentSummary = {
  cohort_uuid: string;
  cohort_name: string;
  subject_uuid: string;
  subject_name: string;
  cohort_subject_uuid: string;
  cohort_subject_ref: string;
};

export type TeacherSummary = {
  actor_uuid: string;
  scholaroscope_user_ref: string;
  display_name: string;
  actor_kind: "TEACHER";
  actor_kinds: string[];
  status: string;
  assignments: TeacherAssignmentSummary[];
};

export type TeachersResponse = {
  teachers: TeacherSummary[];
  count: number;
};

export type TimetableListItem = {
  timetable_uuid: string;
  name: string;
  type: string;
  version_uuid: string | null;
  version_number: number | null;
  status: string | null;
  effective_start: string;
  effective_end: string;
  published_at: string | null;
  term_uuid: string | null;
  term_name: string;
  academic_year_label: string;
};

export type TimetableListResponse = {
  timetables: TimetableListItem[];
  count: number;
};

export type TeachingDemand = {
  teaching_assignment_uuid: string;
  teacher_uuid: string;
  teacher_name: string;
  cohort_subject_uuid: string;
  cohort_uuid: string;
  cohort_name: string;
  subject_uuid: string;
  subject_name: string;
  required_periods_per_cycle: number;
  required_double_lessons: number;
};

export type TeachingDemandResponse = {
  demands: TeachingDemand[];
  count: number;
  status: string;
};

export type CalendarException = {
  exception_uuid: string;
  date: string;
  kind: string;
  title: string;
  blocks_learning: boolean;
  academic_term_uuid: string;
  calendar_name: string;
  source: string;
  end_date: string;
};

export type CalendarExceptionResponse = {
  exceptions: CalendarException[];
  count: number;
  academic_context: AcademicTerm;
  effective_start: string;
  effective_end: string;
};

export type AcademicTerm = {
  term_uuid: string;
  academic_year_uuid: string;
  name: string;
  academic_year_label: string;
  start_date: string;
  end_date: string;
  lifecycle: "ACTIVE" | "UPCOMING" | "ENDED" | "UNAVAILABLE";
  scheduling_permitted: boolean;
  calendar_ready: boolean;
  is_current: boolean;
};

export type AcademicContextResponse = {
  academic_years: Array<{
    academic_year_uuid: string;
    name: string;
    start_date: string;
    end_date: string;
    is_current: boolean;
    lifecycle: string;
    curriculum_name: string;
    terms: AcademicTerm[];
  }>;
  selected_term: AcademicTerm | null;
  has_active_term: boolean;
};

export type ClassSummary = {
  cohort_uuid: string;
  name: string;
  level: string;
  stream: string;
  enrollment_count: number;
  status: string;
  default_room_uuid: string;
  default_room_name: string;
};

export type SpaceSummary = {
  room_uuid: string;
  name: string;
  capacity: number | null;
  exclusive: boolean;
  status: string;
  kind: "GENERAL" | "SPECIALIZED" | "SHARED";
};

export type ClassesSpacesResponse = {
  academic_context: AcademicTerm | null;
  classes: ClassSummary[];
  class_count: number;
  spaces: SpaceSummary[];
  space_count: number;
};

export type WorkflowResponse = {
  state: string;
  explanation: string;
  blocking_conditions: Array<{ code: string; message: string; action_label: string; action_target: string }>;
  recommended_action: { label: string; target: string };
  secondary_actions: Array<{ label: string; target: string }>;
  active_term: AcademicTerm | null;
  relevant_timetable: {
    timetable_uuid: string | null;
    version_uuid: string | null;
    version_number: number | null;
    status: string;
  };
  progress: { completed: number; total: number; assignments: number; teaching_periods: number };
  synchronization: { status: string; last_successful_sync_at: string | null; integration_health: string; source_assignment_count: number; eligible_assignment_count: number };
  last_updated_at: string | null;
};

export type TimetableEntry = {
  entry_uuid: string;
  teacher_uuid: string;
  teacher_name: string;
  cohort_uuid: string;
  cohort_name: string;
  subject_uuid: string;
  subject_name: string;
  subject_code: string;
  cohort_subject_uuid: string;
  room_uuid: string;
  room_name: string;
  day_of_week: string;
  start_time: string;
  end_time: string;
  duration_minutes: number;
  duration_periods: number;
  start_period_index: number;
  has_hard_conflict: boolean;
};

export type VersionDetail = {
  version_uuid: string;
  timetable_uuid: string;
  version_number: number;
  status: string;
  name: string;
  term_uuid: string;
  term_name: string;
  academic_year_label: string;
  effective_start: string;
  effective_end: string;
  validation: Record<string, unknown>;
  entries: TimetableEntry[];
};

export type PortalErrorContract = {
  type: string;
  code: string;
  message: string;
  details: Record<string, unknown>;
  action?: { label: string; target: string };
};

export class PortalApiError extends Error {
  contract: PortalErrorContract;

  constructor(contract: PortalErrorContract) {
    super(contract.message);
    this.contract = contract;
  }
}

const apiBase = process.env.NEXT_PUBLIC_TEMPORAL_API_BASE_URL ?? "http://localhost:8081";

async function responseError(response: Response): Promise<Error> {
  let message = `${response.status} ${response.statusText}`;
  try {
    const payload = await response.json() as { error?: PortalErrorContract };
    if (payload.error?.message && payload.error.code) return new PortalApiError(payload.error);
    message = payload.error?.message ?? message;
  } catch {
    // A non-JSON gateway response is an unexpected failure; retain HTTP context.
  }
  return new Error(message);
}

export async function apiGet<T>(path: string): Promise<T> {
  const response = await fetch(`${apiBase}${path}`, {
    credentials: "include",
    cache: "no-store",
  });
  if (!response.ok) {
    throw await responseError(response);
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
    throw await responseError(response);
  }
  return response.json() as Promise<T>;
}
