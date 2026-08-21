BEGIN;

DROP TRIGGER IF EXISTS timetable_entry_occupancy_refresh ON timetable_entry;
DROP FUNCTION IF EXISTS refresh_timetable_entry_occupancy();
DROP TABLE IF EXISTS timetable_entry_occupancy;
DROP TABLE IF EXISTS solver_run;
DROP INDEX IF EXISTS external_teaching_assignment_source_idx;
ALTER TABLE external_teaching_assignment
    DROP COLUMN IF EXISTS scheduling_requirements,
    DROP COLUMN IF EXISTS academic_year_ref,
    DROP COLUMN IF EXISTS source_id,
    DROP COLUMN IF EXISTS source_model;

COMMIT;
