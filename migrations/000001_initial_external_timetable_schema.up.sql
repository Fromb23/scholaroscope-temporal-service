CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE IF NOT EXISTS external_workspace (
    id uuid PRIMARY KEY,
    scholaroscope_workspace_ref text NOT NULL,
    scholaroscope_organization_ref text NOT NULL,
    display_name text NOT NULL,
    timezone text NOT NULL,
    status text NOT NULL DEFAULT 'PROVISIONING',
    provisioning_state text NOT NULL DEFAULT 'PENDING',
    last_successful_sync_at timestamptz,
    last_event_sequence bigint NOT NULL DEFAULT 0,
    reconciliation_required boolean NOT NULL DEFAULT false,
    integration_health text NOT NULL DEFAULT 'UNKNOWN',
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_workspace_status_check CHECK (status IN ('PROVISIONING', 'ACTIVE', 'DISABLED', 'SUSPENDED')),
    CONSTRAINT external_workspace_provisioning_state_check CHECK (provisioning_state IN ('PENDING', 'READY', 'FAILED', 'RECONCILING'))
);

CREATE TABLE IF NOT EXISTS integration_installation (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    scholaroscope_installation_ref text NOT NULL,
    signing_key_id text NOT NULL,
    status text NOT NULL DEFAULT 'ACTIVE',
    enabled_at timestamptz,
    disabled_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT integration_installation_status_check CHECK (status IN ('ACTIVE', 'DISABLED', 'REVOKED', 'SUSPENDED')),
    CONSTRAINT integration_installation_unique_workspace UNIQUE (workspace_id),
    CONSTRAINT integration_installation_unique_ref UNIQUE (scholaroscope_installation_ref)
);

CREATE TABLE IF NOT EXISTS external_actor (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    scholaroscope_user_ref text NOT NULL,
    display_name text NOT NULL,
    actor_kind text NOT NULL DEFAULT 'USER',
    status text NOT NULL DEFAULT 'ACTIVE',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_actor_kind_check CHECK (actor_kind IN ('USER', 'TEACHER', 'MANAGER', 'SYSTEM')),
    CONSTRAINT external_actor_status_check CHECK (status IN ('ACTIVE', 'DISABLED')),
    CONSTRAINT external_actor_unique_ref UNIQUE (workspace_id, scholaroscope_user_ref)
);

