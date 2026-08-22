package scheduling

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"
)

type audienceTask struct {
	ID              string
	DeliveryGroupID string
	ParallelBlockID string
	AssignmentIDs   []string
	TeacherID       string
	CohortIDs       []string
	CohortSubjectID string
	SubjectID       string
	ResourceID      string
	LearnerIDs      []string
	Remaining       int
	Doubles         int
	Mandatory       bool
}

func solveAudienceAttempt(problem EngineProblem, config EngineConfig, deadline time.Time, rng *rand.Rand) attemptState {
	state := attemptState{
		remaining:   map[string]int{},
		doubles:     map[string]int{},
		teacherOcc:  map[string]map[string]bool{},
		cohortOcc:   map[string]map[string]bool{},
		learnerOcc:  map[string]map[string]bool{},
		groupOcc:    map[string]map[string]bool{},
		resourceOcc: map[string]map[string]int{},
	}
	tasks, tasksByGroup := audienceTasks(problem)
	for _, task := range tasks {
		state.remaining[task.ID] = task.Remaining
		state.doubles[task.ID] = task.Doubles
	}

	periods := usablePeriods(problem)
	adjacent := adjacentPeriodPairs(periods)
	blocks := append([]EngineParallelBlock(nil), problem.ParallelBlocks...)
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].ID < blocks[j].ID })
	for _, block := range blocks {
		if !block.Active || block.WorkspaceID != problem.WorkspaceID || len(block.GroupIDs) < 2 {
			continue
		}
		members := make([]*audienceTask, 0, len(block.GroupIDs))
		for _, groupID := range block.GroupIDs {
			if task := tasksByGroup[groupID]; task != nil {
				task.ParallelBlockID = block.ID
				members = append(members, task)
			}
		}
		if len(members) < 2 {
			continue
		}
		for blockHasRemaining(members) {
			if budgetExceeded(&state, config, deadline) {
				return state
			}
			double := blockNeedsDouble(members)
			if double {
				if !placeParallelMembers(problem, &state, members, pairsAsIDs(adjacent), true, rng) {
					break
				}
			} else if !placeParallelMembers(problem, &state, members, singlesAsIDs(periods), false, rng) {
				break
			}
		}
	}

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].DeliveryGroupID == "" && tasks[j].DeliveryGroupID != "" {
			return false
		}
		if tasks[i].DeliveryGroupID != "" && tasks[j].DeliveryGroupID == "" {
			return true
		}
		if len(tasks[i].AssignmentIDs) != len(tasks[j].AssignmentIDs) {
			return len(tasks[i].AssignmentIDs) > len(tasks[j].AssignmentIDs)
		}
		return tasks[i].ID < tasks[j].ID
	})
	for progress := true; progress && audienceHasRemaining(state.remaining); {
		progress = false
		for index := range tasks {
			task := &tasks[index]
			for state.doubles[task.ID] > 0 && state.remaining[task.ID] >= 2 {
				if budgetExceeded(&state, config, deadline) {
					return state
				}
				if !placeFirstTask(problem, &state, task, pairsAsIDs(adjacent), true, rng) {
					break
				}
				progress = true
			}
			for state.remaining[task.ID] > 0 && state.doubles[task.ID] == 0 {
				if budgetExceeded(&state, config, deadline) {
					return state
				}
				if !placeFirstTask(problem, &state, task, singlesAsIDs(periods), false, rng) {
					break
				}
				progress = true
			}
		}
	}
	sort.Slice(state.placements, func(i, j int) bool {
		left, right := "", ""
		if len(state.placements[i].PeriodIDs) > 0 {
			left = state.placements[i].PeriodIDs[0]
		}
		if len(state.placements[j].PeriodIDs) > 0 {
			right = state.placements[j].PeriodIDs[0]
		}
		if left == right {
			return state.placements[i].AssignmentID < state.placements[j].AssignmentID
		}
		return left < right
	})
	return state
}

