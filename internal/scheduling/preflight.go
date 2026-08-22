package scheduling

import (
	"fmt"
	"sort"
	"time"
)

func usablePeriods(problem EngineProblem) []EnginePeriod {
	periods := make([]EnginePeriod, 0, len(problem.Periods))
	for _, period := range problem.Periods {
		if period.Teaching && !period.Excluded {
			periods = append(periods, period)
		}
	}
	sort.Slice(periods, func(i, j int) bool {
		if periods[i].Day == periods[j].Day {
			return periods[i].Index < periods[j].Index
		}
		return periods[i].Day < periods[j].Day
	})
	return periods
}

func assignmentIsSchedulable(problem EngineProblem, assignment EngineAssignment) bool {
	return assignment.Active && assignment.WorkspaceID == problem.WorkspaceID && assignment.WeeklyPeriods > 0
}

func AnalyzeFeasibility(problem EngineProblem) FeasibilityReport {
	started := time.Now()
	periods := usablePeriods(problem)
	issues := make([]FeasibilityIssue, 0)
	add := func(issue FeasibilityIssue) { issues = append(issues, issue) }

	if problem.WorkspaceID == "" || problem.AcademicYearID == "" || problem.TermID == "" {
		add(FeasibilityIssue{Code: "MISSING_ACADEMIC_SCOPE", Constraint: "ACADEMIC_BOUNDARIES", Message: "Workspace, academic year, and term are required.", SuggestedAction: "Select an active academic year and eligible term."})
	}
	if len(periods) == 0 {
		add(FeasibilityIssue{Code: "NO_USABLE_PERIODS", Constraint: "NON_TEACHING_PERIOD_EXCLUSION", RequiredCapacity: 1, AvailableCapacity: 0, Message: "The active calendar has no usable teaching periods.", SuggestedAction: "Configure and activate bell periods."})
	}
	if len(problem.DeliveryGroups) > 0 || len(problem.Learners) > 0 || len(problem.ParallelBlocks) > 0 {
		return analyzeAudienceFeasibility(problem, periods, issues, started)
	}

	teacherDemand := map[string]int{}
	cohortDemand := map[string]int{}
	doubleDemand := map[string]int{}
	cohortDoubleDemand := map[string]int{}
	resourceDemand := map[string]int{}
	totalDemand := 0
	for _, assignment := range problem.Assignments {
		if !assignmentIsSchedulable(problem, assignment) {
			continue
		}
		if assignment.AcademicYearID != problem.AcademicYearID || assignment.TermID != problem.TermID {
			add(FeasibilityIssue{Code: "ASSIGNMENT_OUTSIDE_ACADEMIC_SCOPE", Constraint: "ACADEMIC_BOUNDARIES", EntityType: "assignment", EntityID: assignment.ID, Message: "Assignment belongs to another academic year or term.", SuggestedAction: "Reconcile the workspace projections for the selected term."})
		}
		teacher, teacherOK := problem.Teachers[assignment.TeacherID]
		cohort, cohortOK := problem.Cohorts[assignment.CohortID]
		if !teacherOK || teacher.WorkspaceID != problem.WorkspaceID {
			add(FeasibilityIssue{Code: "MISSING_ELIGIBLE_TEACHER", Constraint: "ASSIGNMENT_AUTHORITY", EntityType: "assignment", EntityID: assignment.ID, RequiredCapacity: assignment.WeeklyPeriods, Message: "The assignment has no active teacher in this workspace.", SuggestedAction: "Assign an active workspace teacher."})
			continue
		}
		if !cohortOK || cohort.WorkspaceID != problem.WorkspaceID {
			add(FeasibilityIssue{Code: "MISSING_COHORT", Constraint: "MULTI_WORKSPACE_ISOLATION", EntityType: "assignment", EntityID: assignment.ID, Message: "The assignment cohort is not active in this workspace.", SuggestedAction: "Reconcile cohort registrations."})
			continue
		}
		if !problem.Registrations[assignment.CohortID][assignment.CohortSubjectID] {
			add(FeasibilityIssue{Code: "UNREGISTERED_COHORT_SUBJECT", Constraint: "COHORT_SUBJECT_VALIDITY", EntityType: "assignment", EntityID: assignment.ID, Message: "The cohort subject is not registered for this cohort.", SuggestedAction: "Register the subject or remove the teaching assignment."})
		}
		if len(teacher.QualifiedSubjects) > 0 && !teacher.QualifiedSubjects[assignment.SubjectID] {
			add(FeasibilityIssue{Code: "TEACHER_NOT_QUALIFIED", Constraint: "ASSIGNMENT_AUTHORITY", EntityType: "teacher", EntityID: teacher.ID, Message: "The assigned teacher is not qualified for this subject.", SuggestedAction: "Update qualifications or reassign the subject."})
		}
		if assignment.DoubleBlocks*2 > assignment.WeeklyPeriods {
			add(FeasibilityIssue{Code: "INVALID_DOUBLE_DEMAND", Constraint: "DOUBLE_LESSON_CONTIGUITY", EntityType: "assignment", EntityID: assignment.ID, RequiredCapacity: assignment.DoubleBlocks * 2, AvailableCapacity: assignment.WeeklyPeriods, Message: "Double-lesson demand exceeds the weekly lesson requirement.", SuggestedAction: "Reduce required double lessons or increase weekly periods."})
		}
		teacherDemand[assignment.TeacherID] += assignment.WeeklyPeriods
		cohortDemand[assignment.CohortID] += assignment.WeeklyPeriods
		doubleDemand[assignment.TeacherID] += assignment.DoubleBlocks
		cohortDoubleDemand[assignment.CohortID] += assignment.DoubleBlocks
		if assignment.ResourceID != "" {
			resourceDemand[assignment.ResourceID] += assignment.WeeklyPeriods
		}
		totalDemand += assignment.WeeklyPeriods
	}

	availableForTeacher := func(teacher EngineTeacher) int {
		available := 0
		for _, period := range periods {
			if !teacher.Unavailable[period.ID] {
				available++
			}
		}
		return available
	}
	for teacherID, demand := range teacherDemand {
		teacher := problem.Teachers[teacherID]
		available := availableForTeacher(teacher)
		if teacher.WorkloadLimit > 0 && teacher.WorkloadLimit < available {
			available = teacher.WorkloadLimit
		}
		if demand > available {
			add(FeasibilityIssue{Code: "TEACHER_CAPACITY_EXCEEDED", Constraint: "TEACHER_WORKLOAD_LIMITS", EntityType: "teacher", EntityID: teacherID, RequiredCapacity: demand, AvailableCapacity: available, Message: fmt.Sprintf("Teacher needs %d periods but can teach %d.", demand, available), SuggestedAction: "Reassign lessons, increase availability, or adjust workload policy."})
		}
		if doubleDemand[teacherID] > adjacentCapacity(periods, teacher.Unavailable, nil) {
			add(FeasibilityIssue{Code: "INSUFFICIENT_DOUBLE_ADJACENCY", Constraint: "DOUBLE_LESSON_CONTIGUITY", EntityType: "teacher", EntityID: teacherID, RequiredCapacity: doubleDemand[teacherID], AvailableCapacity: adjacentCapacity(periods, teacher.Unavailable, nil), Message: "Teacher availability has too few adjacent period pairs for required double lessons.", SuggestedAction: "Change availability or reduce double-lesson requirements."})
		}
	}
	for cohortID, demand := range cohortDemand {
		cohort := problem.Cohorts[cohortID]
		available := 0
		for _, period := range periods {
			if !cohort.Unavailable[period.ID] {
				available++
			}
		}
		if demand > available {
			add(FeasibilityIssue{Code: "COHORT_CAPACITY_EXCEEDED", Constraint: "COHORT_CAPACITY", EntityType: "cohort", EntityID: cohortID, RequiredCapacity: demand, AvailableCapacity: available, Message: fmt.Sprintf("Cohort needs %d periods but has %d usable periods.", demand, available), SuggestedAction: "Add teaching periods or reduce cohort demand."})
		}
		if cohortDoubleDemand[cohortID] > adjacentCapacity(periods, cohort.Unavailable, nil) {
			add(FeasibilityIssue{Code: "COHORT_DOUBLE_ADJACENCY_EXCEEDED", Constraint: "DOUBLE_LESSON_CONTIGUITY", EntityType: "cohort", EntityID: cohortID, RequiredCapacity: cohortDoubleDemand[cohortID], AvailableCapacity: adjacentCapacity(periods, cohort.Unavailable, nil), Message: "The cohort has too few adjacent period pairs for required double lessons.", SuggestedAction: "Add adjacent teaching periods or reduce double-lesson requirements."})
		}
		if problem.FullCoverage && demand != len(periods) {
			add(FeasibilityIssue{Code: "FULL_COVERAGE_DEMAND_MISMATCH", Constraint: "SIMULTANEOUS_COHORT_COVERAGE", EntityType: "cohort", EntityID: cohortID, RequiredCapacity: len(periods), AvailableCapacity: demand, Message: "Full-coverage policy requires exactly one lesson in every usable period.", SuggestedAction: "Add explicit study/free-period demand or correct lesson requirements."})
		}
	}

	teacherCapacity := 0
	for _, teacher := range problem.Teachers {
		if teacher.WorkspaceID != problem.WorkspaceID {
			continue
		}
		capacity := availableForTeacher(teacher)
		if teacher.WorkloadLimit > 0 && teacher.WorkloadLimit < capacity {
			capacity = teacher.WorkloadLimit
		}
		teacherCapacity += capacity
	}
	if totalDemand > teacherCapacity {
		add(FeasibilityIssue{Code: "TOTAL_TEACHER_CAPACITY_EXCEEDED", Constraint: "TEACHER_PERIOD_CAPACITY", RequiredCapacity: totalDemand, AvailableCapacity: teacherCapacity, Message: "Total lesson demand exceeds available teacher-period capacity.", SuggestedAction: "Add teachers or periods, or reduce demand."})
	}
	for resourceID, demand := range resourceDemand {
		resource, exists := problem.Resources[resourceID]
		if !exists || resource.WorkspaceID != problem.WorkspaceID {
			add(FeasibilityIssue{Code: "MISSING_REQUIRED_RESOURCE", Constraint: "RESOURCE_CAPACITY", EntityType: "resource", EntityID: resourceID, RequiredCapacity: demand, AvailableCapacity: 0, Message: "A required scheduling resource is not active in this workspace.", SuggestedAction: "Enable the resource or remove the requirement."})
			continue
		}
		capacity := resource.Capacity
		if capacity <= 0 {
			capacity = 1
		}
		available := 0
		for _, period := range periods {
			if !resource.Unavailable[period.ID] {
				available += capacity
			}
		}
		if demand > available {
			add(FeasibilityIssue{Code: "RESOURCE_CAPACITY_EXCEEDED", Constraint: "RESOURCE_CAPACITY", EntityType: "resource", EntityID: resourceID, RequiredCapacity: demand, AvailableCapacity: available, Message: "Required resource demand exceeds its available period capacity.", SuggestedAction: "Add equivalent resources, increase availability, or reduce demand."})
		}
	}

	if problem.FullCoverage {
		for _, period := range periods {
			cohortsRequired := 0
			for _, cohort := range problem.Cohorts {
				if cohort.WorkspaceID == problem.WorkspaceID && !cohort.Unavailable[period.ID] {
					cohortsRequired++
				}
			}
			availableTeachers := 0
			for _, teacher := range problem.Teachers {
				if teacher.WorkspaceID == problem.WorkspaceID && !teacher.Unavailable[period.ID] {
					availableTeachers++
				}
			}
			if availableTeachers < cohortsRequired {
				add(FeasibilityIssue{Code: "SIMULTANEOUS_TEACHER_SHORTAGE", Constraint: "DISTINCT_SIMULTANEOUS_TEACHERS", EntityType: "period", EntityID: period.ID, RequiredCapacity: cohortsRequired, AvailableCapacity: availableTeachers, Message: "There are too few distinct available teachers for simultaneous cohort coverage.", SuggestedAction: "Add or free teachers for this period, or configure an explicit combined-session policy."})
			}
			eligibleCoverage := maximumEligibleCoverage(problem, period.ID)
			if eligibleCoverage < cohortsRequired {
				add(FeasibilityIssue{Code: "SIMULTANEOUS_ELIGIBILITY_SHORTAGE", Constraint: "ASSIGNMENT_COMPATIBILITY", EntityType: "period", EntityID: period.ID, RequiredCapacity: cohortsRequired, AvailableCapacity: eligibleCoverage, Message: "Teacher-to-cohort assignment compatibility cannot cover all cohorts simultaneously.", SuggestedAction: "Add compatible teaching assignments or change availability."})
			}
		}
	}

	return FeasibilityReport{Feasible: len(issues) == 0, Issues: issues, Duration: time.Since(started)}
}

