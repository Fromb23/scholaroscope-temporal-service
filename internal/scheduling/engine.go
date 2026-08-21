package scheduling

import (
	"math/rand"
	"sort"
	"time"
)

type matchEdge struct {
	teacher    string
	cohort     string
	assignment string
	resource   string
	priority   int
}

type colorEdge struct {
	left       int
	right      int
	assignment string
}

const (
	colorSingles = iota
	colorDoubles
	colorPairedDemand
)

type attemptState struct {
	placements  []EnginePlacement
	remaining   map[string]int
	doubles     map[string]int
	teacherOcc  map[string]map[string]bool
	cohortOcc   map[string]map[string]bool
	resourceOcc map[string]map[string]int
	iterations  int
	timedOut    bool
}

func Solve(problem EngineProblem, config EngineConfig) SolveResult {
	started := time.Now()
	if config.TimeBudget <= 0 {
		config.TimeBudget = 10 * time.Second
	}
	if config.IterationBudget <= 0 {
		config.IterationBudget = 1_000_000
	}
	if config.Restarts <= 0 {
		config.Restarts = 3
	}
	deadline := started.Add(config.TimeBudget)
	feasibility := AnalyzeFeasibility(problem)
	metrics := SolveMetrics{Seed: config.Seed, PreflightDuration: feasibility.Duration}
	if !feasibility.Feasible {
		metrics.SolveDuration = time.Since(started) - feasibility.Duration
		return SolveResult{Status: StatusInfeasible, Feasibility: feasibility, Validation: ValidationReport{Valid: false}, Metrics: metrics}
	}

	var best *attemptState
	var bestValidation ValidationReport
	for restart := 0; restart < config.Restarts; restart++ {
		if time.Now().After(deadline) {
			break
		}
		rng := rand.New(rand.NewSource(config.Seed + int64(restart)*7919))
		attempt := solveAttempt(problem, config, deadline, rng)
		validation := ValidateSchedule(problem, attempt.placements, config)
		metrics.Iterations += attempt.iterations
		metrics.Restarts = restart + 1
		if best == nil || validation.Unscheduled < bestValidation.Unscheduled || (validation.Unscheduled == bestValidation.Unscheduled && validation.HardConflictCount < bestValidation.HardConflictCount) || (validation.Valid && validation.SoftViolationCount < bestValidation.SoftViolationCount) {
			copyAttempt := attempt
			best = &copyAttempt
			bestValidation = validation
		}
		if validation.Valid && validation.SoftViolationCount == 0 {
			break
		}
	}

	metrics.SolveDuration = time.Since(started) - feasibility.Duration
	if best == nil {
		return SolveResult{Status: StatusTimeBudgetExceeded, Feasibility: feasibility, Validation: ValidationReport{Valid: false}, Metrics: metrics}
	}
	metrics.ScheduledPeriods = scheduledPeriodCount(best.placements)
	metrics.Unscheduled = bestValidation.Unscheduled
	metrics.ExistingMoved = countExistingMoved(problem.Existing, best.placements)
	status := StatusPartialDraft
	if bestValidation.Valid {
		status = StatusComplete
		if bestValidation.SoftViolationCount > 0 {
			status = StatusCompleteWithSoftViolations
		}
	} else if time.Now().After(deadline) || best.timedOut {
		status = StatusTimeBudgetExceeded
	}
	return SolveResult{Status: status, Placements: best.placements, Feasibility: feasibility, Validation: bestValidation, Metrics: metrics}
}

