import { describe, expect, it } from "vitest";
import { configuredDays, lessonCardClass, periodRows, unconfiguredDemandCount, unscheduledPeriods, visibleEntries } from "./timetable-grid";
import type { CalendarResponse, TeachingDemand, TimetableEntry } from "./api";

const calendar: CalendarResponse = {
  status: "ACTIVE",
  calendar_version: null,
  slots: [
    { id: "m0", day_of_week: 0, slot_index: 0, start_time: "08:00", end_time: "08:40", slot_type: "LESSON" },
    { id: "m1", day_of_week: 0, slot_index: 1, start_time: "08:40", end_time: "09:00", slot_type: "BREAK" },
    { id: "t0", day_of_week: 1, slot_index: 0, start_time: "08:00", end_time: "08:40", slot_type: "LESSON" },
    { id: "t1", day_of_week: 1, slot_index: 1, start_time: "08:40", end_time: "09:00", slot_type: "BREAK" },
  ],
};

const doubleLesson = {
  entry_uuid: "entry", teacher_uuid: "teacher", teacher_name: "Daniel Njoroge",
  cohort_uuid: "class-a", cohort_name: "Grade 10 Yellow", subject_uuid: "math",
  subject_name: "Mathematics", subject_code: "MATH", cohort_subject_uuid: "class-math",
  room_uuid: "", room_name: "", day_of_week: "MONDAY", start_time: "08:00", end_time: "09:20",
  duration_minutes: 80, duration_periods: 2, start_period_index: 0, has_hard_conflict: false,
} satisfies TimetableEntry;

describe("weekly timetable grid", () => {
  it("renders configured days and preserves non-teaching period rows", () => {
    expect(configuredDays(calendar)).toEqual([0, 1]);
    expect(periodRows(calendar).map((row) => row.slot_type)).toEqual(["LESSON", "BREAK"]);
  });

  it("preserves a double lesson as one two-period entry", () => {
    expect(doubleLesson.duration_periods).toBe(2);
    expect(lessonCardClass(doubleLesson)).toContain("double-lesson");
  });

  it("filters class and teacher views without leaking other sessions", () => {
    const other = { ...doubleLesson, entry_uuid: "other", cohort_uuid: "class-b", teacher_uuid: "teacher-b" };
    expect(visibleEntries([doubleLesson, other], { kind: "CLASS", entityUuid: "class-a" })).toEqual([doubleLesson]);
    expect(visibleEntries([doubleLesson, other], { kind: "TEACHER", entityUuid: "teacher-b" })).toEqual([other]);
  });

  it("highlights remaining mandatory demand", () => {
    const demand = { teacher_uuid: "teacher", cohort_subject_uuid: "class-math", required_periods_per_cycle: 4 } as TeachingDemand;
    expect(unscheduledPeriods([demand], [doubleLesson])).toBe(2);
  });

  it("does not count missing demand as scheduled", () => {
    const demand = { teacher_uuid: "teacher", cohort_subject_uuid: "class-math", required_periods_per_cycle: null, demand_status: "UNCONFIGURED" } as TeachingDemand;
    expect(unscheduledPeriods([demand], [doubleLesson])).toBe(0);
    expect(unconfiguredDemandCount([demand])).toBe(1);
  });

  it("marks hard conflicts on the affected lesson card", () => {
    expect(lessonCardClass({ ...doubleLesson, has_hard_conflict: true })).toContain("has-conflict");
  });
});
