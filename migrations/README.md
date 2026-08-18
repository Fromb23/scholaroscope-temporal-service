# Temporal service migrations

Migration files are ordered SQL artifacts. Apply them to PostgreSQL before starting the service.

Current local command pattern:

```bash
psql "$TEMPORAL_DATABASE_URL" -f migrations/000001_initial_external_timetable_schema.up.sql
```

Rollback for local development:

```bash
psql "$TEMPORAL_DATABASE_URL" -f migrations/000001_initial_external_timetable_schema.down.sql
```

Production migration execution should be run by deployment orchestration with backups and health checks. Do not start the service against an undocumented pre-created schema.