func solveAttempt(problem EngineProblem, config EngineConfig, deadline time.Time, rng *rand.Rand) attemptState {
	state := attemptState{remaining: map[string]int{}, doubles: map[string]int{}, teacherOcc: map[string]map[string]bool{}, cohortOcc: map[string]map[string]bool{}, resourceOcc: map[string]map[string]int{}}
	assignments := map[string]EngineAssignment{}
	for _, assignment := range problem.Assignments {
		if assignmentIsSchedulable(problem, assignment) {
			assignments[assignment.ID] = assignment
			state.remaining[assignment.ID] = assignment.WeeklyPeriods
			state.doubles[assignment.ID] = assignment.DoubleBlocks
		}
	}

	// Valid existing sessions are pinned first during incremental repair. They
	// are not trusted blindly: every retained cell goes through occupancy and
	// academic-scope checks.
	if config.Incremental {
		for _, existing := range problem.Existing {
			assignment, ok := assignments[existing.AssignmentID]
			if !ok || len(existing.PeriodIDs) == 0 || state.remaining[assignment.ID] < len(existing.PeriodIDs) || (existing.Double && state.doubles[assignment.ID] == 0) {
				continue
			}
			if canPlace(problem, state, assignment, existing.PeriodIDs) {
				place(&state, problem, assignment, existing.PeriodIDs, existing.Double)
			}
		}
	}

	periods := usablePeriods(problem)
	adjacent := make([][2]EnginePeriod, 0)
	for i := 0; i+1 < len(periods); i++ {
		if periods[i].Day == periods[i+1].Day && periods[i+1].Index == periods[i].Index+1 {
			adjacent = append(adjacent, [2]EnginePeriod{periods[i], periods[i+1]})
			i++
		}
	}
	rng.Shuffle(len(adjacent), func(i, j int) { adjacent[i], adjacent[j] = adjacent[j], adjacent[i] })
	if canUsePairedDemandColoring(assignments, state) {
		colors := exactBipartiteColors(problem, assignments, colorPairedDemand, rng)
		state.iterations += len(colors)
		if state.iterations >= config.IterationBudget || time.Now().After(deadline) {
			state.timedOut = true
			return state
		}
		if len(colors) <= len(adjacent) {
			for color, assignmentIDs := range colors {
				ids := []string{adjacent[color][0].ID, adjacent[color][1].ID}
				for _, assignmentID := range assignmentIDs {
					assignment := assignments[assignmentID]
					if !canPlace(problem, state, assignment, ids) {
						continue
					}
					if state.doubles[assignment.ID] > 0 {
						place(&state, problem, assignment, ids, true)
					} else {
						place(&state, problem, assignment, ids[:1], false)
						place(&state, problem, assignment, ids[1:], false)
					}
				}
			}
		}
	}
	if canUseExactColoring(problem, assignments, state, true) {
		colors := exactBipartiteColors(problem, assignments, colorDoubles, rng)
		state.iterations += len(colors)
		if state.iterations >= config.IterationBudget || time.Now().After(deadline) {
			state.timedOut = true
			return state
		}
		if len(colors) <= len(adjacent) {
			for color, assignmentIDs := range colors {
				ids := []string{adjacent[color][0].ID, adjacent[color][1].ID}
				for _, assignmentID := range assignmentIDs {
					assignment := assignments[assignmentID]
					if canPlace(problem, state, assignment, ids) {
						place(&state, problem, assignment, ids, true)
					}
				}
			}
		}
	}
	for progress := true; progress && hasRemainingDoubles(state.doubles); {
		progress = false
		for _, pair := range adjacent {
			if budgetExceeded(&state, config, deadline) {
				return state
			}
			edges := candidateEdges(problem, assignments, state, []string{pair[0].ID, pair[1].ID}, true, rng)
			matches := maximumMatching(edges, rng)
			for _, edge := range matches {
				assignment := assignments[edge.assignment]
				ids := []string{pair[0].ID, pair[1].ID}
				if state.doubles[assignment.ID] > 0 && canPlace(problem, state, assignment, ids) {
					place(&state, problem, assignment, ids, true)
					progress = true
				}
			}
		}
	}

	// Each period is a bipartite teacher-to-cohort matching. Repeated matchings
	// color the remaining teaching multigraph while enforcing both occupancy
	// dimensions during construction.
	rng.Shuffle(len(periods), func(i, j int) { periods[i], periods[j] = periods[j], periods[i] })
	if canUseExactColoring(problem, assignments, state, false) {
		colors := exactBipartiteColors(problem, assignments, colorSingles, rng)
		state.iterations += len(colors)
		if state.iterations >= config.IterationBudget || time.Now().After(deadline) {
			state.timedOut = true
			return state
		}
		if len(colors) <= len(periods) {
			for color, assignmentIDs := range colors {
				for _, assignmentID := range assignmentIDs {
					assignment := assignments[assignmentID]
					if canPlace(problem, state, assignment, []string{periods[color].ID}) {
						place(&state, problem, assignment, []string{periods[color].ID}, false)
					}
				}
			}
		}
	}
	for progress := true; progress && hasRemaining(state.remaining); {
		progress = false
		for _, period := range periods {
			if budgetExceeded(&state, config, deadline) {
				return state
			}
			edges := candidateEdges(problem, assignments, state, []string{period.ID}, false, rng)
			for _, edge := range maximumMatching(edges, rng) {
				assignment := assignments[edge.assignment]
				if state.remaining[assignment.ID] > 0 && canPlace(problem, state, assignment, []string{period.ID}) {
					place(&state, problem, assignment, []string{period.ID}, false)
					progress = true
				}
			}
		}
	}
	for repairSingles(problem, assignments, &state, periods) {
		if budgetExceeded(&state, config, deadline) {
			return state
		}
	}
	sort.Slice(state.placements, func(i, j int) bool {
		if state.placements[i].PeriodIDs[0] == state.placements[j].PeriodIDs[0] {
			return state.placements[i].AssignmentID < state.placements[j].AssignmentID
		}
		return state.placements[i].PeriodIDs[0] < state.placements[j].PeriodIDs[0]
	})
	return state
}