CREATE TABLE IF NOT EXISTS processed_webhook_event (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    installation_id uuid NOT NULL REFERENCES integration_installation(id) ON DELETE RESTRICT,
    event_id uuid NOT NULL,
    event_type text NOT NULL,
    schema_version text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_uuid uuid NOT NULL,
    aggregate_version bigint,
    correlation_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    occurred_at timestamptz NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now(),
    payload_hash text NOT NULL,
    status text NOT NULL DEFAULT 'PROCESSED',
    last_error text,
    CONSTRAINT processed_webhook_event_status_check CHECK (status IN ('PROCESSED', 'IGNORED_DUPLICATE', 'REJECTED', 'RECONCILIATION_REQUIRED')),
    CONSTRAINT processed_webhook_event_unique_event UNIQUE (installation_id, event_id),
    CONSTRAINT processed_webhook_event_unique_idempotency UNIQUE (installation_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS outbox_event (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    installation_id uuid NOT NULL REFERENCES integration_installation(id) ON DELETE RESTRICT,
    event_type text NOT NULL,
    schema_version text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_uuid uuid NOT NULL,
    aggregate_version bigint,
    correlation_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'PENDING',
    attempts integer NOT NULL DEFAULT 0,
    next_retry_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz,
    dead_lettered_at timestamptz,
    CONSTRAINT outbox_event_status_check CHECK (status IN ('PENDING', 'DELIVERING', 'DELIVERED', 'DEAD_LETTER')),
    CONSTRAINT outbox_event_attempts_check CHECK (attempts >= 0),
    CONSTRAINT outbox_event_unique_idempotency UNIQUE (installation_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS portal_launch_grant (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    installation_id uuid NOT NULL REFERENCES integration_installation(id) ON DELETE RESTRICT,
    actor_id uuid NOT NULL REFERENCES external_actor(id) ON DELETE RESTRICT,
    purpose text NOT NULL,
    permission_snapshot jsonb NOT NULL,
    nonce text NOT NULL,
    correlation_id uuid NOT NULL,
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    payload_hash text NOT NULL,
    CONSTRAINT portal_launch_grant_expiry_check CHECK (expires_at > issued_at),
    CONSTRAINT portal_launch_grant_unique_nonce UNIQUE (installation_id, nonce)
);

CREATE TABLE IF NOT EXISTS portal_session (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    installation_id uuid NOT NULL REFERENCES integration_installation(id) ON DELETE RESTRICT,
    actor_id uuid NOT NULL REFERENCES external_actor(id) ON DELETE RESTRICT,
    launch_grant_id uuid NOT NULL REFERENCES portal_launch_grant(id) ON DELETE RESTRICT,
    permission_snapshot jsonb NOT NULL,
    purpose text NOT NULL,
    issued_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    last_seen_at timestamptz,
    CONSTRAINT portal_session_expiry_check CHECK (expires_at > issued_at),
    CONSTRAINT portal_session_unique_grant UNIQUE (launch_grant_id)
);

CREATE TABLE IF NOT EXISTS calendar (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    academic_term_uuid uuid,
    name text NOT NULL,
    timezone text NOT NULL,
    effective_start date NOT NULL,
    effective_end date NOT NULL,
    learning_days smallint[] NOT NULL,
    status text NOT NULL DEFAULT 'DRAFT',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT calendar_date_order_check CHECK (effective_end >= effective_start),
    CONSTRAINT calendar_status_check CHECK (status IN ('DRAFT', 'ACTIVE', 'ARCHIVED')),
    CONSTRAINT calendar_learning_days_check CHECK (cardinality(learning_days) > 0)
);

CREATE TABLE IF NOT EXISTS bell_schedule (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    calendar_id uuid NOT NULL REFERENCES calendar(id) ON DELETE RESTRICT,
    name text NOT NULL,
    effective_start date NOT NULL,
    effective_end date NOT NULL,
    timezone text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT bell_schedule_date_order_check CHECK (effective_end >= effective_start),
    CONSTRAINT bell_schedule_unique_name UNIQUE (workspace_id, calendar_id, name)
);

CREATE TABLE IF NOT EXISTS bell_period (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    bell_schedule_id uuid NOT NULL REFERENCES bell_schedule(id) ON DELETE CASCADE,
    day_of_week smallint NOT NULL,
    period_index smallint NOT NULL,
    label text NOT NULL,
    start_time time NOT NULL,
    end_time time NOT NULL,
    period_kind text NOT NULL DEFAULT 'TEACHING',
    double_period_capable boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT bell_period_day_check CHECK (day_of_week BETWEEN 0 AND 6),
    CONSTRAINT bell_period_time_order_check CHECK (end_time > start_time),
    CONSTRAINT bell_period_index_check CHECK (period_index >= 0),
    CONSTRAINT bell_period_kind_check CHECK (period_kind IN ('TEACHING', 'BREAK', 'NONTEACHING', 'ASSEMBLY', 'EXAM')),
    CONSTRAINT bell_period_unique_index UNIQUE (workspace_id, bell_schedule_id, day_of_week, period_index),
    CONSTRAINT bell_period_no_overlap EXCLUDE USING gist (
        workspace_id WITH =,
        bell_schedule_id WITH =,
        day_of_week WITH =,
        tsrange(
            date '2000-01-01' + start_time,
            date '2000-01-01' + end_time,
            '[)'
        ) WITH &&
    )
);

CREATE TABLE IF NOT EXISTS calendar_exception (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    calendar_id uuid NOT NULL REFERENCES calendar(id) ON DELETE CASCADE,
    exception_date date NOT NULL,
    exception_kind text NOT NULL,
    label text NOT NULL,
    blocks_learning boolean NOT NULL DEFAULT true,
    replacement_bell_schedule_id uuid REFERENCES bell_schedule(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT calendar_exception_kind_check CHECK (exception_kind IN ('PUBLIC_HOLIDAY', 'MIDTERM_BREAK', 'INSTITUTION_CLOSURE', 'SPECIAL_EVENT', 'EXAMINATION_PERIOD', 'SHORTENED_DAY', 'TEACHER_LEAVE')),
    CONSTRAINT calendar_exception_unique_date UNIQUE (workspace_id, calendar_id, exception_date, exception_kind)
);

-- Backward-compatible tables used by the current Go API. org_id is the temporal workspace UUID.
CREATE TABLE IF NOT EXISTS org_calendar_version (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    version_number smallint NOT NULL,
    learning_days jsonb NOT NULL,
    day_start_time time NOT NULL,
    day_end_time time NOT NULL,
    slot_duration_minutes smallint NOT NULL,
    break_structure jsonb NOT NULL DEFAULT '[]'::jsonb,
    is_active boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT org_calendar_version_number_check CHECK (version_number > 0),
    CONSTRAINT org_calendar_version_duration_check CHECK (slot_duration_minutes > 0),
    CONSTRAINT org_calendar_version_time_order_check CHECK (day_end_time > day_start_time),
    CONSTRAINT org_calendar_version_learning_days_array CHECK (jsonb_typeof(learning_days) = 'array'),
    CONSTRAINT org_calendar_version_break_array CHECK (jsonb_typeof(break_structure) = 'array'),
    CONSTRAINT org_calendar_version_unique_number UNIQUE (org_id, version_number)
);

CREATE UNIQUE INDEX IF NOT EXISTS org_calendar_version_single_active_idx
    ON org_calendar_version(org_id)
    WHERE is_active;

CREATE TABLE IF NOT EXISTS time_slot (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    calendar_version_id uuid NOT NULL REFERENCES org_calendar_version(id) ON DELETE CASCADE,
    day_of_week smallint NOT NULL,
    start_time time NOT NULL,
    end_time time NOT NULL,
    slot_index smallint NOT NULL,
    slot_type text NOT NULL,
    bell_period_id uuid REFERENCES bell_period(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT time_slot_day_check CHECK (day_of_week BETWEEN 0 AND 6),
    CONSTRAINT time_slot_index_check CHECK (slot_index >= 0),
    CONSTRAINT time_slot_time_order_check CHECK (end_time > start_time),
    CONSTRAINT time_slot_type_check CHECK (slot_type IN ('LESSON', 'BREAK', 'PREP', 'NONTEACHING', 'EXAM')),
    CONSTRAINT time_slot_unique_index UNIQUE (org_id, calendar_version_id, day_of_week, slot_index),
    CONSTRAINT time_slot_no_overlap EXCLUDE USING gist (
        org_id WITH =,
        calendar_version_id WITH =,
        day_of_week WITH =,
        tsrange(
            date '2000-01-01' + start_time,
            date '2000-01-01' + end_time,
            '[)'
        ) WITH &&
    )
);

CREATE TABLE IF NOT EXISTS room (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    external_ref text,
    name text NOT NULL,
    capacity integer,
    exclusive boolean NOT NULL DEFAULT true,
    status text NOT NULL DEFAULT 'ACTIVE',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT room_capacity_check CHECK (capacity IS NULL OR capacity > 0),
    CONSTRAINT room_status_check CHECK (status IN ('ACTIVE', 'DISABLED')),
    CONSTRAINT room_unique_name UNIQUE (workspace_id, name),
    CONSTRAINT room_unique_external_ref UNIQUE (workspace_id, external_ref)
);

CREATE TABLE IF NOT EXISTS resource (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    external_ref text,
    name text NOT NULL,
    resource_kind text NOT NULL DEFAULT 'EQUIPMENT',
    exclusive boolean NOT NULL DEFAULT true,
    status text NOT NULL DEFAULT 'ACTIVE',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT resource_status_check CHECK (status IN ('ACTIVE', 'DISABLED')),
    CONSTRAINT resource_unique_name UNIQUE (workspace_id, name),
    CONSTRAINT resource_unique_external_ref UNIQUE (workspace_id, external_ref)
);

CREATE TABLE IF NOT EXISTS teacher_availability_rule (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    teacher_uuid uuid NOT NULL,
    day_of_week smallint,
    start_time time,
    end_time time,
    starts_on date,
    ends_on date,
    availability_kind text NOT NULL,
    reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT teacher_availability_rule_day_check CHECK (day_of_week IS NULL OR day_of_week BETWEEN 0 AND 6),
    CONSTRAINT teacher_availability_rule_time_order_check CHECK ((start_time IS NULL AND end_time IS NULL) OR end_time > start_time),
    CONSTRAINT teacher_availability_rule_date_order_check CHECK (ends_on IS NULL OR starts_on IS NULL OR ends_on >= starts_on),
    CONSTRAINT teacher_availability_rule_kind_check CHECK (availability_kind IN ('AVAILABLE', 'UNAVAILABLE', 'PREFERRED'))
);

CREATE TABLE IF NOT EXISTS teacher_availability (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    teacher_id uuid NOT NULL,
    timeslot_id uuid NOT NULL REFERENCES time_slot(id) ON DELETE CASCADE,
    is_available boolean NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT teacher_availability_unique_slot UNIQUE (org_id, teacher_id, timeslot_id)
);

CREATE TABLE IF NOT EXISTS room_availability (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    room_id uuid NOT NULL REFERENCES room(id) ON DELETE CASCADE,
    timeslot_id uuid NOT NULL REFERENCES time_slot(id) ON DELETE CASCADE,
    is_available boolean NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT room_availability_unique_slot UNIQUE (workspace_id, room_id, timeslot_id)
);

CREATE TABLE IF NOT EXISTS resource_availability (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    resource_id uuid NOT NULL REFERENCES resource(id) ON DELETE CASCADE,
    timeslot_id uuid NOT NULL REFERENCES time_slot(id) ON DELETE CASCADE,
    is_available boolean NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT resource_availability_unique_slot UNIQUE (workspace_id, resource_id, timeslot_id)
);

CREATE TABLE IF NOT EXISTS timetable (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    calendar_id uuid REFERENCES calendar(id) ON DELETE RESTRICT,
    academic_term_uuid uuid,
    timetable_type text NOT NULL DEFAULT 'LEARNING',
    scope_kind text NOT NULL DEFAULT 'WORKSPACE',
    scope_uuid uuid,
    name text NOT NULL,
    effective_start date NOT NULL,
    effective_end date NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT timetable_type_check CHECK (timetable_type IN ('LEARNING', 'EXAMINATION')),
    CONSTRAINT timetable_scope_kind_check CHECK (scope_kind IN ('WORKSPACE', 'COHORT', 'COHORT_SUBJECT', 'TEACHER')),
    CONSTRAINT timetable_date_order_check CHECK (effective_end >= effective_start)
);

CREATE TABLE IF NOT EXISTS timetable_version (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    timetable_id uuid NOT NULL REFERENCES timetable(id) ON DELETE RESTRICT,
    version_number integer NOT NULL,
    status text NOT NULL DEFAULT 'DRAFT',
    derived_from_version_id uuid REFERENCES timetable_version(id) ON DELETE RESTRICT,
    effective_start date NOT NULL,
    effective_end date NOT NULL,
    generator_version text,
    generator_config jsonb NOT NULL DEFAULT '{}'::jsonb,
    validation_summary jsonb NOT NULL DEFAULT '{}'::jsonb,
    publication_reason text,
    published_by_actor_id uuid REFERENCES external_actor(id) ON DELETE RESTRICT,
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT timetable_version_number_check CHECK (version_number > 0),
    CONSTRAINT timetable_version_status_check CHECK (status IN ('DRAFT', 'VALIDATING', 'VALIDATED', 'PUBLISHED', 'SUPERSEDED', 'ARCHIVED')),
    CONSTRAINT timetable_version_date_order_check CHECK (effective_end >= effective_start),
    CONSTRAINT timetable_version_unique_number UNIQUE (workspace_id, timetable_id, version_number)
);

ALTER TABLE timetable_version
    ADD CONSTRAINT timetable_version_no_published_overlap
    EXCLUDE USING gist (
        workspace_id WITH =,
        timetable_id WITH =,
        daterange(effective_start, effective_end, '[]') WITH &&
    )
    WHERE (status = 'PUBLISHED');

CREATE TABLE IF NOT EXISTS timetable_entry (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    timetable_version_id uuid NOT NULL REFERENCES timetable_version(id) ON DELETE CASCADE,
    stable_entry_uuid uuid NOT NULL,
    entry_kind text NOT NULL DEFAULT 'LEARNING',
    teacher_uuid uuid,
    cohort_uuid uuid,
    subject_uuid uuid,
    cohort_subject_uuid uuid,
    room_id uuid REFERENCES room(id) ON DELETE RESTRICT,
    resource_id uuid REFERENCES resource(id) ON DELETE RESTRICT,
    day_of_week smallint NOT NULL,
    start_period_index smallint NOT NULL,
    duration_periods smallint NOT NULL DEFAULT 1,
    start_time time NOT NULL,
    end_time time NOT NULL,
    is_pinned boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT timetable_entry_kind_check CHECK (entry_kind IN ('LEARNING', 'EXAMINATION_OVERRIDE')),
    CONSTRAINT timetable_entry_day_check CHECK (day_of_week BETWEEN 0 AND 6),
    CONSTRAINT timetable_entry_period_check CHECK (start_period_index >= 0 AND duration_periods > 0),
    CONSTRAINT timetable_entry_time_order_check CHECK (end_time > start_time),
    CONSTRAINT timetable_entry_unique_stable UNIQUE (workspace_id, timetable_version_id, stable_entry_uuid)
);

CREATE TABLE IF NOT EXISTS scheduled_session (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    session_id uuid NOT NULL,
    calendar_version_id uuid NOT NULL REFERENCES org_calendar_version(id) ON DELETE RESTRICT,
    timeslot_id uuid NOT NULL REFERENCES time_slot(id) ON DELETE RESTRICT,
    teacher_id uuid NOT NULL,
    cohort_subject_id uuid NOT NULL,
    duration_slots smallint NOT NULL,
    schedule_mode text NOT NULL,
    is_pinned boolean NOT NULL DEFAULT false,
    scheduled_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT scheduled_session_duration_check CHECK (duration_slots > 0),
    CONSTRAINT scheduled_session_mode_check CHECK (schedule_mode IN ('LEARNING', 'EXAM')),
    CONSTRAINT scheduled_session_unique_session UNIQUE (org_id, session_id)
);

CREATE TABLE IF NOT EXISTS slot_occupancy (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    calendar_version_id uuid NOT NULL REFERENCES org_calendar_version(id) ON DELETE RESTRICT,
    session_id uuid NOT NULL,
    day_of_week smallint NOT NULL,
    slot_index smallint NOT NULL,
    teacher_id uuid NOT NULL,
    cohort_subject_id uuid NOT NULL,
    cohort_id uuid,
    room_id uuid REFERENCES room(id) ON DELETE RESTRICT,
    resource_id uuid REFERENCES resource(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT slot_occupancy_day_check CHECK (day_of_week BETWEEN 0 AND 6),
    CONSTRAINT slot_occupancy_index_check CHECK (slot_index >= 0),
    CONSTRAINT slot_occupancy_unique_session_slot UNIQUE (org_id, calendar_version_id, session_id, day_of_week, slot_index)
);

CREATE UNIQUE INDEX IF NOT EXISTS slot_occupancy_teacher_unique_idx
    ON slot_occupancy(org_id, calendar_version_id, day_of_week, slot_index, teacher_id);

CREATE UNIQUE INDEX IF NOT EXISTS slot_occupancy_cohort_subject_unique_idx
    ON slot_occupancy(org_id, calendar_version_id, day_of_week, slot_index, cohort_subject_id);

CREATE UNIQUE INDEX IF NOT EXISTS slot_occupancy_cohort_unique_idx
    ON slot_occupancy(org_id, calendar_version_id, day_of_week, slot_index, cohort_id)
    WHERE cohort_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS slot_occupancy_room_unique_idx
    ON slot_occupancy(org_id, calendar_version_id, day_of_week, slot_index, room_id)
    WHERE room_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS slot_occupancy_resource_unique_idx
    ON slot_occupancy(org_id, calendar_version_id, day_of_week, slot_index, resource_id)
    WHERE resource_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS scheduling_conflict (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    calendar_version_id uuid REFERENCES org_calendar_version(id) ON DELETE RESTRICT,
    timetable_version_id uuid REFERENCES timetable_version(id) ON DELETE CASCADE,
    session_id uuid,
    timetable_entry_id uuid REFERENCES timetable_entry(id) ON DELETE CASCADE,
    constraint_code text,
    affected_teacher_uuid uuid,
    affected_cohort_uuid uuid,
    affected_room_id uuid REFERENCES room(id) ON DELETE RESTRICT,
    affected_resource_id uuid REFERENCES resource(id) ON DELETE RESTRICT,
    candidate_period jsonb,
    blocking_entry_uuid uuid,
    severity text NOT NULL DEFAULT 'HARD',
    conflict_type text NOT NULL,
    description text NOT NULL,
    recovery_actions jsonb NOT NULL DEFAULT '[]'::jsonb,
    resolved boolean NOT NULL DEFAULT false,
    detected_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    CONSTRAINT scheduling_conflict_severity_check CHECK (severity IN ('HARD', 'SOFT'))
);

CREATE UNIQUE INDEX IF NOT EXISTS scheduling_conflict_unique_open_idx
    ON scheduling_conflict(
        org_id,
        COALESCE(timetable_version_id, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(timetable_entry_id, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(constraint_code, ''),
        COALESCE(blocking_entry_uuid, '00000000-0000-0000-0000-000000000000'::uuid)
    )
    WHERE resolved = false;

CREATE TABLE IF NOT EXISTS publication_diff (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    timetable_version_id uuid NOT NULL REFERENCES timetable_version(id) ON DELETE CASCADE,
    change_type text NOT NULL,
    stable_entry_uuid uuid,
    before_state jsonb,
    after_state jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT publication_diff_type_check CHECK (change_type IN (
        'ENTRY_ADDED', 'ENTRY_REMOVED', 'TIME_CHANGED', 'DAY_CHANGED',
        'TEACHER_CHANGED', 'COHORT_CHANGED', 'SUBJECT_CHANGED',
        'ROOM_CHANGED', 'DURATION_CHANGED', 'EXAM_SITTING_ADDED',
        'EXAM_SITTING_REMOVED', 'EXAM_TIME_CHANGED', 'INVIGILATOR_CHANGED'
    ))
);

CREATE TABLE IF NOT EXISTS learning_occurrence_projection (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    timetable_version_id uuid NOT NULL REFERENCES timetable_version(id) ON DELETE CASCADE,
    stable_entry_uuid uuid NOT NULL,
    occurrence_identity text NOT NULL,
    occurrence_date date NOT NULL,
    start_time time NOT NULL,
    end_time time NOT NULL,
    teacher_uuid uuid,
    cohort_subject_uuid uuid,
    status text NOT NULL DEFAULT 'PROJECTED',
    last_synchronized_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT learning_occurrence_projection_time_order_check CHECK (end_time > start_time),
    CONSTRAINT learning_occurrence_projection_status_check CHECK (status IN ('PROJECTED', 'MATERIALIZED', 'CANCELLED', 'SUPERSEDED')),
    CONSTRAINT learning_occurrence_projection_unique_identity UNIQUE (workspace_id, occurrence_identity)
);

CREATE TABLE IF NOT EXISTS exam_timetable (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    academic_term_uuid uuid,
    name text NOT NULL,
    effective_start date NOT NULL,
    effective_end date NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_timetable_date_order_check CHECK (effective_end >= effective_start)
);

CREATE TABLE IF NOT EXISTS exam_timetable_version (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    exam_timetable_id uuid NOT NULL REFERENCES exam_timetable(id) ON DELETE RESTRICT,
    version_number integer NOT NULL,
    status text NOT NULL DEFAULT 'DRAFT',
    derived_from_version_id uuid REFERENCES exam_timetable_version(id) ON DELETE RESTRICT,
    validation_summary jsonb NOT NULL DEFAULT '{}'::jsonb,
    publication_reason text,
    published_by_actor_id uuid REFERENCES external_actor(id) ON DELETE RESTRICT,
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_timetable_version_number_check CHECK (version_number > 0),
    CONSTRAINT exam_timetable_version_status_check CHECK (status IN ('DRAFT', 'VALIDATING', 'VALIDATED', 'PUBLISHED', 'SUPERSEDED', 'ARCHIVED')),
    CONSTRAINT exam_timetable_version_unique_number UNIQUE (workspace_id, exam_timetable_id, version_number)
);

CREATE UNIQUE INDEX IF NOT EXISTS exam_timetable_version_single_published_idx
    ON exam_timetable_version(workspace_id, exam_timetable_id)
    WHERE status = 'PUBLISHED';

CREATE TABLE IF NOT EXISTS exam_sitting (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    exam_timetable_version_id uuid NOT NULL REFERENCES exam_timetable_version(id) ON DELETE CASCADE,
    assessment_uuid uuid NOT NULL,
    candidate_cohort_uuid uuid NOT NULL,
    sitting_date date NOT NULL,
    start_time time NOT NULL,
    end_time time NOT NULL,
    setup_minutes integer NOT NULL DEFAULT 0,
    transition_minutes integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_sitting_time_order_check CHECK (end_time > start_time),
    CONSTRAINT exam_sitting_windows_check CHECK (setup_minutes >= 0 AND transition_minutes >= 0),
    CONSTRAINT exam_sitting_unique_assessment UNIQUE (workspace_id, exam_timetable_version_id, assessment_uuid, candidate_cohort_uuid)
);

CREATE TABLE IF NOT EXISTS exam_room_assignment (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    exam_sitting_id uuid NOT NULL REFERENCES exam_sitting(id) ON DELETE CASCADE,
    room_id uuid NOT NULL REFERENCES room(id) ON DELETE RESTRICT,
    allocated_capacity integer,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exam_room_assignment_capacity_check CHECK (allocated_capacity IS NULL OR allocated_capacity > 0),
    CONSTRAINT exam_room_assignment_unique_room UNIQUE (workspace_id, exam_sitting_id, room_id)
);

CREATE TABLE IF NOT EXISTS invigilator_assignment (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    exam_sitting_id uuid NOT NULL REFERENCES exam_sitting(id) ON DELETE CASCADE,
    invigilator_actor_id uuid NOT NULL REFERENCES external_actor(id) ON DELETE RESTRICT,
    role text NOT NULL DEFAULT 'INVIGILATOR',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT invigilator_assignment_unique_actor UNIQUE (workspace_id, exam_sitting_id, invigilator_actor_id)
);

CREATE TABLE IF NOT EXISTS exam_conflict (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    exam_timetable_version_id uuid NOT NULL REFERENCES exam_timetable_version(id) ON DELETE CASCADE,
    exam_sitting_id uuid REFERENCES exam_sitting(id) ON DELETE CASCADE,
    constraint_code text NOT NULL,
    severity text NOT NULL DEFAULT 'HARD',
    description text NOT NULL,
    recovery_actions jsonb NOT NULL DEFAULT '[]'::jsonb,
    resolved boolean NOT NULL DEFAULT false,
    detected_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    CONSTRAINT exam_conflict_severity_check CHECK (severity IN ('HARD', 'SOFT'))
);

CREATE TABLE IF NOT EXISTS audit_event (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    actor_id uuid REFERENCES external_actor(id) ON DELETE SET NULL,
    operation text NOT NULL,
    target_type text NOT NULL,
    target_uuid uuid,
    outcome text NOT NULL,
    correlation_id uuid NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT audit_event_outcome_check CHECK (outcome IN ('SUCCESS', 'DENIED', 'FAILED'))
);

CREATE INDEX IF NOT EXISTS processed_webhook_event_workspace_idx ON processed_webhook_event(workspace_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS outbox_event_delivery_idx ON outbox_event(status, next_retry_at, created_at);
CREATE INDEX IF NOT EXISTS portal_session_active_idx ON portal_session(workspace_id, actor_id, expires_at) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS time_slot_version_idx ON time_slot(calendar_version_id, day_of_week, slot_index);
CREATE INDEX IF NOT EXISTS teacher_availability_teacher_idx ON teacher_availability(org_id, teacher_id);
CREATE INDEX IF NOT EXISTS timetable_entry_filter_idx ON timetable_entry(workspace_id, timetable_version_id, teacher_uuid, cohort_uuid, subject_uuid, cohort_subject_uuid, room_id, day_of_week);
CREATE INDEX IF NOT EXISTS scheduling_conflict_open_idx ON scheduling_conflict(org_id, calendar_version_id, resolved, severity);
CREATE INDEX IF NOT EXISTS publication_diff_version_idx ON publication_diff(workspace_id, timetable_version_id);
CREATE INDEX IF NOT EXISTS learning_occurrence_projection_date_idx ON learning_occurrence_projection(workspace_id, occurrence_date);
CREATE INDEX IF NOT EXISTS exam_sitting_time_idx ON exam_sitting(workspace_id, sitting_date, start_time, end_time);
CREATE INDEX IF NOT EXISTS audit_event_workspace_created_idx ON audit_event(workspace_id, created_at DESC);
