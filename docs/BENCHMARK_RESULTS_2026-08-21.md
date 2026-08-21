# Measured scheduling results - 2026-08-21

Environment: Windows amd64, Intel Core i5-12400, Go 1.27.0. Durations below are measured wall-clock ranges across seeds 7, 23, and 101. Allocation observations are cumulative bytes allocated by the simulation run, not peak resident memory.

| Profile | Teachers | Cohorts | Subjects | Demand | Doubles | Preflight | Solve | Allocated | Hard | Unscheduled | Coverage |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Individual | 1 | 1 | 8 | 32 | 0 | <1 ms | 0.53-2.03 ms | 0.32-1.25 MB | 0 | 0 | 100% |
| Small | 10 | 9 | 16 | 288 | 0 | 0.57-1.13 ms | 7.55-11.26 ms | 12.12-12.13 MB | 0 | 0 | 100% |
| Medium | 50 | 47 | 50 | 1,504 | 0 | 10.42-13.97 ms | 40.05-49.47 ms | 64.45-64.51 MB | 0 | 0 | 100% |
| Large | 200 | 188 | 60 | 6,016 | 0 | 62.59-67.41 ms | 165.73-171.47 ms | 264.67-265.17 MB | 0 | 0 | 100% |
| Very large | 300 | 282 | 60 | 9,024 | 0 | 123.56-164.35 ms | 260.73-269.74 ms | 420.50-421.29 MB | 0 | 0 | 100% |
| Double lessons | 50 | 45 | 50 | 1,440 | 720 blocks | 4.46-5.01 ms | 21.05-22.72 ms | 31.55-31.56 MB | 0 | 0 | 100% |

Every feasible run finished as `COMPLETE` or `COMPLETE_WITH_SOFT_VIOLATIONS`; all had zero hard conflicts, zero unscheduled mandatory periods, valid assignment authority, 100% cohort coverage, and valid double adjacency. Soft counts represent measurable idle-gap/clustering preferences, not conflicts.

The separate `-benchmem -benchtime=1x` execution measured:

| Profile | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| 1 teacher | 565,300 | 322,960 | 795 |
| 10 teachers | 7,130,800 | 6,284,368 | 14,089 |
| 50 teachers | 60,871,600 | 34,514,488 | 77,190 |
| 200 teachers | 193,648,400 | 143,125,688 | 294,318 |
| 300 teachers | 314,270,600 | 231,207,120 | 438,198 |

The intentional infeasibility regression removes a required teacher and proves both `MISSING_ELIGIBLE_TEACHER` and `SIMULTANEOUS_TEACHER_SHORTAGE`; the terminal result is `INFEASIBLE`, never `COMPLETE`.