// exactBipartiteColors implements the constructive theorem behind the core
// solver: pad the teacher/cohort multigraph to a Δ-regular balanced graph, then
// remove one perfect matching per color. Real edges become lesson placements;
// padding edges disappear. It is deterministic for a seed and terminates in Δ
// matching rounds.
func exactBipartiteColors(problem EngineProblem, assignments map[string]EngineAssignment, mode int, rng *rand.Rand) [][]string {
	leftIDs := make([]string, 0, len(problem.Teachers))
	rightIDs := make([]string, 0, len(problem.Cohorts))
	for id, teacher := range problem.Teachers {
		if teacher.WorkspaceID == problem.WorkspaceID {
			leftIDs = append(leftIDs, id)
		}
	}
	for id, cohort := range problem.Cohorts {
		if cohort.WorkspaceID == problem.WorkspaceID {
			rightIDs = append(rightIDs, id)
		}
	}
	sort.Strings(leftIDs)
	sort.Strings(rightIDs)
	n := len(leftIDs)
	if len(rightIDs) > n {
		n = len(rightIDs)
	}
	for len(leftIDs) < n {
		leftIDs = append(leftIDs, "#dummy-left")
	}
	for len(rightIDs) < n {
		rightIDs = append(rightIDs, "#dummy-right")
	}
	leftIndex, rightIndex := map[string]int{}, map[string]int{}
	for index, id := range leftIDs {
		if id != "#dummy-left" {
			leftIndex[id] = index
		}
	}
	for index, id := range rightIDs {
		if id != "#dummy-right" {
			rightIndex[id] = index
		}
	}
	edges := make([]colorEdge, 0)
	leftDegree, rightDegree := make([]int, n), make([]int, n)
	delta := 0
	assignmentIDs := make([]string, 0, len(assignments))
	for assignmentID := range assignments {
		assignmentIDs = append(assignmentIDs, assignmentID)
	}
	sort.Strings(assignmentIDs)
	for _, assignmentID := range assignmentIDs {
		assignment := assignments[assignmentID]
		units := assignment.WeeklyPeriods - assignment.DoubleBlocks*2
		if mode == colorDoubles {
			units = assignment.DoubleBlocks
		} else if mode == colorPairedDemand {
			units = assignment.WeeklyPeriods / 2
		}
		for unit := 0; unit < units; unit++ {
			left, right := leftIndex[assignment.TeacherID], rightIndex[assignment.CohortID]
			edges = append(edges, colorEdge{left: left, right: right, assignment: assignment.ID})
			leftDegree[left]++
			rightDegree[right]++
			if leftDegree[left] > delta {
				delta = leftDegree[left]
			}
			if rightDegree[right] > delta {
				delta = rightDegree[right]
			}
		}
	}
	leftCursor, rightCursor := 0, 0
	for leftCursor < n && rightCursor < n {
		for leftCursor < n && leftDegree[leftCursor] == delta {
			leftCursor++
		}
		for rightCursor < n && rightDegree[rightCursor] == delta {
			rightCursor++
		}
		if leftCursor == n || rightCursor == n {
			break
		}
		amount := delta - leftDegree[leftCursor]
		if rightDeficit := delta - rightDegree[rightCursor]; rightDeficit < amount {
			amount = rightDeficit
		}
		for count := 0; count < amount; count++ {
			edges = append(edges, colorEdge{left: leftCursor, right: rightCursor})
		}
		leftDegree[leftCursor] += amount
		rightDegree[rightCursor] += amount
	}

	active := make([]bool, len(edges))
	for index := range active {
		active[index] = true
	}
	colors := make([][]string, 0, delta)
	for color := 0; color < delta; color++ {
		adjacency := make([][]int, n)
		for edgeIndex, edge := range edges {
			if active[edgeIndex] {
				adjacency[edge.left] = append(adjacency[edge.left], edgeIndex)
			}
		}
		for left := range adjacency {
			rng.Shuffle(len(adjacency[left]), func(i, j int) { adjacency[left][i], adjacency[left][j] = adjacency[left][j], adjacency[left][i] })
		}
		matchRight := make([]int, n)
		for index := range matchRight {
			matchRight[index] = -1
		}
		var visit func(int, []bool) bool
		visit = func(left int, seen []bool) bool {
			for _, edgeIndex := range adjacency[left] {
				right := edges[edgeIndex].right
				if seen[right] {
					continue
				}
				seen[right] = true
				previous := matchRight[right]
				if previous < 0 || visit(edges[previous].left, seen) {
					matchRight[right] = edgeIndex
					return true
				}
			}
			return false
		}
		for left := 0; left < n; left++ {
			visit(left, make([]bool, n))
		}
		assignmentsForColor := []string{}
		for _, edgeIndex := range matchRight {
			if edgeIndex >= 0 {
				active[edgeIndex] = false
				if edges[edgeIndex].assignment != "" {
					assignmentsForColor = append(assignmentsForColor, edges[edgeIndex].assignment)
				}
			}
		}
		colors = append(colors, assignmentsForColor)
	}
	return colors
}

