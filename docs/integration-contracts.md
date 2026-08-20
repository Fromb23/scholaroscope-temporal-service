# Temporal Integration Contracts

Status: launch/session contract and workspace-implicit management API are
implemented for development.

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
## Remote manifest

Temporal exposes `GET /plugin/manifest.json` with:

- `plugin_key: timetable`;
- `requires: ["notifications"]`;
- timetable capability declarations;
- `config_schema` containing `temporal_api_base_url`, `temporal_launch_exchange_url`, `temporal_webhook_url`, and `signing_key_id`;
- protocol metadata for signed installation-aware integration envelopes;
- `contributes: {}` because no additional Scholaroscope contribution type is required for the current integration.

## Management route authentication

Temporal management routes require a valid `temporal_portal_session` cookie
created by `/portal/launch/exchange`. The middleware verifies session expiry and
revocation, active installation/workspace state, exact workspace UUID match
against `{orgId}`, and the required timetable permission. Legacy `/events/*`
routes are removed from the mutation path and return `410 Gone`.

## Workspace-implicit management API

The portal uses session-derived workspace identity for these development
management endpoints:

- `GET /api/v1/workspace`
- `GET /api/v1/calendar`
- `PUT /api/v1/calendar`
- `GET /api/v1/teachers`
- `GET /api/v1/availability`
- `GET /api/v1/teaching-demands`
- `GET /api/v1/rooms`
- `POST /api/v1/rooms`
- `GET /api/v1/timetables`
- `GET /api/v1/conflicts`
- `POST /api/v1/timetable-versions/{version_uuid}/validate`
- `POST /api/v1/timetable-versions/{version_uuid}/publish`

Legacy `/orgs/{orgId}/...` routes remain guarded by the portal session and must
match the authenticated workspace UUID before serving data or accepting
mutations.

## Portal routes

The independent portal frontend exposes:

- `/`
- `/calendar`
- `/availability`
- `/rooms`
- `/demand`
- `/timetables`
- `/timetables/new`
- `/conflicts`
- `/publication`

Direct unauthenticated access renders the session-expired state and instructs the
manager to relaunch from Scholaroscope. There is no standalone username/password
login.

## Development environment variables

- `TEMPORAL_PORTAL_PUBLIC_URL`
- `TEMPORAL_PORTAL_RETURN_URL`
- `TEMPORAL_PORTAL_COOKIE_SECURE`
- `TEMPORAL_CORS_ALLOWED_ORIGINS`

`TEMPORAL_PORTAL_COOKIE_SECURE` defaults to secure production behavior unless
the configured portal public URL is a local HTTP development URL. CORS supports
credentialed launch/session requests only for configured origins.