func audienceTasks(problem EngineProblem) ([]audienceTask, map[string]*audienceTask) {
	assignments := map[string]EngineAssignment{}
	remaining := map[string]int{}
	doubles := map[string]int{}
	for _, assignment := range problem.Assignments {
		if assignmentIsSchedulable(problem, assignment) {
			assignments[assignment.ID] = assignment
			remaining[assignment.ID] = assignment.WeeklyPeriods
			doubles[assignment.ID] = assignment.DoubleBlocks
		}
	}
	tasks := []audienceTask{}
	for _, group := range problem.DeliveryGroups {
		if !group.Active || group.WorkspaceID != problem.WorkspaceID || group.WeeklyPeriods <= 0 {
			continue
		}
		assignmentIDs := uniqueStrings(group.AssignmentIDs)
		if len(assignmentIDs) == 0 {
			continue
		}
		for _, assignmentID := range assignmentIDs {
			remaining[assignmentID] -= group.WeeklyPeriods
			if group.DoubleBlocks > 0 {
				doubles[assignmentID] -= group.DoubleBlocks
			}
		}
		tasks = append(tasks, audienceTask{
			ID:              "group:" + group.ID,
			DeliveryGroupID: group.ID,
			AssignmentIDs:   assignmentIDs,
			TeacherID:       group.TeacherID,
			CohortIDs:       uniqueStrings(group.CohortIDs),
			SubjectID:       group.SubjectID,
			ResourceID:      group.ResourceID,
			LearnerIDs:      uniqueStrings(group.LearnerIDs),
			Remaining:       group.WeeklyPeriods,
			Doubles:         group.DoubleBlocks,
			Mandatory:       group.Mandatory,
		})
	}
	for _, assignment := range assignments {
		left := remaining[assignment.ID]
		if left <= 0 {
			continue
		}
		doubleLeft := doubles[assignment.ID]
		if doubleLeft < 0 {
			doubleLeft = 0
		}
		if doubleLeft*2 > left {
			doubleLeft = left / 2
		}
		tasks = append(tasks, audienceTask{
			ID:              "assignment:" + assignment.ID,
			AssignmentIDs:   []string{assignment.ID},
			TeacherID:       assignment.TeacherID,
			CohortIDs:       []string{assignment.CohortID},
			CohortSubjectID: assignment.CohortSubjectID,
			SubjectID:       assignment.SubjectID,
			ResourceID:      assignment.ResourceID,
			Remaining:       left,
			Doubles:         doubleLeft,
			Mandatory:       assignment.Mandatory,
		})
	}
	byGroup := map[string]*audienceTask{}
	for index := range tasks {
		if tasks[index].DeliveryGroupID != "" {
			byGroup[tasks[index].DeliveryGroupID] = &tasks[index]
		}
	}
	return tasks, byGroup
}

func adjacentPeriodPairs(periods []EnginePeriod) [][2]EnginePeriod {
	pairs := make([][2]EnginePeriod, 0)
	for i := 0; i+1 < len(periods); i++ {
		if periods[i].Day == periods[i+1].Day && periods[i+1].Index == periods[i].Index+1 {
			pairs = append(pairs, [2]EnginePeriod{periods[i], periods[i+1]})
			i++
		}
	}
	return pairs
}

func singlesAsIDs(periods []EnginePeriod) [][]string {
	result := make([][]string, 0, len(periods))
	for _, period := range periods {
		result = append(result, []string{period.ID})
	}
	return result
}

func pairsAsIDs(pairs [][2]EnginePeriod) [][]string {
	result := make([][]string, 0, len(pairs))
	for _, pair := range pairs {
		result = append(result, []string{pair[0].ID, pair[1].ID})
	}
	return result
}

