import type { CalendarResponse, TeachingDemand, TimetableEntry } from "./api";

export const dayLabels = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];

export type GridView = { kind: "WORKSPACE" | "CLASS" | "TEACHER"; entityUuid?: string };

export function configuredDays(calendar: CalendarResponse | null): number[] {
  return [...new Set((calendar?.slots ?? []).map((slot) => slot.day_of_week))].sort((a, b) => a - b);
}

export function periodRows(calendar: CalendarResponse | null) {
  const byIndex = new Map<number, CalendarResponse["slots"][number]>();
  for (const slot of calendar?.slots ?? []) {
    if (!byIndex.has(slot.slot_index)) byIndex.set(slot.slot_index, slot);
  }
  return [...byIndex.values()].sort((left, right) => left.slot_index - right.slot_index);
}

export function visibleEntries(entries: TimetableEntry[], view: GridView): TimetableEntry[] {
  if (view.kind === "CLASS") return entries.filter((entry) => entry.cohort_uuid === view.entityUuid || entry.cohorts?.some((cohort) => cohort.cohort_uuid === view.entityUuid));
  if (view.kind === "TEACHER") return entries.filter((entry) => entry.teacher_uuid === view.entityUuid);
  return entries;
}

export function entryAt(entries: TimetableEntry[], day: number, periodIndex: number): TimetableEntry | undefined {
  const dayName = dayLabels[day]?.toUpperCase();
  return entries.find((entry) => entry.day_of_week === dayName && entry.start_period_index === periodIndex);
}

export function subjectColorIndex(subjectUuid: string, paletteSize = 8): number {
  let hash = 0;
  for (const character of subjectUuid) hash = ((hash << 5) - hash + character.charCodeAt(0)) | 0;
  return Math.abs(hash) % paletteSize;
}

export function lessonCardClass(entry: TimetableEntry): string {
  return [
    "lesson-card",
    `subject-${subjectColorIndex(entry.subject_uuid)}`,
    entry.has_hard_conflict ? "has-conflict" : "",
    entry.duration_periods > 1 ? "double-lesson" : "",
    entry.delivery_group_uuid ? "merged-lesson" : "",
    entry.parallel_block_uuid ? "parallel-lesson" : "",
  ].filter(Boolean).join(" ");
}

export function unscheduledPeriods(demands: TeachingDemand[], entries: TimetableEntry[]): number {
  const scheduledByAssignment = new Map<string, number>();
  for (const entry of entries) {
    if (entry.teaching_assignment_uuids?.length) {
      for (const assignmentUuid of entry.teaching_assignment_uuids) {
        scheduledByAssignment.set(assignmentUuid, (scheduledByAssignment.get(assignmentUuid) ?? 0) + entry.duration_periods);
      }
    } else {
      const key = `${entry.teacher_uuid}:${entry.cohort_subject_uuid}`;
      scheduledByAssignment.set(key, (scheduledByAssignment.get(key) ?? 0) + entry.duration_periods);
    }
  }
  return demands.reduce((total, demand) => {
    if (demand.required_periods_per_cycle == null) return total;
    const scheduled = scheduledByAssignment.get(demand.teaching_assignment_uuid) ?? scheduledByAssignment.get(`${demand.teacher_uuid}:${demand.cohort_subject_uuid}`) ?? 0;
    return total + Math.max(0, demand.required_periods_per_cycle - scheduled);
  }, 0);
}

export function unconfiguredDemandCount(demands: TeachingDemand[]): number {
  return demands.filter((demand) => demand.required_periods_per_cycle == null || demand.demand_status === "UNCONFIGURED").length;
}
