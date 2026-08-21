package scheduling

import (
	"fmt"
	"runtime"
	"sort"
	"time"
)

type SimulationProfile struct {
	Name               string `json:"name"`
	Teachers           int    `json:"teachers"`
	Cohorts            int    `json:"cohorts"`
	Subjects           int    `json:"subjects"`
	Days               int    `json:"days"`
	PeriodsPerDay      int    `json:"periods_per_day"`
	DoubleLessons      bool   `json:"double_lessons"`
	AvailabilityLimits bool   `json:"availability_limits"`
	ResourceCount      int    `json:"resource_count"`
	Seed               int64  `json:"seed"`
}

type BenchmarkObservation struct {
	Profile            string          `json:"profile"`
	Teachers           int             `json:"teachers"`
	Cohorts            int             `json:"cohorts"`
	Subjects           int             `json:"subjects"`
	WeeklyDemand       int             `json:"weekly_demand"`
	AvailablePeriods   int             `json:"available_periods"`
	DoubleLessons      int             `json:"double_lessons"`
	Seed               int64           `json:"seed"`
	PreflightDuration  time.Duration   `json:"preflight_duration"`
	SolveDuration      time.Duration   `json:"solve_duration"`
	Iterations         int             `json:"iterations"`
	Restarts           int             `json:"restarts"`
	MemoryBytes        uint64          `json:"memory_bytes"`
	ScheduledLessons   int             `json:"scheduled_lessons"`
	UnscheduledLessons int             `json:"unscheduled_lessons"`
	HardConflicts      int             `json:"hard_conflicts"`
	SoftViolations     int             `json:"soft_violations"`
	TeacherUtilization float64         `json:"teacher_utilization"`
	CohortCoverage     float64         `json:"cohort_coverage"`
	Fairness           FairnessMetrics `json:"fairness"`
	Status             SolveStatus     `json:"status"`
}

type assignmentAggregate struct {
	assignment EngineAssignment
	witness    []string
}

// GenerateFeasibleSimulation derives normalized demand from a constructive
// Latin-rotation witness. The witness proves fixture feasibility but is not
// included in EngineProblem.Existing and is never visible to Solve.
func GenerateFeasibleSimulation(profile SimulationProfile) (EngineProblem, []EnginePlacement) {
	workspace := "workspace-simulation"
	year := "year-2026"
	term := "term-1"
	periods := make([]EnginePeriod, 0, profile.Days*profile.PeriodsPerDay)
	for day := 0; day < profile.Days; day++ {
		for local := 0; local < profile.PeriodsPerDay; local++ {
			// The index gap is an explicit lunch/break boundary. It prevents a
			// double lesson from crossing the middle of the day.
			index := local
			if local >= profile.PeriodsPerDay/2 {
				index++
			}
			periods = append(periods, EnginePeriod{ID: fmt.Sprintf("d%02d-p%02d", day, local), Day: day, Index: index, Teaching: true, Mandatory: true})
		}
	}
	teachers := map[string]EngineTeacher{}
	cohorts := map[string]EngineCohort{}
	registrations := map[string]map[string]bool{}
	allQualifications := map[string]bool{}
	for subject := 0; subject < profile.Subjects; subject++ {
		allQualifications[fmt.Sprintf("subject-%03d", subject)] = true
	}
	for teacher := 0; teacher < profile.Teachers; teacher++ {
		id := fmt.Sprintf("teacher-%04d", teacher)
		qualified := map[string]bool{}
		for subject := range allQualifications {
			qualified[subject] = true
		}
		teachers[id] = EngineTeacher{ID: id, WorkspaceID: workspace, WorkloadLimit: len(periods), Unavailable: map[string]bool{}, QualifiedSubjects: qualified}
	}
	for cohort := 0; cohort < profile.Cohorts; cohort++ {
		id := fmt.Sprintf("cohort-%04d", cohort)
		cohorts[id] = EngineCohort{ID: id, WorkspaceID: workspace, Unavailable: map[string]bool{}}
		registrations[id] = map[string]bool{}
	}

	aggregates := map[string]*assignmentAggregate{}
	witness := make([]EnginePlacement, 0, profile.Cohorts*len(periods))
	usedTeacherAtPeriod := map[string]map[string]bool{}
	for periodIndex, period := range periods {
		usedTeacherAtPeriod[period.ID] = map[string]bool{}
		for cohort := 0; cohort < profile.Cohorts; cohort++ {
			rotation := periodIndex
			if profile.DoubleLessons {
				rotation = (periodIndex / 2) * 2
			}
			teacherIndex := (cohort + rotation) % profile.Teachers
			subjectIndex := (cohort*7 + rotation) % profile.Subjects
			teacherID := fmt.Sprintf("teacher-%04d", teacherIndex)
			cohortID := fmt.Sprintf("cohort-%04d", cohort)
			subjectID := fmt.Sprintf("subject-%03d", subjectIndex)
			cohortSubjectID := fmt.Sprintf("cs-%04d-%03d", cohort, subjectIndex)
			registrations[cohortID][cohortSubjectID] = true
			key := teacherID + "|" + cohortID + "|" + cohortSubjectID
			aggregate := aggregates[key]
			if aggregate == nil {
				assignmentID := "assignment-" + key
				aggregate = &assignmentAggregate{assignment: EngineAssignment{ID: assignmentID, WorkspaceID: workspace, AcademicYearID: year, TermID: term, TeacherID: teacherID, CohortID: cohortID, CohortSubjectID: cohortSubjectID, SubjectID: subjectID, Mandatory: true, Active: true}}
				aggregates[key] = aggregate
			}
			aggregate.assignment.WeeklyPeriods++
			aggregate.witness = append(aggregate.witness, period.ID)
			usedTeacherAtPeriod[period.ID][teacherID] = true
		}
	}
	if profile.DoubleLessons {
		for _, aggregate := range aggregates {
			aggregate.assignment.DoubleBlocks = aggregate.assignment.WeeklyPeriods / 2
		}
	}
	if profile.AvailabilityLimits {
		for teacherID, teacher := range teachers {
			for index, period := range periods {
				if index%11 == 0 && !usedTeacherAtPeriod[period.ID][teacherID] {
					teacher.Unavailable[period.ID] = true
				}
			}
			teachers[teacherID] = teacher
		}
	}
	assignments := make([]EngineAssignment, 0, len(aggregates))
	for _, aggregate := range aggregates {
		assignments = append(assignments, aggregate.assignment)
		if profile.DoubleLessons {
			for index := 0; index+1 < len(aggregate.witness); index += 2 {
				a := aggregate.witness[index]
				b := aggregate.witness[index+1]
				witness = append(witness, placementFromAssignment(workspace, year, term, aggregate.assignment, []string{a, b}, true))
			}
		} else {
			for _, periodID := range aggregate.witness {
				witness = append(witness, placementFromAssignment(workspace, year, term, aggregate.assignment, []string{periodID}, false))
			}
		}
	}
	sort.Slice(assignments, func(i, j int) bool { return assignments[i].ID < assignments[j].ID })
	problem := EngineProblem{WorkspaceID: workspace, AcademicYearID: year, TermID: term, Periods: periods, Teachers: teachers, Cohorts: cohorts, Assignments: assignments, Registrations: registrations, FullCoverage: true, Resources: map[string]EngineResource{}}
	return problem, witness
}