// repairSingles performs a bounded two-lesson augmenting exchange. It is most
// valuable during incremental repair: retain the published timetable, open the
// invalidated cohort cell, and move only the blocking lesson when its teacher
// can use that cell.
func repairSingles(problem EngineProblem, assignments map[string]EngineAssignment, state *attemptState, periods []EnginePeriod) bool {
	assignmentIDs := make([]string, 0, len(state.remaining))
	for assignmentID, remaining := range state.remaining {
		if remaining > 0 && state.doubles[assignmentID] == 0 {
			assignmentIDs = append(assignmentIDs, assignmentID)
		}
	}
	sort.Strings(assignmentIDs)
	for _, assignmentID := range assignmentIDs {
		target := assignments[assignmentID]
		if target.ResourceID != "" {
			continue
		}
		for _, targetPeriod := range periods {
			if state.teacherOcc[target.TeacherID][targetPeriod.ID] || problem.Teachers[target.TeacherID].Unavailable[targetPeriod.ID] {
				continue
			}
			blockingIndex := -1
			for index, placement := range state.placements {
				if !placement.Double && placement.CohortID == target.CohortID && len(placement.PeriodIDs) == 1 && placement.PeriodIDs[0] == targetPeriod.ID {
					blockingIndex = index
					break
				}
			}
			if blockingIndex < 0 {
				continue
			}
			blockingPlacement := state.placements[blockingIndex]
			blocking := assignments[blockingPlacement.AssignmentID]
			if blocking.ResourceID != "" {
				continue
			}
			for _, hole := range periods {
				if state.cohortOcc[target.CohortID][hole.ID] || state.teacherOcc[blocking.TeacherID][hole.ID] || problem.Teachers[blocking.TeacherID].Unavailable[hole.ID] || problem.Cohorts[target.CohortID].Unavailable[hole.ID] {
					continue
				}
				delete(state.teacherOcc[blocking.TeacherID], targetPeriod.ID)
				delete(state.cohortOcc[target.CohortID], targetPeriod.ID)
				state.teacherOcc[blocking.TeacherID][hole.ID] = true
				state.cohortOcc[target.CohortID][hole.ID] = true
				state.placements[blockingIndex].PeriodIDs = []string{hole.ID}
				place(state, problem, target, []string{targetPeriod.ID}, false)
				return true
			}
		}
	}
	return false
}

