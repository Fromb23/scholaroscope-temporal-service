CREATE TABLE IF NOT EXISTS external_calendar_event (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES external_workspace(id) ON DELETE RESTRICT,
    scholaroscope_event_ref text NOT NULL,
    term_uuid uuid,
    scholaroscope_term_ref text NOT NULL DEFAULT '',
    title text NOT NULL,
    event_kind text NOT NULL,
    starts_on date NOT NULL,
    ends_on date NOT NULL,
    affects_learning boolean NOT NULL DEFAULT true,
    source text NOT NULL DEFAULT 'SCHOLAROSCOPE_TERM_CALENDAR',
    status text NOT NULL DEFAULT 'ACTIVE',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT external_calendar_event_date_order_check CHECK (ends_on >= starts_on),
    CONSTRAINT external_calendar_event_status_check CHECK (status IN ('ACTIVE', 'REMOVED')),
    CONSTRAINT external_calendar_event_unique_ref UNIQUE (workspace_id, scholaroscope_event_ref)
);

CREATE INDEX IF NOT EXISTS external_calendar_event_term_idx
    ON external_calendar_event(workspace_id, term_uuid, starts_on, ends_on);
