BEGIN;

DROP TABLE IF EXISTS timetable_entry_learner_occupancy;

ALTER TABLE timetable_entry
    DROP COLUMN IF EXISTS teaching_assignment_uuids,
    DROP COLUMN IF EXISTS cohort_subject_uuids,
    DROP COLUMN IF EXISTS cohort_uuids,
    DROP COLUMN IF EXISTS learner_count,
    DROP COLUMN IF EXISTS parallel_block_uuid,
    DROP COLUMN IF EXISTS delivery_group_uuid;

DROP TABLE IF EXISTS timetable_parallel_block_member;
DROP TABLE IF EXISTS timetable_parallel_block;
DROP TABLE IF EXISTS timetable_delivery_group_learner;
DROP TABLE IF EXISTS timetable_delivery_group_assignment;
DROP TABLE IF EXISTS timetable_delivery_group;
DROP TABLE IF EXISTS external_learner_subject_enrollment;
DROP TABLE IF EXISTS external_learner_cohort_membership;
DROP TABLE IF EXISTS external_learner;

ALTER TABLE external_workspace
    DROP CONSTRAINT IF EXISTS external_workspace_learner_counts_check,
    DROP COLUMN IF EXISTS learner_enrollment_snapshot_hash,
    DROP COLUMN IF EXISTS eligible_learner_count,
    DROP COLUMN IF EXISTS source_learner_count;

ALTER TABLE external_teaching_assignment DROP CONSTRAINT IF EXISTS external_teaching_assignment_workspace_identity_unique;
ALTER TABLE external_cohort_subject DROP CONSTRAINT IF EXISTS external_cohort_subject_workspace_identity_unique;
ALTER TABLE external_subject DROP CONSTRAINT IF EXISTS external_subject_workspace_identity_unique;
ALTER TABLE external_cohort DROP CONSTRAINT IF EXISTS external_cohort_workspace_identity_unique;
ALTER TABLE external_academic_term DROP CONSTRAINT IF EXISTS external_academic_term_workspace_identity_unique;
ALTER TABLE external_academic_year DROP CONSTRAINT IF EXISTS external_academic_year_workspace_identity_unique;
ALTER TABLE external_actor DROP CONSTRAINT IF EXISTS external_actor_workspace_identity_unique;

COMMIT;
