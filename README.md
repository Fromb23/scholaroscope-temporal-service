# Scholaroscope Temporal Service

The temporal service is Scholaroscope's external timetable plugin. It is a Go backend with PostgreSQL persistence and UUID-owned timetable domain records. It integrates with the Scholaroscope Django kernel through signed, versioned launch, webhook, and event contracts. It must not read or write Scholaroscope's MySQL database.

Current implementation status: Stage 0 audit and contract definition are complete in this branch; broad feature implementation is staged after the ADR. The checked-out repository currently contains only the Go API. No Next.js management portal exists on the inspected local or remote branches, so a portal must be created in a clearly isolated repository-consistent directory in a later stage.

## Ownership boundary

Scholaroscope owns identity, workspaces, memberships, authorization, academic structure, teaching assignments, learner records, notifications, sessions, lesson plans, plugin installation, and plugin entitlement.

Temporal owns timetable calendars, bell periods, timetable versions, timetable entries, teacher availability projections, rooms/resources, scheduling constraints, conflict detection, timetable publication, examination sittings, and timetable-specific scheduling decisions.

## Runtime contract

Managers do not authenticate directly against temporal. Scholaroscope issues a short-lived, single-use launch grant after verifying the active workspace session, plugin entitlement/enablement, integration health, and exact `timetable.manage` permission. Temporal validates the grant and creates an HttpOnly portal session.

Temporal APIs must scope every query and mutation to the authenticated temporal workspace. URL workspace identifiers alone are never authority.

Webhook messages are signed with installation-scoped secrets and use the shared envelope documented in `docs/integration-contracts.md`.

## Local development

Required tools:

- Go matching `go.mod`
- PostgreSQL

Environment:

```bash
TEMPORAL_DATABASE_URL=postgres://temporal_user:root@localhost:5432/temporal_service?sslmode=disable
PORT=8081
```

If `TEMPORAL_DATABASE_URL` is unset, the service builds a local development DSN from `DB_USER`, `DB_PASSWORD`, `DB_HOST`, `DB_PORT`, and `DB_NAME`.

Current start command:

```bash
go run ./cmd/server
```

Current test command:

```bash
go test ./...
```

## Operational requirements to be implemented

- versioned PostgreSQL migrations;
- `/health/live` and `/health/ready`;
- signed launch grant consumption and portal sessions;
- signed webhook inbox/outbox with retry, dead-letter, replay, and correlation IDs;
- timetable lifecycle and publication;
- explicit bell-period calendar semantics;
- learning and examination scheduling constraints;
- management portal;
- structured errors, secure headers, request limits, panic recovery, metrics, graceful shutdown, backup, migration, and recovery documentation.

See `docs/adr_external_timetable_plugin.md` and `docs/integration-contracts.md`.
