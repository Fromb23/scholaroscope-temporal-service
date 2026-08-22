BEGIN;

ALTER TABLE external_workspace
    ADD COLUMN IF NOT EXISTS source_learner_count integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS eligible_learner_count integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS learner_enrollment_snapshot_hash text;

ALTER TABLE external_workspace
    ADD CONSTRAINT external_workspace_learner_counts_check
    CHECK (source_learner_count >= 0 AND eligible_learner_count >= 0 AND eligible_learner_count <= source_learner_count);

ALTER TABLE external_actor ADD CONSTRAINT external_actor_workspace_identity_unique UNIQUE (id, workspace_id);
ALTER TABLE external_academic_year ADD CONSTRAINT external_academic_year_workspace_identity_unique UNIQUE (id, workspace_id);
ALTER TABLE external_academic_term ADD CONSTRAINT external_academic_term_workspace_identity_unique UNIQUE (id, workspace_id);
ALTER TABLE external_cohort ADD CONSTRAINT external_cohort_workspace_identity_unique UNIQUE (id, workspace_id);
ALTER TABLE external_subject ADD CONSTRAINT external_subject_workspace_identity_unique UNIQUE (id, workspace_id);
ALTER TABLE external_cohort_subject ADD CONSTRAINT external_cohort_subject_workspace_identity_unique UNIQUE (id, workspace_id);
ALTER TABLE external_teaching_assignment ADD CONSTRAINT external_teaching_assignment_workspace_identity_unique UNIQUE (id, workspace_id);

CREATE TABLE IF NOT EXISTS external_learner (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    scholaroscope_learner_ref text NOT NULL,
    status text NOT NULL DEFAULT 'ACTIVE',
    source_version text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_learner_unique_ref UNIQUE (workspace_id, scholaroscope_learner_ref),
    CONSTRAINT external_learner_status_check CHECK (status IN ('ACTIVE', 'DISABLED', 'REMOVED'))
);

ALTER TABLE external_learner ADD CONSTRAINT external_learner_workspace_identity_unique UNIQUE (id, workspace_id);