func placementFromAssignment(workspace, year, term string, assignment EngineAssignment, periods []string, double bool) EnginePlacement {
	return EnginePlacement{AssignmentID: assignment.ID, WorkspaceID: workspace, AcademicYearID: year, TermID: term, TeacherID: assignment.TeacherID, CohortID: assignment.CohortID, CohortSubjectID: assignment.CohortSubjectID, SubjectID: assignment.SubjectID, ResourceID: assignment.ResourceID, PeriodIDs: periods, Double: double}
}

func RunSimulation(profile SimulationProfile, config EngineConfig) (BenchmarkObservation, SolveResult) {
	problem, _ := GenerateFeasibleSimulation(profile)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	result := Solve(problem, config)
	runtime.ReadMemStats(&after)
	demand, doubles := 0, 0
	for _, assignment := range problem.Assignments {
		demand += assignment.WeeklyPeriods
		doubles += assignment.DoubleBlocks
	}
	memory := uint64(0)
	if after.TotalAlloc >= before.TotalAlloc {
		memory = after.TotalAlloc - before.TotalAlloc
	}
	teacherCapacity := len(problem.Teachers) * len(problem.Periods)
	cohortCapacity := len(problem.Cohorts) * len(problem.Periods)
	observation := BenchmarkObservation{Profile: profile.Name, Teachers: profile.Teachers, Cohorts: profile.Cohorts, Subjects: profile.Subjects, WeeklyDemand: demand, AvailablePeriods: len(problem.Periods), DoubleLessons: doubles, Seed: config.Seed, PreflightDuration: result.Metrics.PreflightDuration, SolveDuration: result.Metrics.SolveDuration, Iterations: result.Metrics.Iterations, Restarts: result.Metrics.Restarts, MemoryBytes: memory, ScheduledLessons: result.Metrics.ScheduledPeriods, UnscheduledLessons: result.Validation.Unscheduled, HardConflicts: result.Validation.HardConflictCount, SoftViolations: result.Validation.SoftViolationCount, Fairness: result.Validation.Fairness, Status: result.Status}
	if teacherCapacity > 0 {
		observation.TeacherUtilization = float64(result.Metrics.ScheduledPeriods) / float64(teacherCapacity)
	}
	if cohortCapacity > 0 {
		observation.CohortCoverage = float64(result.Metrics.ScheduledPeriods) / float64(cohortCapacity)
	}
	return observation, result
}
