# Examination scheduling feature gate

Examination timetable creation and automatic generation remain explicitly gated. The portal communicates this state and the backend returns `examination_scheduling_feature_gated`; no placeholder can be published.

Implemented foundations that can be reused:

- timetable category and immutable version/publication schema;
- workspace, academic-year, term, bell-period, exception, cohort-subject, room/resource, staff identity, signing, outbox, and projection infrastructure;
- independent cohort/teacher/resource occupancy validation;
- published-version safety and idempotent event delivery.

Required before enabling examinations:

1. Authoritative exam demand with per-cohort-subject duration, sitting windows, grouping policy, and accommodations.
2. Explicit invigilator eligibility/policy distinct from subject-teacher assignment.
3. Multi-room capacity and linked-room session modeling where enabled.
4. A duration-aware exam construction/repair path and independent exam/invigilator invariants.
5. Fair invigilation workload objective and configured threshold.
6. Complete portal create/edit/conflict/publication workflow without raw identifiers.
7. Teacher/workspace projections and notification semantics for initial and changed publications.
8. Feasible and intentionally infeasible 10/50/200/300-staff benchmark fixtures with multiple seeds.
9. Authorization, replay/signature, retry, tenant-isolation, and migration regression coverage.

This ledger is intentionally a release blocker, not a roadmap claim.
