package scheduling

import (
	"reflect"
	"testing"
	"time"
)

func scaleProfiles() []SimulationProfile {
	return []SimulationProfile{
		{Name: "individual-32", Teachers: 1, Cohorts: 1, Subjects: 8, Days: 4, PeriodsPerDay: 8},
		{Name: "small-10", Teachers: 10, Cohorts: 9, Subjects: 16, Days: 4, PeriodsPerDay: 8},
		{Name: "medium-50", Teachers: 50, Cohorts: 47, Subjects: 50, Days: 4, PeriodsPerDay: 8},
		{Name: "large-200", Teachers: 200, Cohorts: 188, Subjects: 60, Days: 4, PeriodsPerDay: 8},
		{Name: "very-large-300", Teachers: 300, Cohorts: 282, Subjects: 60, Days: 4, PeriodsPerDay: 8},
	}
}

func TestGeneratedFixturesAreIndependentlyProvenFeasible(t *testing.T) {
	for _, profile := range append(scaleProfiles(), SimulationProfile{Name: "double-lessons", Teachers: 50, Cohorts: 45, Subjects: 50, Days: 5, PeriodsPerDay: 8, DoubleLessons: true}) {
		t.Run(profile.Name, func(t *testing.T) {
			problem, witness := GenerateFeasibleSimulation(profile)
			report := ValidateSchedule(problem, witness, EngineConfig{MaxConsecutive: 8})
			if !report.Valid {
				t.Fatalf("fixture witness violates hard invariants: conflicts=%d unscheduled=%d first=%+v", report.HardConflictCount, report.Unscheduled, firstFailure(report))
			}
		})
	}
}

func TestHybridSolverScaleProfiles(t *testing.T) {
	for _, profile := range scaleProfiles() {
		for _, seed := range []int64{7, 23, 101} {
			profile.Seed = seed
			t.Run(profile.Name+"-seed", func(t *testing.T) {
				problem, _ := GenerateFeasibleSimulation(profile)
				result := Solve(problem, EngineConfig{Seed: seed, TimeBudget: 30 * time.Second, IterationBudget: 5_000_000, Restarts: 4, MaxConsecutive: 8})
				if result.Status != StatusComplete && result.Status != StatusCompleteWithSoftViolations {
					t.Fatalf("status=%s unscheduled=%d conflicts=%d issues=%+v", result.Status, result.Validation.Unscheduled, result.Validation.HardConflictCount, result.Feasibility.Issues)
				}
				if result.Validation.HardConflictCount != 0 || result.Validation.Unscheduled != 0 {
					t.Fatalf("invalid result: %+v", result.Validation)
				}
			})
		}
	}
}

func TestDoubleLessonConstruction(t *testing.T) {
	profile := SimulationProfile{Name: "double-lessons", Teachers: 50, Cohorts: 45, Subjects: 50, Days: 5, PeriodsPerDay: 8, DoubleLessons: true}
	problem, _ := GenerateFeasibleSimulation(profile)
	result := Solve(problem, EngineConfig{Seed: 29, TimeBudget: 30 * time.Second, IterationBudget: 5_000_000, Restarts: 5, MaxConsecutive: 8})
	if !result.Validation.Valid {
		t.Fatalf("double solve failed status=%s unscheduled=%d failure=%+v", result.Status, result.Validation.Unscheduled, firstFailure(result.Validation))
	}
	for _, placement := range result.Placements {
		if !placement.Double || len(placement.PeriodIDs) != 2 {
			t.Fatalf("expected only contiguous double placements, got %+v", placement)
		}
	}
}

