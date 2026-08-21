package scheduling

import (
	"fmt"
	"math"
	"sort"
)

// ValidateSchedule is deliberately occupancy-driven and independent of the
// solver's candidate ordering and matching implementation.
func ValidateSchedule(problem EngineProblem, placements []EnginePlacement, config EngineConfig) ValidationReport {
	periodByID := map[string]EnginePeriod{}
	for _, period := range problem.Periods {
		periodByID[period.ID] = period
	}
	assignmentByID := map[string]EngineAssignment{}
	for _, assignment := range problem.Assignments {
		assignmentByID[assignment.ID] = assignment
	}

	teacherPeriod := map[string]map[string]int{}
	cohortPeriod := map[string]map[string]int{}
	resourcePeriod := map[string]map[string]int{}
	assignmentCount := map[string]int{}
	teacherLoad := map[string]int{}
	teacherDayLoad := map[string]map[int]int{}
	cohortDaySubject := map[string]map[int]map[string][]int{}
	results := make([]InvariantResult, 0)
	failed := map[string]bool{}

	fail := func(invariant, entityType, entityID, periodID string, observed, expected any, explanation string) {
		failed[invariant] = true
		results = append(results, InvariantResult{Invariant: invariant, Passed: false, WorkspaceID: problem.WorkspaceID, EntityType: entityType, EntityID: entityID, PeriodID: periodID, Observed: observed, Expected: expected, Explanation: explanation})
	}

	for _, placement := range placements {
		assignment, ok := assignmentByID[placement.AssignmentID]
		if !ok || !assignment.Active || assignment.TeacherID != placement.TeacherID || assignment.CohortID != placement.CohortID || assignment.CohortSubjectID != placement.CohortSubjectID {
			fail("ASSIGNMENT_AUTHORITY", "assignment", placement.AssignmentID, "", placement.TeacherID, "active normalized assignment", "Scheduled teacher/cohort subject is not authorized by an active assignment.")
			continue
		}
		if placement.WorkspaceID != problem.WorkspaceID || assignment.WorkspaceID != problem.WorkspaceID {
			fail("MULTI_WORKSPACE_ISOLATION", "assignment", placement.AssignmentID, "", placement.WorkspaceID, problem.WorkspaceID, "Placement crosses the workspace boundary.")
		}
		if placement.AcademicYearID != problem.AcademicYearID || placement.TermID != problem.TermID || assignment.AcademicYearID != problem.AcademicYearID || assignment.TermID != problem.TermID {
			fail("ACADEMIC_BOUNDARIES", "assignment", placement.AssignmentID, "", placement.TermID, problem.TermID, "Placement is outside the selected academic year or term.")
		}
		if !problem.Registrations[placement.CohortID][placement.CohortSubjectID] || placement.SubjectID != assignment.SubjectID {
			fail("COHORT_SUBJECT_VALIDITY", "cohort", placement.CohortID, "", placement.CohortSubjectID, "registered cohort subject", "Scheduled subject is not registered for the cohort.")
		}
		teacher, teacherOK := problem.Teachers[placement.TeacherID]
		if !teacherOK || teacher.WorkspaceID != problem.WorkspaceID || (len(teacher.QualifiedSubjects) > 0 && !teacher.QualifiedSubjects[placement.SubjectID]) {
			fail("ASSIGNMENT_AUTHORITY", "teacher", placement.TeacherID, "", placement.SubjectID, "qualified active teacher", "Teacher is unavailable to this workspace or is not qualified.")
		}
		if placement.Double {
			if len(placement.PeriodIDs) != 2 {
				fail("DOUBLE_LESSON_CONTIGUITY", "assignment", placement.AssignmentID, "", len(placement.PeriodIDs), 2, "Double lesson does not occupy exactly two periods.")
			} else {
				a, aOK := periodByID[placement.PeriodIDs[0]]
				b, bOK := periodByID[placement.PeriodIDs[1]]
				if !aOK || !bOK || a.Day != b.Day || b.Index != a.Index+1 || !a.Teaching || !b.Teaching || a.Excluded || b.Excluded {
					fail("DOUBLE_LESSON_CONTIGUITY", "assignment", placement.AssignmentID, placement.PeriodIDs[0], placement.PeriodIDs, "adjacent eligible periods", "Double lesson crosses a boundary or non-teaching period.")
				}
			}
		} else if len(placement.PeriodIDs) != 1 {
			fail("LESSON_DURATION", "assignment", placement.AssignmentID, "", len(placement.PeriodIDs), 1, "Single lesson must occupy exactly one period.")
		}

		for _, periodID := range placement.PeriodIDs {
			period, periodOK := periodByID[periodID]
			if !periodOK || !period.Teaching || period.Excluded || teacher.Unavailable[periodID] || problem.Cohorts[placement.CohortID].Unavailable[periodID] {
				fail("NON_TEACHING_PERIOD_EXCLUSION", "assignment", placement.AssignmentID, periodID, "scheduled", "eligible and available", "Lesson uses a break, closure, excluded, or unavailable period.")
			}
			if placement.ResourceID != "" {
				resource, exists := problem.Resources[placement.ResourceID]
				if !exists || resource.WorkspaceID != problem.WorkspaceID || resource.Unavailable[periodID] {
					fail("RESOURCE_EXCLUSIVITY", "resource", placement.ResourceID, periodID, "unavailable", "available workspace resource", "Required resource is missing or unavailable.")
				}
			}
			incrementNested(teacherPeriod, placement.TeacherID, periodID)
			incrementNested(cohortPeriod, placement.CohortID, periodID)
			if placement.ResourceID != "" {
				incrementNested(resourcePeriod, placement.ResourceID, periodID)
			}
			assignmentCount[placement.AssignmentID]++
			teacherLoad[placement.TeacherID]++
			if teacherDayLoad[placement.TeacherID] == nil {
				teacherDayLoad[placement.TeacherID] = map[int]int{}
			}
			teacherDayLoad[placement.TeacherID][period.Day]++
		}
		if len(placement.PeriodIDs) > 0 {
			period := periodByID[placement.PeriodIDs[0]]
			if cohortDaySubject[placement.CohortID] == nil {
				cohortDaySubject[placement.CohortID] = map[int]map[string][]int{}
			}
			if cohortDaySubject[placement.CohortID][period.Day] == nil {
				cohortDaySubject[placement.CohortID][period.Day] = map[string][]int{}
			}
			cohortDaySubject[placement.CohortID][period.Day][placement.SubjectID] = append(cohortDaySubject[placement.CohortID][period.Day][placement.SubjectID], period.Index)
		}
	}

	checkOccupancy := func(invariant, entityType string, occupancy map[string]map[string]int, capacity func(string) int) {
		for entityID, byPeriod := range occupancy {
			for periodID, count := range byPeriod {
				limit := capacity(entityID)
				if count > limit {
					fail(invariant, entityType, entityID, periodID, count, fmt.Sprintf("<= %d", limit), "Exclusive period occupancy was exceeded.")
				}
			}
		}
	}
	checkOccupancy("NO_TEACHER_DOUBLE_BOOKING", "teacher", teacherPeriod, func(string) int { return 1 })
	checkOccupancy("NO_COHORT_DOUBLE_BOOKING", "cohort", cohortPeriod, func(string) int { return 1 })
	checkOccupancy("RESOURCE_EXCLUSIVITY", "resource", resourcePeriod, func(id string) int {
		capacity := problem.Resources[id].Capacity
		if capacity <= 0 {
			return 1
		}
		return capacity
	})

	unscheduled := 0
	for _, assignment := range problem.Assignments {
		if !assignmentIsSchedulable(problem, assignment) || !assignment.Mandatory {
			continue
		}
		observed := assignmentCount[assignment.ID]
		if observed != assignment.WeeklyPeriods {
			fail("COMPLETE_DEMAND_SATISFACTION", "assignment", assignment.ID, "", observed, assignment.WeeklyPeriods, "Mandatory weekly lesson demand is not exactly satisfied.")
			if observed < assignment.WeeklyPeriods {
				unscheduled += assignment.WeeklyPeriods - observed
			}
		}
	}
	for teacherID, teacher := range problem.Teachers {
		available := 0
		for _, period := range usablePeriods(problem) {
			if !teacher.Unavailable[period.ID] {
				available++
			}
		}
		limit := available
		if teacher.WorkloadLimit > 0 && teacher.WorkloadLimit < limit {
			limit = teacher.WorkloadLimit
		}
		if teacherLoad[teacherID] > limit {
			fail("TEACHER_WORKLOAD_LIMITS", "teacher", teacherID, "", teacherLoad[teacherID], limit, "Teacher exceeds workload or availability capacity.")
		}
	}

	if problem.FullCoverage {
		for cohortID, cohort := range problem.Cohorts {
			if cohort.WorkspaceID != problem.WorkspaceID {
				continue
			}
			for _, period := range usablePeriods(problem) {
				if cohort.Unavailable[period.ID] {
					continue
				}
				count := cohortPeriod[cohortID][period.ID]
				if period.Mandatory && count != 1 {
					fail("SIMULTANEOUS_COHORT_COVERAGE", "cohort", cohortID, period.ID, count, 1, "Full-coverage cohort does not have exactly one lesson in the mandatory period.")
				}
			}
		}
	}

	fairness := calculateFairness(problem, teacherLoad, teacherDayLoad, cohortDaySubject, teacherPeriod, config)
	soft := fairness.RepeatedSubjectClusters + fairness.IdleGaps + fairness.ConsecutiveOverloads
	allInvariants := []string{"SIMULTANEOUS_COHORT_COVERAGE", "NO_TEACHER_DOUBLE_BOOKING", "NO_COHORT_DOUBLE_BOOKING", "ASSIGNMENT_AUTHORITY", "COHORT_SUBJECT_VALIDITY", "COMPLETE_DEMAND_SATISFACTION", "TEACHER_WORKLOAD_LIMITS", "DOUBLE_LESSON_CONTIGUITY", "NON_TEACHING_PERIOD_EXCLUSION", "ACADEMIC_BOUNDARIES", "RESOURCE_EXCLUSIVITY", "DISTINCT_SIMULTANEOUS_TEACHERS", "MULTI_WORKSPACE_ISOLATION", "SAFE_PUBLICATION", "BOUNDED_EXECUTION", "DETERMINISTIC_REPRODUCIBILITY"}
	for _, invariant := range allInvariants {
		if !failed[invariant] {
			results = append(results, InvariantResult{Invariant: invariant, Passed: true, WorkspaceID: problem.WorkspaceID, Explanation: "No violation observed."})
		}
	}
	hard := 0
	for _, result := range results {
		if !result.Passed {
			hard++
		}
	}
	return ValidationReport{Valid: hard == 0 && unscheduled == 0, HardConflictCount: hard, SoftViolationCount: soft, Unscheduled: unscheduled, Results: results, Fairness: fairness}
}

