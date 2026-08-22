BEGIN;

ALTER TABLE external_workspace
    ADD COLUMN IF NOT EXISTS academic_snapshot_hash text,
    ADD COLUMN IF NOT EXISTS source_assignment_count integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS eligible_assignment_count integer NOT NULL DEFAULT 0;

ALTER TABLE external_workspace
    ADD CONSTRAINT external_workspace_assignment_counts_check
    CHECK (source_assignment_count >= 0 AND eligible_assignment_count >= 0 AND eligible_assignment_count <= source_assignment_count);

ALTER TABLE timetable_version
    ADD COLUMN IF NOT EXISTS academic_snapshot_hash text;

ALTER TABLE external_cohort
    ADD COLUMN IF NOT EXISTS enrollment_count integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS default_room_id uuid REFERENCES room(id) ON DELETE SET NULL;

ALTER TABLE external_cohort
    ADD CONSTRAINT external_cohort_enrollment_count_check CHECK (enrollment_count >= 0);

ALTER TABLE room
    ADD COLUMN IF NOT EXISTS room_kind text NOT NULL DEFAULT 'GENERAL';

ALTER TABLE room
    ADD CONSTRAINT room_kind_check CHECK (room_kind IN ('GENERAL', 'SPECIALIZED', 'SHARED'));

ALTER TABLE external_cohort DROP CONSTRAINT IF EXISTS external_cohort_default_room_id_fkey;
ALTER TABLE room ADD CONSTRAINT room_workspace_identity_unique UNIQUE (id, workspace_id);
ALTER TABLE external_cohort
    ADD CONSTRAINT external_cohort_default_room_workspace_fkey
    FOREIGN KEY (default_room_id, workspace_id) REFERENCES room(id, workspace_id) ON DELETE RESTRICT;

-- The portal and solver use org_calendar_version/time_slot. The foundational
-- foreign key incorrectly pointed at the unused legacy calendar table.
ALTER TABLE timetable DROP CONSTRAINT IF EXISTS timetable_calendar_id_fkey;
ALTER TABLE timetable
    ADD CONSTRAINT timetable_calendar_id_fkey
    FOREIGN KEY (calendar_id) REFERENCES org_calendar_version(id) ON DELETE RESTRICT;

CREATE TABLE IF NOT EXISTS timetable_generation_job (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    timetable_version_id uuid NOT NULL REFERENCES timetable_version(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'RUNNING',
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    error_code text,
    CONSTRAINT timetable_generation_job_status_check CHECK (status IN ('RUNNING', 'COMPLETED', 'FAILED'))
);

CREATE UNIQUE INDEX IF NOT EXISTS timetable_generation_job_one_running_idx
    ON timetable_generation_job(workspace_id, timetable_version_id)
    WHERE status = 'RUNNING';

CREATE INDEX IF NOT EXISTS timetable_generation_job_version_idx
    ON timetable_generation_job(workspace_id, timetable_version_id, started_at DESC);

COMMIT;