func TestMixedSingleAndDoubleLessonConstruction(t *testing.T) {
	profile := SimulationProfile{Name: "mixed-lessons", Teachers: 50, Cohorts: 45, Subjects: 50, Days: 5, PeriodsPerDay: 8, DoubleLessons: true}
	problem, witness := GenerateFeasibleSimulation(profile)
	singleAssignments := map[string]bool{}
	for index := range problem.Assignments {
		if index%2 == 0 {
			problem.Assignments[index].DoubleBlocks = 0
			singleAssignments[problem.Assignments[index].ID] = true
		}
	}
	mixedWitness := make([]EnginePlacement, 0, len(witness)*2)
	for _, placement := range witness {
		if !singleAssignments[placement.AssignmentID] {
			mixedWitness = append(mixedWitness, placement)
			continue
		}
		for _, periodID := range placement.PeriodIDs {
			copy := placement
			copy.PeriodIDs = []string{periodID}
			copy.Double = false
			mixedWitness = append(mixedWitness, copy)
		}
	}
	if proof := ValidateSchedule(problem, mixedWitness, EngineConfig{MaxConsecutive: 8}); !proof.Valid {
		t.Fatalf("mixed fixture invalid: %+v", firstFailure(proof))
	}
	result := Solve(problem, EngineConfig{Seed: 31, TimeBudget: 30 * time.Second, IterationBudget: 5_000_000, Restarts: 12, MaxConsecutive: 8})
	if !result.Validation.Valid {
		t.Fatalf("mixed solve failed status=%s unscheduled=%d first=%+v", result.Status, result.Validation.Unscheduled, firstFailure(result.Validation))
	}
}

func TestAvailabilityAndScarceResourceScenario(t *testing.T) {
	profile := SimulationProfile{Name: "availability-resource", Teachers: 50, Cohorts: 45, Subjects: 50, Days: 4, PeriodsPerDay: 8}
	problem, _ := GenerateFeasibleSimulation(profile)
	baseline := Solve(problem, EngineConfig{Seed: 41, TimeBudget: 30 * time.Second, IterationBudget: 5_000_000, Restarts: 1, MaxConsecutive: 8})
	if !baseline.Validation.Valid {
		t.Fatal("could not construct baseline availability witness")
	}
	occupied := map[string]map[string]bool{}
	for _, placement := range baseline.Placements {
		if occupied[placement.TeacherID] == nil {
			occupied[placement.TeacherID] = map[string]bool{}
		}
		for _, periodID := range placement.PeriodIDs {
			occupied[placement.TeacherID][periodID] = true
		}
	}
	for teacherID, teacher := range problem.Teachers {
		for index, period := range problem.Periods {
			if index%11 == 0 && !occupied[teacherID][period.ID] {
				teacher.Unavailable[period.ID] = true
			}
		}
		problem.Teachers[teacherID] = teacher
	}
	resourceID := "laboratory-1"
	problem.Resources[resourceID] = EngineResource{ID: resourceID, WorkspaceID: problem.WorkspaceID, Capacity: 1, Unavailable: map[string]bool{}}
	for index := range problem.Assignments {
		if problem.Assignments[index].CohortID == "cohort-0000" {
			problem.Assignments[index].ResourceID = resourceID
		}
	}
	for index := range baseline.Placements {
		if baseline.Placements[index].CohortID == "cohort-0000" {
			baseline.Placements[index].ResourceID = resourceID
		}
	}
	if proof := ValidateSchedule(problem, baseline.Placements, EngineConfig{MaxConsecutive: 8}); !proof.Valid {
		t.Fatalf("resource fixture is not feasible: %+v", firstFailure(proof))
	}
	result := Solve(problem, EngineConfig{Seed: 41, TimeBudget: 30 * time.Second, IterationBudget: 5_000_000, Restarts: 12, MaxConsecutive: 8})
	if !result.Validation.Valid {
		t.Fatalf("availability/resource solve failed: status=%s unscheduled=%d first=%+v", result.Status, result.Validation.Unscheduled, firstFailure(result.Validation))
	}
}

func TestPreflightClassifiesSimultaneousTeacherShortage(t *testing.T) {
	problem, _ := GenerateFeasibleSimulation(SimulationProfile{Name: "infeasible", Teachers: 2, Cohorts: 2, Subjects: 4, Days: 1, PeriodsPerDay: 4})
	delete(problem.Teachers, "teacher-0001")
	result := Solve(problem, EngineConfig{Seed: 1, TimeBudget: time.Second})
	if result.Status != StatusInfeasible {
		t.Fatalf("got %s", result.Status)
	}
	if !hasIssue(result.Feasibility, "SIMULTANEOUS_TEACHER_SHORTAGE") || !hasIssue(result.Feasibility, "MISSING_ELIGIBLE_TEACHER") {
		t.Fatalf("expected precise simultaneous and assignment issues, got %+v", result.Feasibility.Issues)
	}
}

