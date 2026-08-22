BEGIN;

DROP TABLE IF EXISTS timetable_generation_job;

ALTER TABLE timetable_version DROP COLUMN IF EXISTS academic_snapshot_hash;
ALTER TABLE external_workspace DROP CONSTRAINT IF EXISTS external_workspace_assignment_counts_check;
ALTER TABLE external_workspace DROP COLUMN IF EXISTS eligible_assignment_count;
ALTER TABLE external_workspace DROP COLUMN IF EXISTS source_assignment_count;
ALTER TABLE external_workspace DROP COLUMN IF EXISTS academic_snapshot_hash;

ALTER TABLE timetable DROP CONSTRAINT IF EXISTS timetable_calendar_id_fkey;
ALTER TABLE timetable
    ADD CONSTRAINT timetable_calendar_id_fkey
    FOREIGN KEY (calendar_id) REFERENCES calendar(id) ON DELETE RESTRICT;

ALTER TABLE room DROP CONSTRAINT IF EXISTS room_kind_check;
ALTER TABLE room DROP COLUMN IF EXISTS room_kind;

ALTER TABLE external_cohort DROP CONSTRAINT IF EXISTS external_cohort_default_room_workspace_fkey;
ALTER TABLE room DROP CONSTRAINT IF EXISTS room_workspace_identity_unique;
ALTER TABLE external_cohort DROP CONSTRAINT IF EXISTS external_cohort_enrollment_count_check;
ALTER TABLE external_cohort DROP COLUMN IF EXISTS default_room_id;
ALTER TABLE external_cohort DROP COLUMN IF EXISTS enrollment_count;

COMMIT;
