# Timetable workspace model

## Ownership boundary

Scholaroscope is authoritative for workspaces, academic years and terms, term
lifecycle, calendar exceptions, cohorts, subjects, cohort-subject registrations,
teacher identities, teaching assignments, and workspace permissions. The
temporal plugin stores workspace-scoped projections of those records only so it
can schedule safely. A complete, signed academic snapshot is applied
idempotently on installation and reconciliation; missing source records are
deactivated in the projection.

The temporal plugin owns bell periods, optional physical spaces and resources,
lesson requirements, generation jobs, draft versions, manual adjustments,
conflicts, validation, publication, and published projections.

## Classes and physical spaces

`external_cohort` is the canonical projected class. It carries the synchronized
human-readable name, lifecycle, academic-year binding, and enrolment count.
Managers never recreate a class as a room.

`room` represents an optional physical scheduling resource. A room can be
general, shared, or specialized and can be exclusive when requested by a
lesson. A class can reference one optional default room through a composite
room/workspace foreign key. Class occupancy and room occupancy remain separate
constraints, and cohort enrolment is never treated as room capacity.

The **Classes & spaces** UI therefore lists synchronized classes first and
optional rooms separately. Deleting a room is permitted only when no timetable
history uses it; otherwise it can be deactivated. Cross-workspace associations
are rejected by both API queries and database constraints.

## Academic context and exceptions

The workspace API classifies terms as `ACTIVE`, `UPCOMING`, `ENDED`, or
`UNAVAILABLE`. The active eligible term is the default; an explicit historical
term can be selected for read-only calendar inspection. Sequence remains an
internal Scholaroscope field and is not returned to the portal.

An exception is selected only when workspace, academic year, and term all match
the timetable and its inclusive date range intersects the timetable version's
effective range. The portal list and solver call the same selector. Removed
events remain projected for audit but are excluded from scheduling.

## Resumable workflow

`GET /api/v1/workflow` derives state from workspace-scoped academic projections,
calendar configuration, generation jobs, and the most recently updated version
for the selected term and category. It does not depend on browser storage.
Possible states are:

- `ACADEMIC_CONTEXT_REQUIRED`
- `INTEGRATION_DEGRADED`
- `ASSIGNMENTS_REQUIRED`
- `BELL_PERIODS_REQUIRED`
- `REQUIREMENTS_REQUIRED`
- `READY_TO_GENERATE`
- `GENERATING`
- `DRAFT_IN_PROGRESS`
- `DRAFT_HAS_CONFLICTS`
- `DRAFT_READY_FOR_VALIDATION`
- `READY_TO_PUBLISH`
- `PUBLISHED`

The response contains a readable explanation, blockers, primary and secondary
actions, progress counts, active term, relevant timetable/version, integration
status, and last update time. Every persistence query includes the portal
session workspace, term, category, and version identity. A partial unique index
allows only one running generation job per workspace/version.

Synchronization readiness separately reports pending, failed, no source
assignments in Scholaroscope, and source assignments that synchronized but did
not meet active teacher/curriculum eligibility. Source and eligible counts are
recorded with the snapshot so those conditions are not inferred from transport
status.

## Academic snapshot safety

Each successful full reconciliation records a SHA-256 hash of its academic
snapshot. Generation binds that hash to the draft. Validation and publication
compare the draft hash with the workspace hash, so assignment, cohort, subject,
term, or calendar changes require regeneration. Pre-upgrade drafts have no hash
and are intentionally treated as outdated. Published versions remain immutable.

## Validation and publication

Generation creates a persisted solver run and leaves the version as a draft.
Validation rebuilds conflicts, requires a current solver run and academic
snapshot, and persists `VALIDATED` only when there are no hard conflicts or
unscheduled mandatory lessons. Manual entry changes clear validation.
Publication additionally checks term eligibility, solver completion, persisted
validation, and rebuilt conflicts before atomically superseding the previous
published version and writing signed projection/notification outbox events.

## Error contract

Known domain failures use a deterministic response:

```json
{
  "error": {
    "type": "business_rule",
    "code": "draft_validation_required",
    "message": "Validate the timetable and resolve any highlighted issues before publishing.",
    "details": {},
    "action": {"label": "Validate timetable", "target": "/timetable"}
  }
}
```

Raw database errors, UUIDs, solver codes, stack traces, and transport messages
are not returned. Unexpected failures use `timetable_update_failed` and the safe
message “Something went wrong while updating the timetable. Please try again.”
Technical details remain in service logs.

## Product and permission boundary

Managers enter through a short-lived, signed Scholaroscope launch grant. The
portal has no independent login. Session cookies survive refresh and are bound
to actor, workspace, installation, permission snapshot, expiry, and revocation.
Management routes require `timetable.manage`; publication additionally requires
`timetable.publish`. Learning timetable management is operational. Examination
scheduling remains feature-gated until its full validation, invigilation,
publication, projection, notification, and permission workflow is implemented.
