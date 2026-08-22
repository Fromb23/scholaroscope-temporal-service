BEGIN;

ALTER TABLE time_slot DROP CONSTRAINT IF EXISTS time_slot_type_check;
ALTER TABLE time_slot
    ADD CONSTRAINT time_slot_type_check
    CHECK (slot_type IN ('LESSON', 'BREAK', 'LUNCH', 'ASSEMBLY', 'NON_TEACHING', 'PREP', 'TRANSITION', 'BUFFER', 'UNSCHEDULED', 'EXAM'));

CREATE TABLE IF NOT EXISTS timetable_demand_override (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    academic_term_uuid uuid NOT NULL REFERENCES external_academic_term(id) ON DELETE CASCADE,
    teaching_assignment_uuid uuid NOT NULL REFERENCES external_teaching_assignment(id) ON DELETE CASCADE,
    weekly_lesson_requirement integer NOT NULL,
    required_double_lessons integer NOT NULL DEFAULT 0,
    source text NOT NULL DEFAULT 'TIMETABLE_OVERRIDE',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT timetable_demand_override_requirement_check CHECK (weekly_lesson_requirement > 0),
    CONSTRAINT timetable_demand_override_double_check CHECK (required_double_lessons >= 0 AND required_double_lessons * 2 <= weekly_lesson_requirement),
    CONSTRAINT timetable_demand_override_source_check CHECK (source = 'TIMETABLE_OVERRIDE'),
    CONSTRAINT timetable_demand_override_scope_unique UNIQUE (workspace_id, academic_term_uuid, teaching_assignment_uuid)
);

CREATE INDEX IF NOT EXISTS timetable_demand_override_assignment_idx
    ON timetable_demand_override(workspace_id, teaching_assignment_uuid);

COMMIT;
