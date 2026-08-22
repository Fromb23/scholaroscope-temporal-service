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

func TestCohortSpecificActivitiesDoNotGloballyBlockOtherClasses(t *testing.T) {
	problem := EngineProblem{
		WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term",
		Periods: []EnginePeriod{{ID: "p1", Day: 0, Index: 0, Teaching: true, Mandatory: true}},
		Teachers: map[string]EngineTeacher{
			"pe-teacher":   {ID: "pe-teacher", WorkspaceID: "workspace", Unavailable: map[string]bool{}},
			"math-teacher": {ID: "math-teacher", WorkspaceID: "workspace", Unavailable: map[string]bool{}},
		},
		Cohorts: map[string]EngineCohort{
			"green":  {ID: "green", WorkspaceID: "workspace", Unavailable: map[string]bool{}},
			"yellow": {ID: "yellow", WorkspaceID: "workspace", Unavailable: map[string]bool{}},
		},
		Registrations: map[string]map[string]bool{"green": {"green-pe": true}, "yellow": {"yellow-math": true}},
		Assignments: []EngineAssignment{
			{ID: "green-pe", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "pe-teacher", CohortID: "green", CohortSubjectID: "green-pe", SubjectID: "pe", WeeklyPeriods: 1, Mandatory: true, Active: true},
			{ID: "yellow-math", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "math-teacher", CohortID: "yellow", CohortSubjectID: "yellow-math", SubjectID: "math", WeeklyPeriods: 1, Mandatory: true, Active: true},
		},
	}
	report := ValidateSchedule(problem, []EnginePlacement{
		{AssignmentID: "green-pe", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "pe-teacher", CohortID: "green", CohortSubjectID: "green-pe", SubjectID: "pe", PeriodIDs: []string{"p1"}},
		{AssignmentID: "yellow-math", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "math-teacher", CohortID: "yellow", CohortSubjectID: "yellow-math", SubjectID: "math", PeriodIDs: []string{"p1"}},
	}, EngineConfig{})
	if !report.Valid {
		t.Fatalf("cohort-specific PE should not block another class: %+v", firstFailure(report))
	}
}

