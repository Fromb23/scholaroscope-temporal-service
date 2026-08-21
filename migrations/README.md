# Temporal service migrations

Migration files are ordered SQL artifacts. Apply them to PostgreSQL before starting the service.

Current local command pattern:

```bash
psql "$TEMPORAL_DATABASE_URL" -f migrations/000001_initial_external_timetable_schema.up.sql
psql "$TEMPORAL_DATABASE_URL" -f migrations/000002_workspace_reference_uniqueness.up.sql
psql "$TEMPORAL_DATABASE_URL" -f migrations/000006_academic_years_actor_roles.up.sql
psql "$TEMPORAL_DATABASE_URL" -f migrations/000007_solver_projection_integrity.up.sql
```

Rollback for local development:

```bash
psql "$TEMPORAL_DATABASE_URL" -f migrations/000001_initial_external_timetable_schema.down.sql
```

Production migration execution should be run by deployment orchestration with backups and health checks. Do not start the service against an undocumented pre-created schema.

Apply every ordered migration. Do not stop after `000001`; `000002` adds the
unique workspace reference target required by idempotent bootstrap
`ON CONFLICT (scholaroscope_workspace_ref, scholaroscope_organization_ref)`.

Production deployment should:

1. back up the temporal PostgreSQL database;
2. apply ordered `*.up.sql` files exactly once through deployment orchestration;
3. run `GET /health/ready`;
4. deploy the Go API only after readiness succeeds;
5. deploy the portal after the API URL and cookie settings are correct.

`000006` and later integrity migrations are explicitly transactional. `000007`
adds source-aware assignment requirements, solver diagnostics, and trigger-backed
teacher/cohort/room/resource occupancy. Apply it before deploying an API binary
that accepts generation requests.

Rollback is limited to explicit `*.down.sql` files and should not be used after
publication data exists without a data-retention plan.
