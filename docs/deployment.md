# Temporal timetable plugin deployment

The temporal repository is the complete plugin package: Go API, Next.js portal,
PostgreSQL migrations, portal assets, outbox worker, and compose deployment
assets live here.

Do not commit real `.env` files or secrets.

## Services

- `postgres`: private PostgreSQL database, bound to localhost only for host
  administration.
- `migrations`: idempotently applies `migrations/*.up.sql` before API/worker
  startup.
- `api`: Go HTTP service exposing health, manifest, integration, launch, and
  `/api/v1` management routes.
- `outbox-worker`: delivers temporal-to-Scholaroscope outbox events with
  installation-scoped HMAC signatures and bounded retry.
- `portal`: independent Next.js management portal. It has no username/password
  login; access originates from Scholaroscope launch grants.

## Nginx routing boundary

Recommended public routing:

- Portal pages: `/`
- Go API:
  - `/plugin/manifest.json`
  - `/integration/`
  - `/portal/`
  - `/api/v1/`

Do not expose deprecated unsigned `/events/*` routes.

## Required environment

Copy `.env.example` to `.env` and set:

- `POSTGRES_PASSWORD`
- `TEMPORAL_SCHOLAROSCOPE_WEBHOOK_SECRET` — bootstrap/control-plane secret only.
  This must match Scholaroscope `TIMETABLE_PLUGIN_BOOTSTRAP_SECRET`.
- `SCHOLAROSCOPE_TIMETABLE_WEBHOOK_URL`, for production:
  `https://api.scholaroscope.com/api/plugins/timetable/webhooks/`
- `TEMPORAL_PORTAL_PUBLIC_URL`
- `TEMPORAL_API_PUBLIC_URL`
- `TEMPORAL_CORS_ALLOWED_ORIGINS`, including
  `https://scholaroscope.com`, `https://www.scholaroscope.com`, and the portal
  origin where same-origin portal API calls are routed through the Go service.

Production should keep `TEMPORAL_PORTAL_COOKIE_SECURE=true`.

Do not use wildcard CORS origins with credentials. The launch exchange is a
credentialed POST from Scholaroscope to `/portal/launch/exchange` and requires
`Content-Type`, `X-Scholaroscope-Timestamp`, and `X-Scholaroscope-Signature`.

The compose `migrations` service records applied `*.up.sql` files in
`schema_migrations` and skips previously applied versions. This is required for
existing production volumes because some historical DDL statements are not safe
to rerun.

## Installation-scoped runtime trust

Bootstrap events register each installation’s runtime signing key and key id.
Normal launch grants and integration events are verified against the stored
installation key. Different workspaces therefore do not share runtime signing
secrets.

## Examinations

Learning timetable management is implemented. Examination scheduling remains
gated and is not exposed as an operational workflow until the full examination
domain, publication, projections, and notifications are implemented.