func canUseExactColoring(problem EngineProblem, assignments map[string]EngineAssignment, state attemptState, doubles bool) bool {
	if len(state.placements) != 0 {
		return false
	}
	for _, assignment := range assignments {
		if doubles {
			if assignment.WeeklyPeriods != assignment.DoubleBlocks*2 {
				return false
			}
		} else if assignment.DoubleBlocks > 0 {
			return false
		}
	}
	return true
}

func canUsePairedDemandColoring(assignments map[string]EngineAssignment, state attemptState) bool {
	if len(state.placements) != 0 {
		return false
	}
	hasRequiredDouble, hasSingleDemand := false, false
	for _, assignment := range assignments {
		if assignment.WeeklyPeriods%2 != 0 {
			return false
		}
		if assignment.DoubleBlocks > 0 {
			hasRequiredDouble = true
		}
		if assignment.WeeklyPeriods > assignment.DoubleBlocks*2 {
			hasSingleDemand = true
		}
	}
	return hasRequiredDouble && hasSingleDemand
}

func candidateEdges(problem EngineProblem, assignments map[string]EngineAssignment, state attemptState, periods []string, doublesOnly bool, rng *rand.Rand) []matchEdge {
	edges := make([]matchEdge, 0, len(assignments))
	for _, assignment := range assignments {
		if doublesOnly {
			if state.doubles[assignment.ID] <= 0 || state.remaining[assignment.ID] < 2 {
				continue
			}
		} else if state.remaining[assignment.ID] <= 0 || state.doubles[assignment.ID] > 0 {
			continue
		}
		if canPlace(problem, state, assignment, periods) {
			edges = append(edges, matchEdge{teacher: assignment.TeacherID, cohort: assignment.CohortID, assignment: assignment.ID, resource: assignment.ResourceID, priority: state.remaining[assignment.ID]*1000 + rng.Intn(997)})
		}
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].priority > edges[j].priority })
	return edges
}

func maximumMatching(edges []matchEdge, rng *rand.Rand) []matchEdge {
	byTeacher := map[string][]matchEdge{}
	teachers := make([]string, 0)
	seen := map[string]bool{}
	for _, edge := range edges {
		byTeacher[edge.teacher] = append(byTeacher[edge.teacher], edge)
		if !seen[edge.teacher] {
			seen[edge.teacher] = true
			teachers = append(teachers, edge.teacher)
		}
	}
	rng.Shuffle(len(teachers), func(i, j int) { teachers[i], teachers[j] = teachers[j], teachers[i] })
	cohortMatch := map[string]matchEdge{}
	var visit func(string, map[string]bool) bool
	visit = func(teacher string, visited map[string]bool) bool {
		for _, edge := range byTeacher[teacher] {
			if visited[edge.cohort] {
				continue
			}
			visited[edge.cohort] = true
			previous, occupied := cohortMatch[edge.cohort]
			if !occupied || visit(previous.teacher, visited) {
				cohortMatch[edge.cohort] = edge
				return true
			}
		}
		return false
	}
	for _, teacher := range teachers {
		visit(teacher, map[string]bool{})
	}
	result := make([]matchEdge, 0, len(cohortMatch))
	for _, edge := range cohortMatch {
		result = append(result, edge)
	}
	return result
}