func placeParallelMembers(problem EngineProblem, state *attemptState, members []*audienceTask, candidates [][]string, double bool, rng *rand.Rand) bool {
	order := append([][]string(nil), candidates...)
	rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	for _, periodIDs := range order {
		ok := true
		for _, task := range members {
			if state.remaining[task.ID] <= 0 || (double && state.doubles[task.ID] <= 0) || !canPlaceTask(problem, *state, *task, periodIDs) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		for _, task := range members {
			placeTask(state, problem, *task, periodIDs, double)
			task.Remaining -= len(periodIDs)
			if double {
				task.Doubles--
			}
		}
		return true
	}
	return false
}

func placeFirstTask(problem EngineProblem, state *attemptState, task *audienceTask, candidates [][]string, double bool, rng *rand.Rand) bool {
	order := append([][]string(nil), candidates...)
	rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	for _, periodIDs := range order {
		if canPlaceTask(problem, *state, *task, periodIDs) {
			placeTask(state, problem, *task, periodIDs, double)
			task.Remaining -= len(periodIDs)
			if double {
				task.Doubles--
			}
			return true
		}
	}
	return false
}

func canPlaceTask(problem EngineProblem, state attemptState, task audienceTask, periodIDs []string) bool {
	teacher, teacherOK := problem.Teachers[task.TeacherID]
	if !teacherOK || teacher.WorkspaceID != problem.WorkspaceID {
		return false
	}
	for _, cohortID := range task.CohortIDs {
		cohort, ok := problem.Cohorts[cohortID]
		if !ok || cohort.WorkspaceID != problem.WorkspaceID {
			return false
		}
	}
	periodByID := map[string]EnginePeriod{}
	for _, period := range problem.Periods {
		periodByID[period.ID] = period
	}
	for _, learnerID := range task.LearnerIDs {
		learner, ok := problem.Learners[learnerID]
		if !ok || learner.WorkspaceID != problem.WorkspaceID || !learner.Active {
			return false
		}
	}
	for _, periodID := range periodIDs {
		period, ok := periodByID[periodID]
		if !ok || !period.Teaching || period.Excluded || teacher.Unavailable[periodID] || state.teacherOcc[task.TeacherID][periodID] {
			return false
		}
		if task.DeliveryGroupID != "" && state.groupOcc[task.DeliveryGroupID][periodID] {
			return false
		}
		for _, cohortID := range task.CohortIDs {
			if problem.Cohorts[cohortID].Unavailable[periodID] {
				return false
			}
			if len(task.LearnerIDs) == 0 {
				if state.cohortOcc[cohortID][periodID] || cohortHasLearnerOccupancy(problem, state, cohortID, periodID) {
					return false
				}
			} else if state.cohortOcc[cohortID][periodID] {
				return false
			}
		}
		for _, learnerID := range task.LearnerIDs {
			if state.learnerOcc[learnerID][periodID] {
				return false
			}
		}
		if task.ResourceID != "" {
			resource, ok := problem.Resources[task.ResourceID]
			capacity := resource.Capacity
			if capacity <= 0 {
				capacity = 1
			}
			if !ok || resource.WorkspaceID != problem.WorkspaceID || resource.Unavailable[periodID] || state.resourceOcc[task.ResourceID][periodID] >= capacity {
				return false
			}
		}
	}
	return true
}

func placeTask(state *attemptState, problem EngineProblem, task audienceTask, periodIDs []string, double bool) {
	if state.teacherOcc[task.TeacherID] == nil {
		state.teacherOcc[task.TeacherID] = map[string]bool{}
	}
	if task.DeliveryGroupID != "" && state.groupOcc[task.DeliveryGroupID] == nil {
		state.groupOcc[task.DeliveryGroupID] = map[string]bool{}
	}
	for _, cohortID := range task.CohortIDs {
		if state.cohortOcc[cohortID] == nil {
			state.cohortOcc[cohortID] = map[string]bool{}
		}
	}
	for _, learnerID := range task.LearnerIDs {
		if state.learnerOcc[learnerID] == nil {
			state.learnerOcc[learnerID] = map[string]bool{}
		}
	}
	if task.ResourceID != "" && state.resourceOcc[task.ResourceID] == nil {
		state.resourceOcc[task.ResourceID] = map[string]int{}
	}
	for _, periodID := range periodIDs {
		state.teacherOcc[task.TeacherID][periodID] = true
		if task.DeliveryGroupID != "" {
			state.groupOcc[task.DeliveryGroupID][periodID] = true
		}
		if len(task.LearnerIDs) == 0 {
			for _, cohortID := range task.CohortIDs {
				state.cohortOcc[cohortID][periodID] = true
			}
		} else {
			for _, learnerID := range task.LearnerIDs {
				state.learnerOcc[learnerID][periodID] = true
			}
		}
		if task.ResourceID != "" {
			state.resourceOcc[task.ResourceID][periodID]++
		}
	}
	state.remaining[task.ID] -= len(periodIDs)
	if double {
		state.doubles[task.ID]--
	}
	assignmentID := ""
	if len(task.AssignmentIDs) > 0 {
		assignmentID = task.AssignmentIDs[0]
	}
	cohortID := ""
	if len(task.CohortIDs) > 0 {
		cohortID = task.CohortIDs[0]
	}
	state.placements = append(state.placements, EnginePlacement{
		AssignmentID:     assignmentID,
		AssignmentIDs:    append([]string(nil), task.AssignmentIDs...),
		DeliveryGroupID:  task.DeliveryGroupID,
		ParallelBlockID:  task.ParallelBlockID,
		WorkspaceID:      problem.WorkspaceID,
		AcademicYearID:   problem.AcademicYearID,
		TermID:           problem.TermID,
		TeacherID:        task.TeacherID,
		CohortID:         cohortID,
		CohortIDs:        append([]string(nil), task.CohortIDs...),
		CohortSubjectID:  task.CohortSubjectID,
		SubjectID:        task.SubjectID,
		LearnerIDs:       append([]string(nil), task.LearnerIDs...),
		ResourceID:       task.ResourceID,
		PeriodIDs:        append([]string(nil), periodIDs...),
		Double:           double,
	})
}

func cohortHasLearnerOccupancy(problem EngineProblem, state attemptState, cohortID, periodID string) bool {
	for learnerID, learner := range problem.Learners {
		if learner.CohortID == cohortID && state.learnerOcc[learnerID][periodID] {
			return true
		}
	}
	return false
}

func blockHasRemaining(tasks []*audienceTask) bool {
	for _, task := range tasks {
		if task != nil && task.Remaining > 0 {
			return true
		}
	}
	return false
}

func blockNeedsDouble(tasks []*audienceTask) bool {
	for _, task := range tasks {
		if task != nil && task.Doubles > 0 {
			return true
		}
	}
	return false
}

func audienceHasRemaining(values map[string]int) bool {
	for _, value := range values {
		if value > 0 {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func analyzeAudienceFeasibility(problem EngineProblem, periods []EnginePeriod, issues []FeasibilityIssue, started time.Time) FeasibilityReport {
	add := func(issue FeasibilityIssue) { issues = append(issues, issue) }
	if len(periods) == 0 {
		return FeasibilityReport{Feasible: len(issues) == 0, Issues: issues, Duration: time.Since(started)}
	}
	assignments := map[string]EngineAssignment{}
	sharedCredit := map[string]int{}
	for _, assignment := range problem.Assignments {
		if assignmentIsSchedulable(problem, assignment) {
			assignments[assignment.ID] = assignment
		}
	}
	for _, group := range problem.DeliveryGroups {
		if !group.Active || group.WorkspaceID != problem.WorkspaceID {
			continue
		}
		if group.WeeklyPeriods <= 0 {
			add(FeasibilityIssue{Code: "INVALID_DELIVERY_GROUP_DEMAND", Constraint: "COMPLETE_DEMAND_SATISFACTION", EntityType: "delivery_group", EntityID: group.ID, Message: "Shared lessons per week must be greater than zero.", SuggestedAction: "Set shared lessons or remove the group."})
		}
		if group.DoubleBlocks*2 > group.WeeklyPeriods {
			add(FeasibilityIssue{Code: "INVALID_DOUBLE_DEMAND", Constraint: "DOUBLE_LESSON_CONTIGUITY", EntityType: "delivery_group", EntityID: group.ID, RequiredCapacity: group.DoubleBlocks * 2, AvailableCapacity: group.WeeklyPeriods, Message: "A double lesson uses two periods. Reduce the number of double lessons or increase weekly periods.", SuggestedAction: "Reduce required double lessons or increase weekly periods."})
		}
		teacher, teacherOK := problem.Teachers[group.TeacherID]
		if !teacherOK || teacher.WorkspaceID != problem.WorkspaceID {
			add(FeasibilityIssue{Code: "MISSING_ELIGIBLE_TEACHER", Constraint: "ASSIGNMENT_AUTHORITY", EntityType: "delivery_group", EntityID: group.ID, Message: "The delivery group has no active teacher in this workspace.", SuggestedAction: "Correct the teacher assignment in Scholaroscope."})
		}
		for _, learnerID := range uniqueStrings(group.LearnerIDs) {
			learner, learnerOK := problem.Learners[learnerID]
			if !learnerOK || learner.WorkspaceID != problem.WorkspaceID || !learner.Active || !containsString(group.CohortIDs, learner.CohortID) {
				add(FeasibilityIssue{Code: "INELIGIBLE_DELIVERY_GROUP_LEARNER", Constraint: "ACTIVE_LEARNER_ELIGIBILITY", EntityType: "learner", EntityID: learnerID, Message: "A delivery group contains a learner who is not active in one of its included classes.", SuggestedAction: "Refresh Scholaroscope learner participation and rebuild the group."})
			}
		}
		for _, assignmentID := range uniqueStrings(group.AssignmentIDs) {
			assignment, ok := assignments[assignmentID]
			if !ok {
				add(FeasibilityIssue{Code: "DELIVERY_GROUP_ASSIGNMENT_MISSING", Constraint: "ASSIGNMENT_AUTHORITY", EntityType: "assignment", EntityID: assignmentID, Message: "A delivery group references an inactive teaching assignment.", SuggestedAction: "Remove the group or refresh Scholaroscope assignments."})
				continue
			}
			if assignment.TeacherID != group.TeacherID || assignment.SubjectID != group.SubjectID {
				add(FeasibilityIssue{Code: "DELIVERY_GROUP_TEACHER_MISMATCH", Constraint: "ASSIGNMENT_AUTHORITY", EntityType: "assignment", EntityID: assignmentID, Message: "Merged lessons require the same authorized teacher and subject for every included class subject.", SuggestedAction: "Correct the teaching assignments in Scholaroscope before combining classes."})
			}
			if !containsString(group.CohortIDs, assignment.CohortID) {
				add(FeasibilityIssue{Code: "DELIVERY_GROUP_COHORT_MISMATCH", Constraint: "ACTIVE_LEARNER_ELIGIBILITY", EntityType: "delivery_group", EntityID: group.ID, Message: "A delivery group does not include one of the assignment classes.", SuggestedAction: "Recreate the group from compatible class assignments."})
			}
			sharedCredit[assignmentID] += group.WeeklyPeriods
		}
	}
	for assignmentID, credit := range sharedCredit {
		if assignment, ok := assignments[assignmentID]; ok && credit > assignment.WeeklyPeriods {
			add(FeasibilityIssue{Code: "MERGED_DEMAND_EXCEEDS_ASSIGNMENT", Constraint: "COMPLETE_DEMAND_SATISFACTION", EntityType: "assignment", EntityID: assignmentID, RequiredCapacity: credit, AvailableCapacity: assignment.WeeklyPeriods, Message: "Shared lessons exceed this assignment's weekly lesson requirement.", SuggestedAction: "Reduce shared lessons per week or increase the Scholaroscope lesson requirement."})
		}
	}
	tasks, tasksByGroup := audienceTasks(problem)
	teacherDemand := map[string]int{}
	resourceDemand := map[string]int{}
	for _, task := range tasks {
		if task.Remaining <= 0 {
			continue
		}
		teacherDemand[task.TeacherID] += task.Remaining
		if task.ResourceID != "" {
			resourceDemand[task.ResourceID] += task.Remaining
		}
		if task.Doubles*2 > task.Remaining {
			add(FeasibilityIssue{Code: "INVALID_DOUBLE_DEMAND", Constraint: "DOUBLE_LESSON_CONTIGUITY", EntityType: "assignment", EntityID: task.ID, RequiredCapacity: task.Doubles * 2, AvailableCapacity: task.Remaining, Message: "A double lesson uses two periods. Reduce the number of double lessons or increase weekly periods.", SuggestedAction: "Reduce required double lessons or increase weekly periods."})
		}
		for _, assignmentID := range task.AssignmentIDs {
			assignment, ok := assignments[assignmentID]
			if !ok {
				continue
			}
			if assignment.AcademicYearID != problem.AcademicYearID || assignment.TermID != problem.TermID {
				add(FeasibilityIssue{Code: "ASSIGNMENT_OUTSIDE_ACADEMIC_SCOPE", Constraint: "ACADEMIC_BOUNDARIES", EntityType: "assignment", EntityID: assignmentID, Message: "Assignment belongs to another academic year or term.", SuggestedAction: "Reconcile the workspace projections for the selected term."})
			}
			if !problem.Registrations[assignment.CohortID][assignment.CohortSubjectID] {
				add(FeasibilityIssue{Code: "UNREGISTERED_COHORT_SUBJECT", Constraint: "COHORT_SUBJECT_VALIDITY", EntityType: "assignment", EntityID: assignmentID, Message: "The cohort subject is not registered for this class.", SuggestedAction: "Register the subject or remove the teaching assignment in Scholaroscope."})
			}
		}
	}
	for teacherID, demand := range teacherDemand {
		teacher := problem.Teachers[teacherID]
		available := 0
		for _, period := range periods {
			if !teacher.Unavailable[period.ID] {
				available++
			}
		}
		if teacher.WorkloadLimit > 0 && teacher.WorkloadLimit < available {
			available = teacher.WorkloadLimit
		}
		if demand > available {
			add(FeasibilityIssue{Code: "TEACHER_CAPACITY_EXCEEDED", Constraint: "TEACHER_WORKLOAD_LIMITS", EntityType: "teacher", EntityID: teacherID, RequiredCapacity: demand, AvailableCapacity: available, Message: fmt.Sprintf("Teacher needs %d periods but can teach %d.", demand, available), SuggestedAction: "Reassign lessons, increase availability, or adjust workload policy."})
		}
	}
	for resourceID, demand := range resourceDemand {
		resource, ok := problem.Resources[resourceID]
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
		if !ok || resource.WorkspaceID != problem.WorkspaceID || demand > available {
			add(FeasibilityIssue{Code: "RESOURCE_CAPACITY_EXCEEDED", Constraint: "RESOURCE_CAPACITY", EntityType: "resource", EntityID: resourceID, RequiredCapacity: demand, AvailableCapacity: available, Message: "Required resource demand exceeds available period capacity.", SuggestedAction: "Add equivalent resources, increase availability, or reduce demand."})
		}
	}
	for _, block := range problem.ParallelBlocks {
		if !block.Active || block.WorkspaceID != problem.WorkspaceID {
			continue
		}
		if len(block.GroupIDs) < 2 {
			add(FeasibilityIssue{Code: "PARALLEL_BLOCK_TOO_SMALL", Constraint: "PARALLEL_BLOCK_SIMULTANEITY", EntityType: "parallel_block", EntityID: block.ID, Message: "Alternative-subject blocks must contain at least two groups.", SuggestedAction: "Add another group or remove the block."})
		}
		seenLearners := map[string]string{}
		for _, groupID := range block.GroupIDs {
			task := tasksByGroup[groupID]
			if task == nil {
				add(FeasibilityIssue{Code: "PARALLEL_BLOCK_GROUP_MISSING", Constraint: "PARALLEL_BLOCK_SIMULTANEITY", EntityType: "parallel_block", EntityID: block.ID, Message: "A parallel block references a missing delivery group.", SuggestedAction: "Remove the missing group from the block."})
				continue
			}
			for _, learnerID := range task.LearnerIDs {
				if previous := seenLearners[learnerID]; previous != "" {
					add(FeasibilityIssue{Code: "LEARNER_AUDIENCE_OVERLAP", Constraint: "NO_LEARNER_DOUBLE_BOOKING", EntityType: "learner", EntityID: learnerID, Message: "One learner appears in two alternative groups that must run at the same time.", SuggestedAction: "Correct the learner subject selection in Scholaroscope or edit the alternative groups.", Details: map[string]any{"first_group": previous, "second_group": groupID}})
				}
				seenLearners[learnerID] = groupID
			}
		}
	}
	if problem.FullCoverage && len(problem.Learners) == 0 {
		add(FeasibilityIssue{Code: "LEARNER_COVERAGE_DATA_REQUIRED", Constraint: "SIMULTANEOUS_COHORT_COVERAGE", Message: "Full coverage requires synchronized learner enrolment data.", SuggestedAction: "Refresh Scholaroscope learner participation before generating a full-coverage timetable."})
	}
	return FeasibilityReport{Feasible: len(issues) == 0, Issues: issues, Duration: time.Since(started)}
}
