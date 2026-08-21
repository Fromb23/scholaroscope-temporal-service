DROP INDEX IF EXISTS external_actor_role_lookup_idx;
DROP TABLE IF EXISTS external_actor_role;

DROP INDEX IF EXISTS external_calendar_event_year_idx;
ALTER TABLE external_calendar_event
    DROP COLUMN IF EXISTS scholaroscope_academic_year_ref,
    DROP COLUMN IF EXISTS academic_year_uuid;

DROP INDEX IF EXISTS external_cohort_year_idx;
ALTER TABLE external_cohort
    DROP COLUMN IF EXISTS academic_year_uuid;

DROP INDEX IF EXISTS external_academic_term_year_idx;
ALTER TABLE external_academic_term
    DROP COLUMN IF EXISTS is_frozen,
    DROP COLUMN IF EXISTS calendar_ready,
    DROP COLUMN IF EXISTS scholaroscope_academic_year_ref,
    DROP COLUMN IF EXISTS academic_year_uuid;

DROP INDEX IF EXISTS external_academic_year_one_current_idx;
DROP TABLE IF EXISTS external_academic_year;