func canPlace(problem EngineProblem, state attemptState, assignment EngineAssignment, periodIDs []string) bool {
	teacher, teacherOK := problem.Teachers[assignment.TeacherID]
	cohort, cohortOK := problem.Cohorts[assignment.CohortID]
	if !teacherOK || !cohortOK || teacher.WorkspaceID != problem.WorkspaceID || cohort.WorkspaceID != problem.WorkspaceID {
		return false
	}
	periodByID := map[string]EnginePeriod{}
	for _, period := range problem.Periods {
		periodByID[period.ID] = period
	}
	for _, periodID := range periodIDs {
		period, ok := periodByID[periodID]
		if !ok || !period.Teaching || period.Excluded || teacher.Unavailable[periodID] || cohort.Unavailable[periodID] || state.teacherOcc[assignment.TeacherID][periodID] || state.cohortOcc[assignment.CohortID][periodID] {
			return false
		}
		if assignment.ResourceID != "" {
			resource, ok := problem.Resources[assignment.ResourceID]
			capacity := resource.Capacity
			if capacity <= 0 {
				capacity = 1
			}
			if !ok || resource.WorkspaceID != problem.WorkspaceID || resource.Unavailable[periodID] || state.resourceOcc[assignment.ResourceID][periodID] >= capacity {
				return false
			}
		}
	}
	return true
}

func place(state *attemptState, problem EngineProblem, assignment EngineAssignment, periodIDs []string, double bool) {
	if state.teacherOcc[assignment.TeacherID] == nil {
		state.teacherOcc[assignment.TeacherID] = map[string]bool{}
	}
	if state.cohortOcc[assignment.CohortID] == nil {
		state.cohortOcc[assignment.CohortID] = map[string]bool{}
	}
	if assignment.ResourceID != "" && state.resourceOcc[assignment.ResourceID] == nil {
		state.resourceOcc[assignment.ResourceID] = map[string]int{}
	}
	for _, periodID := range periodIDs {
		state.teacherOcc[assignment.TeacherID][periodID] = true
		state.cohortOcc[assignment.CohortID][periodID] = true
		if assignment.ResourceID != "" {
			state.resourceOcc[assignment.ResourceID][periodID]++
		}
	}
	state.remaining[assignment.ID] -= len(periodIDs)
	if double {
		state.doubles[assignment.ID]--
	}
	state.placements = append(state.placements, EnginePlacement{AssignmentID: assignment.ID, WorkspaceID: problem.WorkspaceID, AcademicYearID: problem.AcademicYearID, TermID: problem.TermID, TeacherID: assignment.TeacherID, CohortID: assignment.CohortID, CohortSubjectID: assignment.CohortSubjectID, SubjectID: assignment.SubjectID, ResourceID: assignment.ResourceID, PeriodIDs: append([]string(nil), periodIDs...), Double: double})
}

func budgetExceeded(state *attemptState, config EngineConfig, deadline time.Time) bool {
	state.iterations++
	if state.iterations >= config.IterationBudget || time.Now().After(deadline) {
		state.timedOut = true
		return true
	}
	return false
}

func hasRemaining(values map[string]int) bool {
	for _, value := range values {
		if value > 0 {
			return true
		}
	}
	return false
}
func hasRemainingDoubles(values map[string]int) bool { return hasRemaining(values) }
func scheduledPeriodCount(placements []EnginePlacement) int {
	total := 0
	for _, placement := range placements {
		total += len(placement.PeriodIDs)
	}
	return total
}

func countExistingMoved(existing, generated []EnginePlacement) int {
	generatedKeys := map[string]bool{}
	for _, placement := range generated {
		for _, periodID := range placement.PeriodIDs {
			generatedKeys[placement.AssignmentID+"|"+periodID] = true
		}
	}
	moved := 0
	for _, placement := range existing {
		for _, periodID := range placement.PeriodIDs {
			if !generatedKeys[placement.AssignmentID+"|"+periodID] {
				moved++
			}
		}
	}
	return moved
}
