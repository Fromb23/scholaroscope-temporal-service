BEGIN;

ALTER TABLE external_teaching_assignment
    ADD COLUMN IF NOT EXISTS source_model text NOT NULL DEFAULT 'academic.CohortSubjectInstructor',
    ADD COLUMN IF NOT EXISTS source_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS academic_year_ref text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS scheduling_requirements jsonb NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX IF NOT EXISTS external_teaching_assignment_source_idx
    ON external_teaching_assignment(workspace_id, source_model, source_id);

CREATE TABLE IF NOT EXISTS solver_run (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    timetable_version_id uuid NOT NULL REFERENCES timetable_version(id) ON DELETE CASCADE,
    status text NOT NULL,
    seed bigint NOT NULL,
    preflight_duration_ns bigint NOT NULL DEFAULT 0,
    solve_duration_ns bigint NOT NULL DEFAULT 0,
    iterations integer NOT NULL DEFAULT 0,
    restarts integer NOT NULL DEFAULT 0,
    scheduled_periods integer NOT NULL DEFAULT 0,
    unscheduled_mandatory_lessons integer NOT NULL DEFAULT 0,
    hard_conflicts integer NOT NULL DEFAULT 0,
    soft_violations integer NOT NULL DEFAULT 0,
    feasibility jsonb NOT NULL DEFAULT '{}'::jsonb,
    validation jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT solver_run_status_check CHECK (status IN ('COMPLETE', 'COMPLETE_WITH_SOFT_VIOLATIONS', 'INFEASIBLE', 'TIME_BUDGET_EXCEEDED', 'PARTIAL_DRAFT'))
);

CREATE INDEX IF NOT EXISTS solver_run_version_idx
    ON solver_run(workspace_id, timetable_version_id, created_at DESC);

-- Materialized per-period occupancy closes the race between portal validation
-- and entry insertion. Every write path is guarded by the same database keys.
CREATE TABLE IF NOT EXISTS timetable_entry_occupancy (
    timetable_entry_id uuid NOT NULL REFERENCES timetable_entry(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    timetable_version_id uuid NOT NULL REFERENCES timetable_version(id) ON DELETE CASCADE,
    day_of_week smallint NOT NULL,
    period_index smallint NOT NULL,
    teacher_uuid uuid,
    cohort_uuid uuid,
    room_id uuid,
    resource_id uuid,
    PRIMARY KEY (timetable_entry_id, period_index)
);

CREATE UNIQUE INDEX IF NOT EXISTS timetable_entry_occupancy_teacher_idx
    ON timetable_entry_occupancy(workspace_id, timetable_version_id, day_of_week, period_index, teacher_uuid)
    WHERE teacher_uuid IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS timetable_entry_occupancy_cohort_idx
    ON timetable_entry_occupancy(workspace_id, timetable_version_id, day_of_week, period_index, cohort_uuid)
    WHERE cohort_uuid IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS timetable_entry_occupancy_room_idx
    ON timetable_entry_occupancy(workspace_id, timetable_version_id, day_of_week, period_index, room_id)
    WHERE room_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS timetable_entry_occupancy_resource_idx
    ON timetable_entry_occupancy(workspace_id, timetable_version_id, day_of_week, period_index, resource_id)
    WHERE resource_id IS NOT NULL;

CREATE OR REPLACE FUNCTION refresh_timetable_entry_occupancy() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        DELETE FROM solver_run
        WHERE workspace_id = OLD.workspace_id
          AND timetable_version_id = OLD.timetable_version_id;
    ELSE
        DELETE FROM solver_run
        WHERE workspace_id = NEW.workspace_id
          AND timetable_version_id = NEW.timetable_version_id;
    END IF;
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        DELETE FROM timetable_entry_occupancy WHERE timetable_entry_id = OLD.id;
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        INSERT INTO timetable_entry_occupancy (
            timetable_entry_id, workspace_id, timetable_version_id, day_of_week,
            period_index, teacher_uuid, cohort_uuid, room_id, resource_id
        )
        SELECT NEW.id, NEW.workspace_id, NEW.timetable_version_id, NEW.day_of_week,
               period_index, NEW.teacher_uuid, NEW.cohort_uuid, NEW.room_id, NEW.resource_id
        FROM generate_series(
            NEW.start_period_index::integer,
            NEW.start_period_index::integer + NEW.duration_periods::integer - 1
        ) AS period_index;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS timetable_entry_occupancy_refresh ON timetable_entry;
CREATE TRIGGER timetable_entry_occupancy_refresh
AFTER INSERT OR UPDATE OR DELETE ON timetable_entry
FOR EACH ROW EXECUTE FUNCTION refresh_timetable_entry_occupancy();

INSERT INTO timetable_entry_occupancy (
    timetable_entry_id, workspace_id, timetable_version_id, day_of_week,
    period_index, teacher_uuid, cohort_uuid, room_id, resource_id
)
SELECT entry.id, entry.workspace_id, entry.timetable_version_id, entry.day_of_week,
       period_index, entry.teacher_uuid, entry.cohort_uuid, entry.room_id, entry.resource_id
FROM timetable_entry entry
CROSS JOIN LATERAL generate_series(
    entry.start_period_index::integer,
    entry.start_period_index::integer + entry.duration_periods::integer - 1
) AS period_index
ON CONFLICT DO NOTHING;

COMMIT;
