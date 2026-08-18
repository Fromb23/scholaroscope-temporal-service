# Temporal Integration Contracts

Status: Stage 0 contract definition. Implementation is added in later staged commits.

## Webhook envelope

```json
{
  "event_id": "uuid",
  "event_type": "temporal.timetable.learning.published.v1",
  "schema_version": "1.0",
  "occurred_at": "2026-08-18T10:00:00Z",
  "workspace_uuid": "uuid",
  "plugin_installation_ref": "uuid-or-stable-ref",
  "aggregate_type": "TIMETABLE_VERSION",
  "aggregate_uuid": "uuid",
  "aggregate_version": 1,
  "correlation_id": "uuid",
  "idempotency_key": "stable-string",
  "payload": {}
}
```

Unknown major schema versions fail safely. Duplicate `event_id` or `idempotency_key` is idempotent. Out-of-order aggregate versions are rejected or marked for reconciliation.

## Signing

Each installation has a separate secret. The sender signs timestamp and raw request body. The receiver validates signature and timestamp before JSON payload processing. Secrets and complete authorization tokens are never logged.

## Scholaroscope to temporal events

- `scholaroscope.timetable.workspace.bootstrap_requested.v1`
- `scholaroscope.timetable.workspace.disabled.v1`
- `scholaroscope.timetable.authorization.revoked.v1`
- `scholaroscope.academic.teacher.upserted.v1`
- `scholaroscope.academic.term.upserted.v1`
- `scholaroscope.academic.cohort.upserted.v1`
- `scholaroscope.academic.subject.upserted.v1`
- `scholaroscope.academic.cohort_subject.upserted.v1`
- `scholaroscope.academic.teaching_assignment.upserted.v1`
- `scholaroscope.academic.room.upserted.v1`
- `scholaroscope.academic.resource.upserted.v1`

## Temporal to Scholaroscope events

- `temporal.timetable.workspace.provisioned.v1`
- `temporal.timetable.sync.reconciliation_required.v1`
- `temporal.timetable.learning.published.v1`
- `temporal.timetable.learning.amended.v1`
- `temporal.timetable.examination.published.v1`
- `temporal.timetable.examination.amended.v1`
- `temporal.timetable.integration.health_changed.v1`

## Publication diff classifications

- `ENTRY_ADDED`
- `ENTRY_REMOVED`
- `TIME_CHANGED`
- `DAY_CHANGED`
- `TEACHER_CHANGED`
- `COHORT_CHANGED`
- `SUBJECT_CHANGED`
- `ROOM_CHANGED`
- `DURATION_CHANGED`
- `EXAM_SITTING_ADDED`
- `EXAM_SITTING_REMOVED`
- `EXAM_TIME_CHANGED`
- `INVIGILATOR_CHANGED`

## Launch grant payload

The launch grant binds:

- actor external UUID;
- workspace external UUID;
- plugin installation reference;
- resolved permission snapshot;
- allowed purpose;
- issued at;
- expires at;
- nonce;
- correlation ID.

Temporal consumes the grant once and creates a portal session. Actor/workspace are read from the grant only, never from URL parameters.
