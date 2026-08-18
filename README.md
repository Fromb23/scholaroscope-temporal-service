# Scholaroscope Temporal Service

The temporal service is Scholaroscope's external timetable plugin. It is a Go backend with PostgreSQL persistence and UUID-owned timetable domain records. It integrates with the Scholaroscope Django kernel through signed, versioned launch, webhook, and event contracts. It must not read or write Scholaroscope's MySQL database.

Current implementation status: production-readiness continuation.

Implemented in this branch:

- `GET /plugin/manifest.json` remote plugin manifest for Scholaroscope installation;
- `GET /health/live` and `GET /health/ready`;
- portal sessions with a configurable lifetime independent of the single-use launch grant;
- session, workspace UUID, and permission middleware on temporal management endpoints;
- legacy `/events/*` mutation routes return `410 Gone`; producers must use `/integration/scholaroscope/events`;
- an isolated Next.js TypeScript portal in `portal/` that authenticates exclusively through `/portal/session` and calls real Go API routes.

Still incomplete:

- installation-scoped trust bootstrap and key rotation are not fully implemented; the existing launch/provisioning verifier still uses the configured backend secret;
- durable outbound delivery workers, publication events, notifications, Scholaroscope read models, session materialization, lesson-plan generation, and examination publication are not complete;
- PostgreSQL migrations could not be executed in the current local environment because `psql`/Docker are unavailable.

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
TEMPORAL_PORTAL_PUBLIC_URL=http://localhost:3000
TEMPORAL_PORTAL_SESSION_MINUTES=480
SCHOLAROSCOPE_TIMETABLE_WEBHOOK_URL=http://localhost:8000/api/plugins/timetable/webhooks/
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

Portal startup:

```bash
cd portal
npm install
npm run dev
```

Portal verification:

```bash
npm run lint
npm run typecheck
npm run build
```

## Remaining operational requirements

- repository-native automatic migration runner or documented deployment wrapper;
- signed webhook inbox/outbox with retry, dead-letter, replay, and correlation IDs;
- timetable lifecycle and publication;
- explicit bell-period calendar semantics;
- learning and examination scheduling constraints;
- structured errors, secure headers, request limits, panic recovery, metrics, graceful shutdown, backup, migration, and recovery documentation.

See `docs/adr_external_timetable_plugin.md` and `docs/integration-contracts.md`.