func TestValidatorRejectsTeacherCollisionAndPartialPublication(t *testing.T) {
	problem, witness := GenerateFeasibleSimulation(SimulationProfile{Name: "collision", Teachers: 2, Cohorts: 2, Subjects: 4, Days: 1, PeriodsPerDay: 4})
	partial := append([]EnginePlacement(nil), witness[:len(witness)-1]...)
	partial = append(partial, witness[0])
	report := ValidateSchedule(problem, partial, EngineConfig{})
	if report.Valid || report.HardConflictCount == 0 || report.Unscheduled == 0 {
		t.Fatalf("partial conflicting timetable was accepted: %+v", report)
	}
}

func TestDeterministicSeedProducesSameSchedule(t *testing.T) {
	problem, _ := GenerateFeasibleSimulation(SimulationProfile{Name: "deterministic", Teachers: 10, Cohorts: 9, Subjects: 16, Days: 4, PeriodsPerDay: 8})
	config := EngineConfig{Seed: 83, TimeBudget: 10 * time.Second, IterationBudget: 1_000_000, Restarts: 1, MaxConsecutive: 8}
	first := Solve(problem, config)
	second := Solve(problem, config)
	if !reflect.DeepEqual(first.Placements, second.Placements) {
		t.Fatal("same normalized problem and seed produced a different schedule")
	}
}

func TestIncrementalRepairPreservesUnaffectedSessions(t *testing.T) {
	problem, _ := GenerateFeasibleSimulation(SimulationProfile{Name: "incremental", Teachers: 10, Cohorts: 8, Subjects: 16, Days: 4, PeriodsPerDay: 8})
	base := Solve(problem, EngineConfig{Seed: 5, TimeBudget: 10 * time.Second, IterationBudget: 1_000_000, Restarts: 2, MaxConsecutive: 8})
	if !base.Validation.Valid {
		t.Fatalf("base solve failed: %+v", base.Validation)
	}
	problem.Existing = base.Placements
	// A newly declared unavailability invalidates one cell. The repair retains
	// every other independently valid existing placement before rematching.
	disrupted := base.Placements[0]
	teacher := problem.Teachers[disrupted.TeacherID]
	teacher.Unavailable[disrupted.PeriodIDs[0]] = true
	problem.Teachers[disrupted.TeacherID] = teacher
	repaired := Solve(problem, EngineConfig{Seed: 5, TimeBudget: 10 * time.Second, IterationBudget: 1_000_000, Restarts: 3, MaxConsecutive: 8, Incremental: true})
	if !repaired.Validation.Valid {
		t.Fatalf("repair failed: status=%s first=%+v", repaired.Status, firstFailure(repaired.Validation))
	}
	if repaired.Metrics.ExistingMoved > 4 {
		t.Fatalf("repair moved %d existing cells", repaired.Metrics.ExistingMoved)
	}
}

func BenchmarkHybridSolver(b *testing.B) {
	for _, profile := range scaleProfiles() {
		problem, _ := GenerateFeasibleSimulation(profile)
		b.Run(profile.Name, func(b *testing.B) {
			for iteration := 0; iteration < b.N; iteration++ {
				result := Solve(problem, EngineConfig{Seed: 23, TimeBudget: 30 * time.Second, IterationBudget: 5_000_000, Restarts: 2, MaxConsecutive: 8})
				if !result.Validation.Valid {
					b.Fatalf("invalid result: %s", result.Status)
				}
			}
		})
	}
}

func firstFailure(report ValidationReport) *InvariantResult {
	for index := range report.Results {
		if !report.Results[index].Passed {
			return &report.Results[index]
		}
	}
	return nil
}

func hasIssue(report FeasibilityReport, code string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