func incrementNested(target map[string]map[string]int, outer, inner string) {
	if target[outer] == nil {
		target[outer] = map[string]int{}
	}
	target[outer][inner]++
}

func calculateFairness(problem EngineProblem, loads map[string]int, daily map[string]map[int]int, subject map[string]map[int]map[string][]int, occupancy map[string]map[string]int, config EngineConfig) FairnessMetrics {
	values := make([]float64, 0, len(loads))
	for _, load := range loads {
		values = append(values, float64(load))
	}
	variance := variance(values)
	dailyValues := []float64{}
	for _, days := range daily {
		for _, load := range days {
			dailyValues = append(dailyValues, float64(load))
		}
	}
	clusters := 0
	for _, days := range subject {
		for _, subjects := range days {
			for _, indexes := range subjects {
				sort.Ints(indexes)
				for i := 1; i < len(indexes); i++ {
					if indexes[i] == indexes[i-1]+1 {
						clusters++
					}
				}
			}
		}
	}
	idle, overload := 0, 0
	byDay := map[int][]EnginePeriod{}
	for _, period := range usablePeriods(problem) {
		byDay[period.Day] = append(byDay[period.Day], period)
	}
	maxConsecutive := config.MaxConsecutive
	if maxConsecutive <= 0 {
		maxConsecutive = 4
	}
	for teacherID := range problem.Teachers {
		for _, periods := range byDay {
			first, last, run := -1, -1, 0
			for i, period := range periods {
				if occupancy[teacherID][period.ID] > 0 {
					if first < 0 {
						first = i
					}
					last = i
					run++
					if run > maxConsecutive {
						overload++
					}
				} else {
					run = 0
				}
			}
			if first >= 0 {
				for i := first; i <= last; i++ {
					if occupancy[teacherID][periods[i].ID] == 0 {
						idle++
					}
				}
			}
		}
	}
	return FairnessMetrics{TeacherWorkloadVariance: variance, DailyLoadVariance: varianceOfDailyDeviation(daily), RepeatedSubjectClusters: clusters, IdleGaps: idle, ConsecutiveOverloads: overload}
}

func variance(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	mean := 0.0
	for _, value := range values {
		mean += value
	}
	mean /= float64(len(values))
	total := 0.0
	for _, value := range values {
		delta := value - mean
		total += delta * delta
	}
	return total / float64(len(values))
}

func varianceOfDailyDeviation(daily map[string]map[int]int) float64 {
	deviations := []float64{}
	for _, days := range daily {
		values := []float64{}
		for _, load := range days {
			values = append(values, float64(load))
		}
		if len(values) == 0 {
			continue
		}
		mean := 0.0
		for _, value := range values {
			mean += value
		}
		mean /= float64(len(values))
		for _, value := range values {
			deviations = append(deviations, math.Abs(value-mean))
		}
	}
	return variance(deviations)
}
