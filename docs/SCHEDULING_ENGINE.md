# Scholaroscope learning timetable engine

## Ownership and normalized model

Scholaroscope owns workspace membership, academic years, terms, calendar exceptions, cohorts, subjects, cohort-subject registrations, and active teaching assignments. Temporal owns bell-period versions, availability/resource constraints, drafts, solving, validation, publication, and projections. The generation API refuses to load assignments from another workspace or academic year and refuses to mutate published/superseded versions.

An assignment is the curriculum-independent authority edge `(workspace, academic year, term, teacher, cohort, canonical cohort subject, subject, optional resource)`. `weekly_periods` comes from Scholaroscope schemes when present or from an explicit Temporal override where the contract permits it. Rooms remain optional physical resources and are not classes.

A delivery group is the timetable-owned scheduling audience. It references one authorized teacher, one subject, one or more active Scholaroscope teaching assignments, one or more source cohorts, and opaque learner UUIDs derived from Scholaroscope learner subject participation. A merged delivery-group placement can satisfy several teaching assignments for the same period while consuming one teacher period and one optional resource allocation. Residual periods remain schedulable per assignment.

A parallel block contains two or more delivery groups that must run in the same period. Member groups may use different subjects and teachers, but learner audiences must be disjoint.

## Research and architecture decision

The design review covered:

- CP/CP-SAT: Google describes constraint programming as suitable for large scheduling search spaces. OR-Tools exposes C++, Python, Java, and C#, but no supported Go API. Adding it would require a native binding or separately deployed worker. Source: <https://developers.google.com/optimization/cp/>.
- School-specific CP plus local search: a published high-school model combines CP with operations-research bounds/local search and explicitly models daily availability, teacher limits, and full class coverage. Source: <https://www.sciencedirect.com/science/article/pii/S0305054802000837>.
- MIP/decomposition: practical high-school research uses decomposition and bipartite matching; another case found a comprehensive direct MIP untenable for realistic instances. Sources: <https://doi.org/10.1016/j.cor.2013.08.025> and <https://www.sciencedirect.com/science/article/pii/S0305054815000428>.
- Timefold Community supports school timetabling and metaheuristics under Apache-2.0, but requires a Java/Kotlin runtime; its published community scale statement is up to 5,000 assignments, below this service's 9,024-session acceptance profile. Sources: <https://timefold.ai/solver> and <https://licenses.timefold.ai/pricing>.
- UniTime/CPSolver supports course, exam, room, and instructor scheduling through Java local search. CPSolver is LGPL-3.0 and would add a JVM/solver-service boundary. Sources: <https://github.com/UniTime/unitime> and <https://central.sonatype.com/artifact/org.unitime/cpsolver>.
- FET was reviewed as an established school generator, but its AGPL licensing and external application model were not adopted.
- Examination invigilator literature confirms availability, workload bounds, preferences, and fairness are a distinct assignment problem. Sources: <https://doi.org/10.31127/tuje.467003> and <https://doi.org/10.1007/s10951-025-00868-7>.

No third-party solver code was copied and no new solver runtime was introduced. The selected pure-Go hybrid keeps the existing single-container deployment:

1. Capacity and compatibility preflight, including per-period bipartite coverage.
2. Exact bipartite multigraph edge coloring for the ordinary weekly core. The graph is padded to a balanced delta-regular multigraph and decomposed into perfect matchings. This guarantees teacher/cohort exclusivity for ordinary one-cohort demand within a bounded number of matching rounds.
3. Audience hyperedge scheduling for delivery groups and learner-aware fixtures. Each task is a bounded hyperedge over teacher, learner set or fallback cohort, covered assignment set, optional resource, and optional parallel block. Parallel-block members are placed atomically in the same candidate period. This avoids weakening validation to preserve the old bipartite assumption.
3. Paired super-period coloring for mixed/double demand, with break/day adjacency enforced independently.
4. Availability/resource filtering, repeated maximum matchings, and bounded augmenting repair.
5. Seeded restarts; retain the best feasible candidate by hard completeness then soft score.
6. Independent occupancy-based validation before a result can be published.

## Hard invariants

`validator.go` calculates teacher-period, learner-period, delivery-group-period, fallback cohort-period, resource-period, demand, workload, academic-scope, registration, assignment-authority, parallel-block, and double-adjacency state without using solver candidate logic. A violation is machine-readable with invariant, workspace, entity, period, observed/expected values, and explanation.

Publication requires the latest solver run to be `COMPLETE` or `COMPLETE_WITH_SOFT_VIOLATIONS`, with zero hard conflicts and zero unscheduled mandatory periods. `PARTIAL_DRAFT`, `TIME_BUDGET_EXCEEDED`, and `INFEASIBLE` are never publishable. Database-triggered occupancy tables independently protect teacher, fallback cohort, learner, room, and resource occupancy on generated entry write paths.

## Soft metrics

The validator reports teacher workload variance, daily load variance, repeated-subject clustering, teacher idle gaps, and consecutive overloads. These remain preferences unless a workspace policy promotes one to a hard constraint. The current constructive core retains the lowest-soft-violation result across configured restarts; a later local-search neighborhood can improve these scores without changing publication safety.

## Feasibility checks

Preflight reports exact required/available capacity and remediation for:

- missing academic scope or bell periods;
- missing/foreign teachers, cohorts, registrations, or qualifications;
- total and per-teacher capacity/workload;
- per-cohort capacity and full-coverage demand;
- teacher/cohort double adjacency;
- resource capacity;
- per-period distinct teacher count;
- per-period assignment-compatible maximum matching.

Preflight is a necessary-condition analyzer. A heuristic failure never changes a mathematically proven feasible fixture into `INFEASIBLE`; it returns a bounded partial/time-budget outcome instead.

## Reproducibility and commands

Ordinary suite:

```text
go test ./...
```

All deterministic simulations (including 200/300 teachers):

```text
go run ./cmd/simbench -profiles all -seeds 7,23,101
```

Go microbenchmarks:

```text
go test ./internal/scheduling -run '^$' -bench BenchmarkHybridSolver -benchtime=1x -benchmem
```

The fixture generator first creates a Latin-rotation feasibility witness, validates that witness independently, and then withholds it from `Solve`. Scale tests also cover mixed and all-double lessons, explicit lunch/break adjacency, availability, a scarce exclusive resource, infeasibility, deterministic replay, and incremental repair.

## Operational limits

- Default portal budget: 30 seconds, 5,000,000 iterations, 5 restarts; API caps are 120 seconds, 20,000,000 iterations, and 20 restarts.
- Solver runs are synchronous in the current Go API. The measured 9,024-period case is sub-second on the recorded workstation, so a worker boundary is not currently justified. If real tenant constraints cause requests to approach the 120-second cap, move the same engine contract to a durable worker without changing projection ownership.
- Calendar exceptions are authoritative dated projections. Weekly templates are bounded by timetable/term dates; dated rendering must continue to suppress affected occurrences.
- Combined and split lessons are represented through delivery groups and parallel blocks. Full coverage is learner-audience based when learner data is synchronized; absent learner data is a readiness blocker for full-coverage generation, not a guessed cohort-wide audience.
- Examination scheduling remains feature-gated and must not be inferred from the learning-timetable delivery-group model.