func maximumEligibleCoverage(problem EngineProblem, periodID string) int {
	adjacency := map[string][]string{}
	for _, assignment := range problem.Assignments {
		if !assignmentIsSchedulable(problem, assignment) {
			continue
		}
		teacher, teacherOK := problem.Teachers[assignment.TeacherID]
		cohort, cohortOK := problem.Cohorts[assignment.CohortID]
		if !teacherOK || !cohortOK || teacher.Unavailable[periodID] || cohort.Unavailable[periodID] {
			continue
		}
		adjacency[assignment.TeacherID] = append(adjacency[assignment.TeacherID], assignment.CohortID)
	}
	match := map[string]string{}
	var visit func(string, map[string]bool) bool
	visit = func(teacher string, seen map[string]bool) bool {
		for _, cohort := range adjacency[teacher] {
			if seen[cohort] {
				continue
			}
			seen[cohort] = true
			previous, occupied := match[cohort]
			if !occupied || visit(previous, seen) {
				match[cohort] = teacher
				return true
			}
		}
		return false
	}
	for teacher := range adjacency {
		visit(teacher, map[string]bool{})
	}
	return len(match)
}

func adjacentCapacity(periods []EnginePeriod, unavailableA, unavailableB map[string]bool) int {
	count := 0
	for i := 0; i+1 < len(periods); i++ {
		a, b := periods[i], periods[i+1]
		if a.Day != b.Day || b.Index != a.Index+1 || unavailableA[a.ID] || unavailableA[b.ID] || unavailableB[a.ID] || unavailableB[b.ID] {
			continue
		}
		count++
		i++
	}
	return count
}
