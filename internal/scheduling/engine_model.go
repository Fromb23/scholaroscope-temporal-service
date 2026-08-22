package scheduling

import "time"

// SolveStatus is terminal. No solve operation is allowed to run without a
// time and iteration budget.
type SolveStatus string

const (
	StatusComplete                   SolveStatus = "COMPLETE"
	StatusCompleteWithSoftViolations SolveStatus = "COMPLETE_WITH_SOFT_VIOLATIONS"
	StatusInfeasible                 SolveStatus = "INFEASIBLE"
	StatusTimeBudgetExceeded         SolveStatus = "TIME_BUDGET_EXCEEDED"
	StatusPartialDraft               SolveStatus = "PARTIAL_DRAFT"
)

type EnginePeriod struct {
	ID        string `json:"id"`
	Day       int    `json:"day"`
	Index     int    `json:"index"`
	Teaching  bool   `json:"teaching"`
	Mandatory bool   `json:"mandatory"`
	Excluded  bool   `json:"excluded"`
}

type EngineTeacher struct {
	ID                string          `json:"id"`
	WorkspaceID       string          `json:"workspace_id"`
	WorkloadLimit     int             `json:"workload_limit"`
	Unavailable       map[string]bool `json:"unavailable,omitempty"`
	Preferred         map[string]bool `json:"preferred,omitempty"`
	QualifiedSubjects map[string]bool `json:"qualified_subjects,omitempty"`
}

type EngineCohort struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	Unavailable map[string]bool `json:"unavailable,omitempty"`
}

type EngineResource struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	Capacity    int             `json:"capacity"`
	Unavailable map[string]bool `json:"unavailable,omitempty"`
}

type EngineCalendarException struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspace_id"`
	AcademicYearID string `json:"academic_year_id"`
	TermID         string `json:"term_id"`
	Kind           string `json:"kind"`
	StartsOn       string `json:"starts_on"`
	EndsOn         string `json:"ends_on"`
	BlocksLearning bool   `json:"blocks_learning"`
}

// EngineAssignment is the normalized curriculum-independent scheduling edge.
// WeeklyPeriods counts occupied timetable cells; DoubleBlocks consumes two of
// those periods per block.
type EngineAssignment struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	AcademicYearID  string `json:"academic_year_id"`
	TermID          string `json:"term_id"`
	TeacherID       string `json:"teacher_id"`
	CohortID        string `json:"cohort_id"`
	CohortSubjectID string `json:"cohort_subject_id"`
	SubjectID       string `json:"subject_id"`
	ResourceID      string `json:"resource_id,omitempty"`
	WeeklyPeriods   int    `json:"weekly_periods"`
	DoubleBlocks    int    `json:"double_blocks"`
	Mandatory       bool   `json:"mandatory"`
	Active          bool   `json:"active"`
}

type EngineProblem struct {
	WorkspaceID        string                     `json:"workspace_id"`
	AcademicYearID     string                     `json:"academic_year_id"`
	TermID             string                     `json:"term_id"`
	Periods            []EnginePeriod             `json:"periods"`
	Teachers           map[string]EngineTeacher   `json:"teachers"`
	Cohorts            map[string]EngineCohort    `json:"cohorts"`
	Resources          map[string]EngineResource  `json:"resources,omitempty"`
	Assignments        []EngineAssignment         `json:"assignments"`
	Registrations      map[string]map[string]bool `json:"registrations"`
	FullCoverage       bool                       `json:"full_coverage"`
	Existing           []EnginePlacement          `json:"existing,omitempty"`
	CalendarExceptions []EngineCalendarException  `json:"calendar_exceptions,omitempty"`
}

type EnginePlacement struct {
	AssignmentID    string   `json:"assignment_id"`
	WorkspaceID     string   `json:"workspace_id"`
	AcademicYearID  string   `json:"academic_year_id"`
	TermID          string   `json:"term_id"`
	TeacherID       string   `json:"teacher_id"`
	CohortID        string   `json:"cohort_id"`
	CohortSubjectID string   `json:"cohort_subject_id"`
	SubjectID       string   `json:"subject_id"`
	ResourceID      string   `json:"resource_id,omitempty"`
	PeriodIDs       []string `json:"period_ids"`
	Double          bool     `json:"double"`
}

type EngineConfig struct {
	Seed            int64         `json:"seed"`
	TimeBudget      time.Duration `json:"time_budget"`
	IterationBudget int           `json:"iteration_budget"`
	Restarts        int           `json:"restarts"`
	MaxConsecutive  int           `json:"max_consecutive"`
	MaxIdleGaps     int           `json:"max_idle_gaps"`
	Incremental     bool          `json:"incremental"`
}

type FeasibilityIssue struct {
	Code              string         `json:"code"`
	Constraint        string         `json:"constraint"`
	EntityType        string         `json:"entity_type,omitempty"`
	EntityID          string         `json:"entity_id,omitempty"`
	RequiredCapacity  int            `json:"required_capacity"`
	AvailableCapacity int            `json:"available_capacity"`
	Message           string         `json:"message"`
	SuggestedAction   string         `json:"suggested_action"`
	Details           map[string]any `json:"details,omitempty"`
}

type FeasibilityReport struct {
	Feasible bool               `json:"feasible"`
	Issues   []FeasibilityIssue `json:"issues"`
	Duration time.Duration      `json:"duration"`
}

type FairnessMetrics struct {
	TeacherWorkloadVariance float64 `json:"teacher_workload_variance"`
	DailyLoadVariance       float64 `json:"daily_load_variance"`
	RepeatedSubjectClusters int     `json:"repeated_subject_clusters"`
	IdleGaps                int     `json:"idle_gaps"`
	ConsecutiveOverloads    int     `json:"consecutive_overloads"`
}

type InvariantResult struct {
	Invariant   string `json:"invariant"`
	Passed      bool   `json:"passed"`
	WorkspaceID string `json:"workspace_id"`
	EntityType  string `json:"entity_type,omitempty"`
	EntityID    string `json:"entity_id,omitempty"`
	PeriodID    string `json:"period_id,omitempty"`
	Observed    any    `json:"observed,omitempty"`
	Expected    any    `json:"expected,omitempty"`
	Explanation string `json:"explanation"`
}

type ValidationReport struct {
	Valid              bool              `json:"valid"`
	HardConflictCount  int               `json:"hard_conflict_count"`
	SoftViolationCount int               `json:"soft_violation_count"`
	Unscheduled        int               `json:"unscheduled_mandatory_lessons"`
	Results            []InvariantResult `json:"results"`
	Fairness           FairnessMetrics   `json:"fairness"`
}

type SolveMetrics struct {
	Seed              int64         `json:"seed"`
	PreflightDuration time.Duration `json:"preflight_duration"`
	SolveDuration     time.Duration `json:"solve_duration"`
	Iterations        int           `json:"iterations"`
	Restarts          int           `json:"restarts"`
	ScheduledPeriods  int           `json:"scheduled_periods"`
	Unscheduled       int           `json:"unscheduled_periods"`
	ExistingMoved     int           `json:"existing_sessions_moved"`
}

type SolveResult struct {
	Status      SolveStatus       `json:"status"`
	Placements  []EnginePlacement `json:"placements"`
	Feasibility FeasibilityReport `json:"feasibility"`
	Validation  ValidationReport  `json:"validation"`
	Metrics     SolveMetrics      `json:"metrics"`
}
