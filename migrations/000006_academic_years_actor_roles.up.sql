CREATE TABLE IF NOT EXISTS external_academic_year (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    scholaroscope_academic_year_ref text NOT NULL,
    name text NOT NULL,
    start_date date NOT NULL,
    end_date date NOT NULL,
    is_current boolean NOT NULL DEFAULT false,
    status text NOT NULL DEFAULT 'ACTIVE',
    curriculum_ref text NOT NULL DEFAULT '',
    curriculum_name text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_academic_year_date_order_check CHECK (end_date >= start_date),
    CONSTRAINT external_academic_year_status_check CHECK (status IN ('CURRENT', 'ACTIVE', 'DISABLED', 'ARCHIVED')),
    CONSTRAINT external_academic_year_unique_ref UNIQUE (workspace_id, scholaroscope_academic_year_ref)
);

CREATE UNIQUE INDEX IF NOT EXISTS external_academic_year_one_current_idx
    ON external_academic_year(workspace_id, curriculum_ref)
    WHERE is_current = true AND curriculum_ref <> '';

ALTER TABLE external_academic_term
    ADD COLUMN IF NOT EXISTS academic_year_uuid uuid,
    ADD COLUMN IF NOT EXISTS scholaroscope_academic_year_ref text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS calendar_ready boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS is_frozen boolean NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS external_academic_term_year_idx
    ON external_academic_term(workspace_id, academic_year_uuid, start_date, end_date);

ALTER TABLE external_cohort
    ADD COLUMN IF NOT EXISTS academic_year_uuid uuid;

CREATE INDEX IF NOT EXISTS external_cohort_year_idx
    ON external_cohort(workspace_id, academic_year_uuid);

ALTER TABLE external_calendar_event
    ADD COLUMN IF NOT EXISTS academic_year_uuid uuid,
    ADD COLUMN IF NOT EXISTS scholaroscope_academic_year_ref text NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS external_calendar_event_year_idx
    ON external_calendar_event(workspace_id, academic_year_uuid, starts_on, ends_on);

CREATE TABLE IF NOT EXISTS external_actor_role (
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    actor_id uuid NOT NULL REFERENCES external_actor(id) ON DELETE CASCADE,
    actor_kind text NOT NULL,
    status text NOT NULL DEFAULT 'ACTIVE',
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, actor_id, actor_kind),
    CONSTRAINT external_actor_role_kind_check CHECK (actor_kind IN ('USER', 'TEACHER', 'MANAGER', 'SYSTEM')),
    CONSTRAINT external_actor_role_status_check CHECK (status IN ('ACTIVE', 'DISABLED'))
);

INSERT INTO external_actor_role (workspace_id, actor_id, actor_kind, status)
SELECT workspace_id, id, actor_kind, status
FROM external_actor
WHERE actor_kind IN ('USER', 'TEACHER', 'MANAGER', 'SYSTEM')
ON CONFLICT (workspace_id, actor_id, actor_kind)
DO UPDATE SET status = EXCLUDED.status, updated_at = now();

INSERT INTO external_actor_role (workspace_id, actor_id, actor_kind, status)
SELECT eta.workspace_id, eta.teacher_uuid, 'TEACHER', 'ACTIVE'
FROM external_teaching_assignment eta
WHERE eta.status = 'ACTIVE'
ON CONFLICT (workspace_id, actor_id, actor_kind)
DO UPDATE SET status = 'ACTIVE', updated_at = now();

CREATE INDEX IF NOT EXISTS external_actor_role_lookup_idx
    ON external_actor_role(workspace_id, actor_kind, status);
