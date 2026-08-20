CREATE TABLE IF NOT EXISTS external_academic_term (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    scholaroscope_term_ref text NOT NULL,
    name text NOT NULL,
    academic_year_label text NOT NULL DEFAULT '',
    start_date date NOT NULL,
    end_date date NOT NULL,
    status text NOT NULL DEFAULT 'OPEN',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_academic_term_unique_ref UNIQUE (workspace_id, scholaroscope_term_ref)
);

CREATE TABLE IF NOT EXISTS external_cohort (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    scholaroscope_cohort_ref text NOT NULL,
    name text NOT NULL,
    level text NOT NULL DEFAULT '',
    stream text NOT NULL DEFAULT '',
    academic_year_ref text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'ACTIVE',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_cohort_unique_ref UNIQUE (workspace_id, scholaroscope_cohort_ref)
);

CREATE TABLE IF NOT EXISTS external_subject (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    scholaroscope_subject_ref text NOT NULL,
    name text NOT NULL,
    code text NOT NULL DEFAULT '',
    level text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'ACTIVE',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_subject_unique_ref UNIQUE (workspace_id, scholaroscope_subject_ref)
);

CREATE TABLE IF NOT EXISTS external_cohort_subject (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    scholaroscope_cohort_subject_ref text NOT NULL,
    cohort_uuid uuid NOT NULL,
    subject_uuid uuid NOT NULL,
    label text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'ACTIVE',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_cohort_subject_unique_ref UNIQUE (workspace_id, scholaroscope_cohort_subject_ref)
);

CREATE TABLE IF NOT EXISTS external_teaching_assignment (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    scholaroscope_teaching_assignment_ref text NOT NULL,
    teacher_uuid uuid NOT NULL REFERENCES external_actor(id) ON DELETE RESTRICT,
    cohort_subject_uuid uuid NOT NULL,
    cohort_uuid uuid NOT NULL,
    subject_uuid uuid NOT NULL,
    teacher_ref text NOT NULL DEFAULT '',
    cohort_subject_ref text NOT NULL DEFAULT '',
    cohort_ref text NOT NULL DEFAULT '',
    subject_ref text NOT NULL DEFAULT '',
    subject_name text NOT NULL DEFAULT '',
    cohort_name text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'ACTIVE',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_teaching_assignment_unique_ref UNIQUE (workspace_id, scholaroscope_teaching_assignment_ref)
);

CREATE INDEX IF NOT EXISTS external_teaching_assignment_lookup_idx
    ON external_teaching_assignment(workspace_id, teacher_uuid, cohort_subject_uuid, status);