CREATE TABLE IF NOT EXISTS external_learner_cohort_membership (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    learner_uuid uuid NOT NULL,
    cohort_uuid uuid NOT NULL,
    scholaroscope_membership_ref text NOT NULL,
    starts_on date,
    ends_on date,
    status text NOT NULL DEFAULT 'ACTIVE',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_learner_membership_unique_ref UNIQUE (workspace_id, scholaroscope_membership_ref),
    CONSTRAINT external_learner_membership_status_check CHECK (status IN ('ACTIVE', 'DISABLED', 'REMOVED')),
    CONSTRAINT external_learner_membership_learner_fk FOREIGN KEY (learner_uuid, workspace_id) REFERENCES external_learner(id, workspace_id) ON DELETE RESTRICT,
    CONSTRAINT external_learner_membership_cohort_fk FOREIGN KEY (cohort_uuid, workspace_id) REFERENCES external_cohort(id, workspace_id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS external_learner_subject_enrollment (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    learner_uuid uuid NOT NULL,
    cohort_uuid uuid NOT NULL,
    cohort_subject_uuid uuid NOT NULL,
    subject_uuid uuid NOT NULL,
    scholaroscope_enrollment_ref text NOT NULL,
    starts_on date,
    ends_on date,
    status text NOT NULL DEFAULT 'ACTIVE',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_learner_subject_unique_ref UNIQUE (workspace_id, scholaroscope_enrollment_ref),
    CONSTRAINT external_learner_subject_status_check CHECK (status IN ('ACTIVE', 'DISABLED', 'REMOVED')),
    CONSTRAINT external_learner_subject_learner_fk FOREIGN KEY (learner_uuid, workspace_id) REFERENCES external_learner(id, workspace_id) ON DELETE RESTRICT,
    CONSTRAINT external_learner_subject_cohort_fk FOREIGN KEY (cohort_uuid, workspace_id) REFERENCES external_cohort(id, workspace_id) ON DELETE RESTRICT,
    CONSTRAINT external_learner_subject_cohort_subject_fk FOREIGN KEY (cohort_subject_uuid, workspace_id) REFERENCES external_cohort_subject(id, workspace_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS external_learner_period_conflict_idx
    ON external_learner_subject_enrollment(workspace_id, learner_uuid, cohort_uuid, cohort_subject_uuid, status);

CREATE TABLE IF NOT EXISTS timetable_delivery_group (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    academic_year_uuid uuid NOT NULL REFERENCES external_academic_year(id) ON DELETE RESTRICT,
    academic_term_uuid uuid NOT NULL REFERENCES external_academic_term(id) ON DELETE RESTRICT,
    name text NOT NULL,
    teacher_uuid uuid NOT NULL,
    subject_uuid uuid NOT NULL,
    weekly_lesson_requirement integer NOT NULL,
    required_double_lessons integer NOT NULL DEFAULT 0,
    lifecycle_status text NOT NULL DEFAULT 'ACTIVE',
    source_snapshot_hash text NOT NULL DEFAULT '',
    source_version text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT timetable_delivery_group_demand_check CHECK (weekly_lesson_requirement > 0),
    CONSTRAINT timetable_delivery_group_double_check CHECK (required_double_lessons >= 0 AND required_double_lessons * 2 <= weekly_lesson_requirement),
    CONSTRAINT timetable_delivery_group_status_check CHECK (lifecycle_status IN ('DRAFT', 'ACTIVE', 'DISABLED', 'ARCHIVED')),
    CONSTRAINT timetable_delivery_group_teacher_fk FOREIGN KEY (teacher_uuid, workspace_id) REFERENCES external_actor(id, workspace_id) ON DELETE RESTRICT,
    CONSTRAINT timetable_delivery_group_subject_fk FOREIGN KEY (subject_uuid, workspace_id) REFERENCES external_subject(id, workspace_id) ON DELETE RESTRICT
);

ALTER TABLE timetable_delivery_group ADD CONSTRAINT timetable_delivery_group_workspace_identity_unique UNIQUE (id, workspace_id);

CREATE TABLE IF NOT EXISTS timetable_delivery_group_assignment (
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    delivery_group_id uuid NOT NULL,
    teaching_assignment_uuid uuid NOT NULL,
    PRIMARY KEY (workspace_id, delivery_group_id, teaching_assignment_uuid),
    CONSTRAINT timetable_delivery_group_assignment_group_fk FOREIGN KEY (delivery_group_id, workspace_id) REFERENCES timetable_delivery_group(id, workspace_id) ON DELETE CASCADE,
    CONSTRAINT timetable_delivery_group_assignment_assignment_fk FOREIGN KEY (teaching_assignment_uuid, workspace_id) REFERENCES external_teaching_assignment(id, workspace_id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS timetable_delivery_group_learner (
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    delivery_group_id uuid NOT NULL,
    learner_uuid uuid NOT NULL,
    cohort_uuid uuid NOT NULL,
    PRIMARY KEY (workspace_id, delivery_group_id, learner_uuid),
    CONSTRAINT timetable_delivery_group_learner_group_fk FOREIGN KEY (delivery_group_id, workspace_id) REFERENCES timetable_delivery_group(id, workspace_id) ON DELETE CASCADE,
    CONSTRAINT timetable_delivery_group_learner_learner_fk FOREIGN KEY (learner_uuid, workspace_id) REFERENCES external_learner(id, workspace_id) ON DELETE RESTRICT,
    CONSTRAINT timetable_delivery_group_learner_cohort_fk FOREIGN KEY (cohort_uuid, workspace_id) REFERENCES external_cohort(id, workspace_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS timetable_delivery_group_learner_conflict_idx
    ON timetable_delivery_group_learner(workspace_id, learner_uuid, delivery_group_id);

CREATE TABLE IF NOT EXISTS timetable_parallel_block (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    academic_year_uuid uuid NOT NULL REFERENCES external_academic_year(id) ON DELETE RESTRICT,
    academic_term_uuid uuid NOT NULL REFERENCES external_academic_term(id) ON DELETE RESTRICT,
    name text NOT NULL,
    lifecycle_status text NOT NULL DEFAULT 'ACTIVE',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT timetable_parallel_block_status_check CHECK (lifecycle_status IN ('DRAFT', 'ACTIVE', 'DISABLED', 'ARCHIVED'))
);

ALTER TABLE timetable_parallel_block ADD CONSTRAINT timetable_parallel_block_workspace_identity_unique UNIQUE (id, workspace_id);

CREATE TABLE IF NOT EXISTS timetable_parallel_block_member (
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    parallel_block_id uuid NOT NULL,
    delivery_group_id uuid NOT NULL,
    PRIMARY KEY (workspace_id, parallel_block_id, delivery_group_id),
    CONSTRAINT timetable_parallel_block_member_block_fk FOREIGN KEY (parallel_block_id, workspace_id) REFERENCES timetable_parallel_block(id, workspace_id) ON DELETE CASCADE,
    CONSTRAINT timetable_parallel_block_member_group_fk FOREIGN KEY (delivery_group_id, workspace_id) REFERENCES timetable_delivery_group(id, workspace_id) ON DELETE CASCADE
);

ALTER TABLE timetable_entry
    ADD COLUMN IF NOT EXISTS delivery_group_uuid uuid,
    ADD COLUMN IF NOT EXISTS parallel_block_uuid uuid,
    ADD COLUMN IF NOT EXISTS learner_count integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cohort_uuids uuid[] NOT NULL DEFAULT '{}'::uuid[],
    ADD COLUMN IF NOT EXISTS cohort_subject_uuids uuid[] NOT NULL DEFAULT '{}'::uuid[],
    ADD COLUMN IF NOT EXISTS teaching_assignment_uuids uuid[] NOT NULL DEFAULT '{}'::uuid[];

CREATE TABLE IF NOT EXISTS timetable_entry_learner_occupancy (
    timetable_entry_id uuid NOT NULL REFERENCES timetable_entry(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    timetable_version_id uuid NOT NULL REFERENCES timetable_version(id) ON DELETE CASCADE,
    day_of_week smallint NOT NULL,
    period_index smallint NOT NULL,
    learner_uuid uuid NOT NULL,
    PRIMARY KEY (timetable_entry_id, period_index, learner_uuid),
    CONSTRAINT timetable_entry_learner_workspace_fk FOREIGN KEY (learner_uuid, workspace_id) REFERENCES external_learner(id, workspace_id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX IF NOT EXISTS timetable_entry_learner_no_overlap_idx
    ON timetable_entry_learner_occupancy(workspace_id, timetable_version_id, day_of_week, period_index, learner_uuid);

COMMIT;