func audienceScenarioProblem() EngineProblem {
	periods := []EnginePeriod{
		{ID: "p1", Day: 0, Index: 0, Teaching: true, Mandatory: true},
		{ID: "p2", Day: 0, Index: 1, Teaching: true, Mandatory: true},
		{ID: "p3", Day: 1, Index: 0, Teaching: true, Mandatory: true},
		{ID: "p4", Day: 1, Index: 1, Teaching: true, Mandatory: true},
		{ID: "p5", Day: 2, Index: 0, Teaching: true, Mandatory: true},
		{ID: "p6", Day: 2, Index: 1, Teaching: true, Mandatory: true},
	}
	return EngineProblem{
		WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", Periods: periods,
		Teachers: map[string]EngineTeacher{
			"teacher-computer": {ID: "teacher-computer", WorkspaceID: "workspace", WorkloadLimit: len(periods), Unavailable: map[string]bool{}},
			"teacher-arts-a":   {ID: "teacher-arts-a", WorkspaceID: "workspace", WorkloadLimit: len(periods), Unavailable: map[string]bool{}},
			"teacher-arts-b":   {ID: "teacher-arts-b", WorkspaceID: "workspace", WorkloadLimit: len(periods), Unavailable: map[string]bool{}},
			"teacher-ict":      {ID: "teacher-ict", WorkspaceID: "workspace", WorkloadLimit: len(periods), Unavailable: map[string]bool{}},
		},
		Cohorts: map[string]EngineCohort{
			"g10a": {ID: "g10a", WorkspaceID: "workspace", Unavailable: map[string]bool{}},
			"g10b": {ID: "g10b", WorkspaceID: "workspace", Unavailable: map[string]bool{}},
			"g10c": {ID: "g10c", WorkspaceID: "workspace", Unavailable: map[string]bool{}},
			"g10d": {ID: "g10d", WorkspaceID: "workspace", Unavailable: map[string]bool{}},
		},
		Learners: map[string]EngineLearner{
			"a-cs": {ID: "a-cs", WorkspaceID: "workspace", CohortID: "g10a", Active: true},
			"a-fa": {ID: "a-fa", WorkspaceID: "workspace", CohortID: "g10a", Active: true},
			"b-cs": {ID: "b-cs", WorkspaceID: "workspace", CohortID: "g10b", Active: true},
			"b-fa": {ID: "b-fa", WorkspaceID: "workspace", CohortID: "g10b", Active: true},
			"c-cs": {ID: "c-cs", WorkspaceID: "workspace", CohortID: "g10c", Active: true},
			"c-fa": {ID: "c-fa", WorkspaceID: "workspace", CohortID: "g10c", Active: true},
			"d-cs": {ID: "d-cs", WorkspaceID: "workspace", CohortID: "g10d", Active: true},
			"d-fa": {ID: "d-fa", WorkspaceID: "workspace", CohortID: "g10d", Active: true},
		},
		Registrations: map[string]map[string]bool{
			"g10a": {"a-computer": true, "a-arts": true, "a-ict": true},
			"g10b": {"b-computer": true, "b-arts": true, "b-ict": true},
			"g10c": {"c-computer": true, "c-arts": true, "c-ict": true},
			"g10d": {"d-computer": true, "d-arts": true, "d-ict": true},
		},
		Assignments: []EngineAssignment{
			{ID: "a-computer", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-computer", CohortID: "g10a", CohortSubjectID: "a-computer", SubjectID: "computer", WeeklyPeriods: 1, Mandatory: true, Active: true},
			{ID: "b-computer", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-computer", CohortID: "g10b", CohortSubjectID: "b-computer", SubjectID: "computer", WeeklyPeriods: 1, Mandatory: true, Active: true},
			{ID: "c-computer", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-computer", CohortID: "g10c", CohortSubjectID: "c-computer", SubjectID: "computer", WeeklyPeriods: 1, Mandatory: true, Active: true},
			{ID: "d-computer", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-computer", CohortID: "g10d", CohortSubjectID: "d-computer", SubjectID: "computer", WeeklyPeriods: 1, Mandatory: true, Active: true},
			{ID: "a-arts", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-arts-a", CohortID: "g10a", CohortSubjectID: "a-arts", SubjectID: "arts", WeeklyPeriods: 1, Mandatory: true, Active: true},
			{ID: "b-arts", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-arts-a", CohortID: "g10b", CohortSubjectID: "b-arts", SubjectID: "arts", WeeklyPeriods: 1, Mandatory: true, Active: true},
			{ID: "c-arts", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-arts-b", CohortID: "g10c", CohortSubjectID: "c-arts", SubjectID: "arts", WeeklyPeriods: 1, Mandatory: true, Active: true},
			{ID: "d-arts", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-arts-b", CohortID: "g10d", CohortSubjectID: "d-arts", SubjectID: "arts", WeeklyPeriods: 1, Mandatory: true, Active: true},
		},
	}
}

func TestSplitCohortDisjointLearnersCanSharePeriod(t *testing.T) {
	problem := audienceScenarioProblem()
	problem.Assignments = []EngineAssignment{problem.Assignments[0], problem.Assignments[4]}
	report := ValidateSchedule(problem, []EnginePlacement{
		{AssignmentID: "a-computer", AssignmentIDs: []string{"a-computer"}, DeliveryGroupID: "group-computer-a", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-computer", CohortID: "g10a", CohortIDs: []string{"g10a"}, SubjectID: "computer", LearnerIDs: []string{"a-cs"}, PeriodIDs: []string{"p1"}},
		{AssignmentID: "a-arts", AssignmentIDs: []string{"a-arts"}, DeliveryGroupID: "group-arts-a", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-arts-a", CohortID: "g10a", CohortIDs: []string{"g10a"}, SubjectID: "arts", LearnerIDs: []string{"a-fa"}, PeriodIDs: []string{"p1"}},
	}, EngineConfig{})
	if !report.Valid {
		t.Fatalf("split disjoint learners should validate: %+v", firstFailure(report))
	}
}

func TestIllegalLearnerOverlapIsRejected(t *testing.T) {
	problem := audienceScenarioProblem()
	problem.Assignments = []EngineAssignment{problem.Assignments[0], problem.Assignments[4]}
	report := ValidateSchedule(problem, []EnginePlacement{
		{AssignmentID: "a-computer", AssignmentIDs: []string{"a-computer"}, DeliveryGroupID: "group-computer-a", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-computer", CohortID: "g10a", CohortIDs: []string{"g10a"}, SubjectID: "computer", LearnerIDs: []string{"a-cs"}, PeriodIDs: []string{"p1"}},
		{AssignmentID: "a-arts", AssignmentIDs: []string{"a-arts"}, DeliveryGroupID: "group-arts-a", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-arts-a", CohortID: "g10a", CohortIDs: []string{"g10a"}, SubjectID: "arts", LearnerIDs: []string{"a-cs"}, PeriodIDs: []string{"p1"}},
	}, EngineConfig{})
	if report.Valid || !hasFailedInvariant(report, "NO_LEARNER_DOUBLE_BOOKING") {
		t.Fatalf("overlapping learner audiences should fail deterministically: %+v", report.Results)
	}
}

func TestFourCohortMergedLessonCreditsEveryAssignment(t *testing.T) {
	problem := audienceScenarioProblem()
	problem.Assignments = problem.Assignments[:4]
	problem.DeliveryGroups = []EngineDeliveryGroup{{
		ID: "merged-computer", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term",
		TeacherID: "teacher-computer", SubjectID: "computer",
		AssignmentIDs: []string{"a-computer", "b-computer", "c-computer", "d-computer"},
		CohortIDs: []string{"g10a", "g10b", "g10c", "g10d"}, LearnerIDs: []string{"a-cs", "b-cs", "c-cs", "d-cs"},
		WeeklyPeriods: 1, Mandatory: true, Active: true,
	}}
	result := Solve(problem, EngineConfig{Seed: 3, TimeBudget: time.Second, IterationBudget: 10000, Restarts: 1})
	if !result.Validation.Valid || len(result.Placements) != 1 {
		t.Fatalf("merged lesson should solve as one placement: status=%s validation=%+v", result.Status, result.Validation)
	}
	if got := len(result.Placements[0].AssignmentIDs); got != 4 {
		t.Fatalf("merged placement credited %d assignments", got)
	}
}

func TestParallelAlternativesArePlacedTogether(t *testing.T) {
	problem := audienceScenarioProblem()
	problem.DeliveryGroups = []EngineDeliveryGroup{
		{ID: "merged-computer", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-computer", SubjectID: "computer", AssignmentIDs: []string{"a-computer", "b-computer", "c-computer", "d-computer"}, CohortIDs: []string{"g10a", "g10b", "g10c", "g10d"}, LearnerIDs: []string{"a-cs", "b-cs", "c-cs", "d-cs"}, WeeklyPeriods: 1, Mandatory: true, Active: true},
		{ID: "arts-ab", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-arts-a", SubjectID: "arts", AssignmentIDs: []string{"a-arts", "b-arts"}, CohortIDs: []string{"g10a", "g10b"}, LearnerIDs: []string{"a-fa", "b-fa"}, WeeklyPeriods: 1, Mandatory: true, Active: true},
		{ID: "arts-cd", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-arts-b", SubjectID: "arts", AssignmentIDs: []string{"c-arts", "d-arts"}, CohortIDs: []string{"g10c", "g10d"}, LearnerIDs: []string{"c-fa", "d-fa"}, WeeklyPeriods: 1, Mandatory: true, Active: true},
	}
	problem.ParallelBlocks = []EngineParallelBlock{{ID: "elective-block", WorkspaceID: "workspace", GroupIDs: []string{"merged-computer", "arts-ab", "arts-cd"}, Active: true}}
	result := Solve(problem, EngineConfig{Seed: 5, TimeBudget: time.Second, IterationBudget: 10000, Restarts: 1})
	if !result.Validation.Valid {
		t.Fatalf("parallel alternatives should solve: %+v", firstFailure(result.Validation))
	}
	period := result.Placements[0].PeriodIDs[0]
	for _, placement := range result.Placements {
		if placement.ParallelBlockID != "elective-block" || placement.PeriodIDs[0] != period {
			t.Fatalf("parallel block member not placed atomically: %+v", result.Placements)
		}
	}
}

func TestMixedMergedAndResidualDemand(t *testing.T) {
	problem := audienceScenarioProblem()
	problem.Periods = []EnginePeriod{}
	for day := 0; day < 5; day++ {
		for index := 0; index < 4; index++ {
			problem.Periods = append(problem.Periods, EnginePeriod{ID: "mp" + string(rune('a'+day)) + string(rune('0'+index)), Day: day, Index: index, Teaching: true, Mandatory: true})
		}
	}
	teacher := problem.Teachers["teacher-computer"]
	teacher.WorkloadLimit = len(problem.Periods)
	problem.Teachers["teacher-computer"] = teacher
	problem.Assignments = problem.Assignments[:4]
	for index := range problem.Assignments {
		problem.Assignments[index].WeeklyPeriods = 5
	}
	problem.DeliveryGroups = []EngineDeliveryGroup{{
		ID: "merged-computer", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term",
		TeacherID: "teacher-computer", SubjectID: "computer",
		AssignmentIDs: []string{"a-computer", "b-computer", "c-computer", "d-computer"},
		CohortIDs: []string{"g10a", "g10b", "g10c", "g10d"}, LearnerIDs: []string{"a-cs", "b-cs", "c-cs", "d-cs"},
		WeeklyPeriods: 2, Mandatory: true, Active: true,
	}}
	result := Solve(problem, EngineConfig{Seed: 7, TimeBudget: 2 * time.Second, IterationBudget: 100000, Restarts: 2})
	if !result.Validation.Valid {
		t.Fatalf("merged plus residual demand should solve: %+v", firstFailure(result.Validation))
	}
	mergedPeriods := 0
	for _, placement := range result.Placements {
		if placement.DeliveryGroupID == "merged-computer" {
			mergedPeriods += len(placement.PeriodIDs)
		}
	}
	if mergedPeriods != 2 {
		t.Fatalf("expected exactly two shared merged periods, got %d", mergedPeriods)
	}
}

func TestDoubleMergedLessonCreditsTwoPeriodsPerAssignment(t *testing.T) {
	problem := audienceScenarioProblem()
	problem.Assignments = problem.Assignments[:4]
	for index := range problem.Assignments {
		problem.Assignments[index].WeeklyPeriods = 2
		problem.Assignments[index].DoubleBlocks = 1
	}
	problem.DeliveryGroups = []EngineDeliveryGroup{{
		ID: "merged-computer", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term",
		TeacherID: "teacher-computer", SubjectID: "computer",
		AssignmentIDs: []string{"a-computer", "b-computer", "c-computer", "d-computer"},
		CohortIDs: []string{"g10a", "g10b", "g10c", "g10d"}, LearnerIDs: []string{"a-cs", "b-cs", "c-cs", "d-cs"},
		WeeklyPeriods: 2, DoubleBlocks: 1, Mandatory: true, Active: true,
	}}
	result := Solve(problem, EngineConfig{Seed: 11, TimeBudget: time.Second, IterationBudget: 10000, Restarts: 1})
	if !result.Validation.Valid || len(result.Placements) != 1 || !result.Placements[0].Double {
		t.Fatalf("double merged lesson should be one adjacent placement: %+v", result)
	}
}

func TestCompulsorySubjectWithOneTeacherIsStaggered(t *testing.T) {
	problem := audienceScenarioProblem()
	problem.Assignments = []EngineAssignment{
		{ID: "a-ict", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-ict", CohortID: "g10a", CohortSubjectID: "a-ict", SubjectID: "ict", WeeklyPeriods: 1, Mandatory: true, Active: true},
		{ID: "b-ict", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-ict", CohortID: "g10b", CohortSubjectID: "b-ict", SubjectID: "ict", WeeklyPeriods: 1, Mandatory: true, Active: true},
		{ID: "c-ict", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-ict", CohortID: "g10c", CohortSubjectID: "c-ict", SubjectID: "ict", WeeklyPeriods: 1, Mandatory: true, Active: true},
		{ID: "d-ict", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher-ict", CohortID: "g10d", CohortSubjectID: "d-ict", SubjectID: "ict", WeeklyPeriods: 1, Mandatory: true, Active: true},
	}
	result := Solve(problem, EngineConfig{Seed: 13, TimeBudget: time.Second, IterationBudget: 10000, Restarts: 1})
	if !result.Validation.Valid {
		t.Fatalf("single ICT teacher should be staggered, not simultaneous: %+v", firstFailure(result.Validation))
	}
	seen := map[string]bool{}
	for _, placement := range result.Placements {
		if seen[placement.PeriodIDs[0]] {
			t.Fatalf("teacher was double-booked in period %s: %+v", placement.PeriodIDs[0], result.Placements)
		}
		seen[placement.PeriodIDs[0]] = true
	}
}

func TestSchoolWideBreakBlocksEveryCohort(t *testing.T) {
	problem := EngineProblem{
		WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term",
		Periods: []EnginePeriod{{ID: "break", Day: 0, Index: 0, Teaching: false}},
		Teachers: map[string]EngineTeacher{"teacher": {ID: "teacher", WorkspaceID: "workspace", Unavailable: map[string]bool{}}},
		Cohorts: map[string]EngineCohort{"green": {ID: "green", WorkspaceID: "workspace", Unavailable: map[string]bool{}}, "yellow": {ID: "yellow", WorkspaceID: "workspace", Unavailable: map[string]bool{}}},
		Registrations: map[string]map[string]bool{"green": {"green-math": true}, "yellow": {"yellow-math": true}},
		Assignments: []EngineAssignment{{ID: "green-math", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher", CohortID: "green", CohortSubjectID: "green-math", SubjectID: "math", WeeklyPeriods: 1, Mandatory: true, Active: true}},
	}
	report := ValidateSchedule(problem, []EnginePlacement{{AssignmentID: "green-math", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "teacher", CohortID: "green", CohortSubjectID: "green-math", SubjectID: "math", PeriodIDs: []string{"break"}}}, EngineConfig{})
	if report.Valid || !hasFailedInvariant(report, "NON_TEACHING_PERIOD_EXCLUSION") {
		t.Fatalf("school-wide break should reject all lesson placement: %+v", report.Results)
	}
}

func TestLifeSkillsCanRunBesideICTButTeacherCannotDoubleBook(t *testing.T) {
	problem := EngineProblem{
		WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term",
		Periods: []EnginePeriod{{ID: "p1", Day: 0, Index: 0, Teaching: true, Mandatory: true}},
		Teachers: map[string]EngineTeacher{
			"shared-teacher": {ID: "shared-teacher", WorkspaceID: "workspace", Unavailable: map[string]bool{}},
			"ict-teacher":    {ID: "ict-teacher", WorkspaceID: "workspace", Unavailable: map[string]bool{}},
		},
		Cohorts: map[string]EngineCohort{"green": {ID: "green", WorkspaceID: "workspace", Unavailable: map[string]bool{}}, "yellow": {ID: "yellow", WorkspaceID: "workspace", Unavailable: map[string]bool{}}},
		Registrations: map[string]map[string]bool{"green": {"green-life": true}, "yellow": {"yellow-ict": true}},
		Assignments: []EngineAssignment{
			{ID: "green-life", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "shared-teacher", CohortID: "green", CohortSubjectID: "green-life", SubjectID: "life", WeeklyPeriods: 1, Mandatory: true, Active: true},
			{ID: "yellow-ict", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "ict-teacher", CohortID: "yellow", CohortSubjectID: "yellow-ict", SubjectID: "ict", WeeklyPeriods: 1, Mandatory: true, Active: true},
		},
	}
	valid := ValidateSchedule(problem, []EnginePlacement{
		{AssignmentID: "green-life", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "shared-teacher", CohortID: "green", CohortSubjectID: "green-life", SubjectID: "life", PeriodIDs: []string{"p1"}},
		{AssignmentID: "yellow-ict", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "ict-teacher", CohortID: "yellow", CohortSubjectID: "yellow-ict", SubjectID: "ict", PeriodIDs: []string{"p1"}},
	}, EngineConfig{})
	if !valid.Valid {
		t.Fatalf("Life Skills and ICT in separate cohorts should be valid: %+v", firstFailure(valid))
	}
	doubleBookedProblem := problem
	doubleBookedProblem.Assignments = append([]EngineAssignment(nil), problem.Assignments...)
	doubleBookedProblem.Assignments[1].TeacherID = "shared-teacher"
	conflict := ValidateSchedule(doubleBookedProblem, []EnginePlacement{
		{AssignmentID: "green-life", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "shared-teacher", CohortID: "green", CohortSubjectID: "green-life", SubjectID: "life", PeriodIDs: []string{"p1"}},
		{AssignmentID: "yellow-ict", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "shared-teacher", CohortID: "yellow", CohortSubjectID: "yellow-ict", SubjectID: "ict", PeriodIDs: []string{"p1"}},
	}, EngineConfig{})
	if conflict.Valid || !hasFailedInvariant(conflict, "NO_TEACHER_DOUBLE_BOOKING") {
		t.Fatalf("teacher supervising two cohorts at the same time should fail: %+v", conflict.Results)
	}
}

func TestExclusivePEResourceAndTransitionBoundary(t *testing.T) {
	problem := EngineProblem{
		WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term",
		Periods: []EnginePeriod{{ID: "p1", Day: 0, Index: 0, Teaching: true, Mandatory: true}, {ID: "transition", Day: 0, Index: 1, Teaching: false}, {ID: "p2", Day: 0, Index: 2, Teaching: true, Mandatory: true}},
		Teachers: map[string]EngineTeacher{"t1": {ID: "t1", WorkspaceID: "workspace", Unavailable: map[string]bool{}}, "t2": {ID: "t2", WorkspaceID: "workspace", Unavailable: map[string]bool{}}},
		Cohorts: map[string]EngineCohort{"green": {ID: "green", WorkspaceID: "workspace", Unavailable: map[string]bool{}}, "yellow": {ID: "yellow", WorkspaceID: "workspace", Unavailable: map[string]bool{}}},
		Resources: map[string]EngineResource{"field": {ID: "field", WorkspaceID: "workspace", Capacity: 1, Unavailable: map[string]bool{}}},
		Registrations: map[string]map[string]bool{"green": {"green-pe": true}, "yellow": {"yellow-pe": true}},
		Assignments: []EngineAssignment{
			{ID: "green-pe", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "t1", CohortID: "green", CohortSubjectID: "green-pe", SubjectID: "pe", ResourceID: "field", WeeklyPeriods: 1, Mandatory: true, Active: true},
			{ID: "yellow-pe", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "t2", CohortID: "yellow", CohortSubjectID: "yellow-pe", SubjectID: "pe", ResourceID: "field", WeeklyPeriods: 1, Mandatory: true, Active: true},
		},
	}
	resourceConflict := ValidateSchedule(problem, []EnginePlacement{
		{AssignmentID: "green-pe", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "t1", CohortID: "green", CohortSubjectID: "green-pe", SubjectID: "pe", ResourceID: "field", PeriodIDs: []string{"p1"}},
		{AssignmentID: "yellow-pe", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "t2", CohortID: "yellow", CohortSubjectID: "yellow-pe", SubjectID: "pe", ResourceID: "field", PeriodIDs: []string{"p1"}},
	}, EngineConfig{})
	if resourceConflict.Valid || !hasFailedInvariant(resourceConflict, "RESOURCE_EXCLUSIVITY") {
		t.Fatalf("exclusive PE resource collision should fail: %+v", resourceConflict.Results)
	}
	doubleAcrossTransition := ValidateSchedule(problem, []EnginePlacement{
		{AssignmentID: "green-pe", WorkspaceID: "workspace", AcademicYearID: "year", TermID: "term", TeacherID: "t1", CohortID: "green", CohortSubjectID: "green-pe", SubjectID: "pe", ResourceID: "field", PeriodIDs: []string{"p1", "p2"}, Double: true},
	}, EngineConfig{})
	if doubleAcrossTransition.Valid || !hasFailedInvariant(doubleAcrossTransition, "DOUBLE_LESSON_CONTIGUITY") {
		t.Fatalf("double lesson crossing transition should fail: %+v", doubleAcrossTransition.Results)
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

func hasFailedInvariant(report ValidationReport, invariant string) bool {
	for _, result := range report.Results {
		if result.Invariant == invariant && !result.Passed {
			return true
		}
	}
	return false
}
