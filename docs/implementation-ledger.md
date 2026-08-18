# External Timetable Plugin Cross-Repository Implementation Ledger

Branch in all repositories: `feat/external-timetable-plugin-v2`

This ledger records stage completion after each affected repository has committed the stage and focused verification has run.

| Stage | Scholaroscope backend | Scholaroscope frontend | Temporal service | Verification |
| --- | --- | --- | --- | --- |
| 0. Audit, ADR, contracts, and branch setup | `ae6cd1684bc8b9c6fb4bd2676ea7e129f750e4e1` | `c33d6401562c230ed801108212a8bd194efdb49f` | `0139fa8d251603fe2b0463ab60ca061a3c5eba65` | Frontend `npm run check:workspace` passed; frontend `npm run check:boundaries` passed; frontend `npm run check:plugin-boundaries` failed on pre-existing CBC route imports; backend `manage.py check` blocked because Django is not installed in the discovered Python; backend architecture/error-contract checks failed on pre-existing CBC/reporting plain `PermissionDenied` findings; temporal `go test ./...` blocked because Go is not installed on PATH or in inspected common locations. |
| 2. Temporal PostgreSQL migrations and foundational invariants | Not affected | Not affected | `d54f20274c51e6a331787d75a7bfb89465d70ad0` | Static quote-aware parenthesis check passed; `psql` and Docker are unavailable locally, so the migration was not executed against PostgreSQL; `go test ./...` remains blocked because Go is unavailable. |
