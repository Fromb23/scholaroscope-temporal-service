package provisioning

import "github.com/google/uuid"

type BootstrapPayload struct {
	ScholaroscopeWorkspaceRef    string              `json:"scholaroscope_workspace_ref"`
	ScholaroscopeOrganizationRef string              `json:"scholaroscope_organization_ref"`
	DisplayName                  string              `json:"display_name"`
	Timezone                     string              `json:"timezone"`
	PluginInstallationRef        string              `json:"plugin_installation_ref"`
	SigningKeyID                 string              `json:"signing_key_id"`
	SigningSecret                string              `json:"signing_secret"`
	CallbackURL                  string              `json:"callback_url"`
	AcademicSync                 AcademicSyncPayload `json:"academic_sync"`
}

type AcademicSyncPayload struct {
	Actors              []ActorProjection              `json:"actors"`
	AcademicYears       []AcademicYearProjection       `json:"academic_years"`
	Terms               []TermProjection               `json:"terms"`
	CalendarEvents      []CalendarEventProjection      `json:"calendar_events"`
	Cohorts             []CohortProjection             `json:"cohorts"`
	Subjects            []SubjectProjection            `json:"subjects"`
	CohortSubjects      []CohortSubjectProjection      `json:"cohort_subjects"`
	Learners            []LearnerProjection            `json:"learners"`
	LearnerMemberships  []LearnerCohortMembershipProjection `json:"learner_cohort_memberships"`
	LearnerEnrollments  []LearnerSubjectEnrollmentProjection `json:"learner_subject_enrollments"`
	TeachingAssignments []TeachingAssignmentProjection `json:"teaching_assignments"`
	AssignmentReadiness AssignmentReadinessProjection  `json:"assignment_readiness"`
	LearnerReadiness    LearnerReadinessProjection     `json:"learner_readiness"`
}

type AssignmentReadinessProjection struct {
	SourceAssignmentCount   int `json:"source_assignment_count"`
	EligibleAssignmentCount int `json:"eligible_assignment_count"`
}

type LearnerReadinessProjection struct {
	SourceLearnerCount      int `json:"source_learner_count"`
	EligibleLearnerCount    int `json:"eligible_learner_count"`
	SubjectEnrollmentCount  int `json:"subject_enrollment_count"`
}

type AcademicYearProjection struct {
	AcademicYearRef  string `json:"academic_year_ref"`
	AcademicYearUUID string `json:"academic_year_uuid"`
	WorkspaceRef     string `json:"workspace_ref"`
	Name             string `json:"name"`
	StartDate        string `json:"start_date"`
	EndDate          string `json:"end_date"`
	IsCurrent        bool   `json:"is_current"`
	Status           string `json:"status"`
	CurriculumRef    string `json:"curriculum_ref"`
	CurriculumName   string `json:"curriculum_name"`
}

type ActorProjection struct {
	UserRef     string   `json:"user_ref"`
	ActorUUID   string   `json:"actor_uuid"`
	DisplayName string   `json:"display_name"`
	Email       string   `json:"email"`
	ActorKinds  []string `json:"actor_kinds"`
	Status      string   `json:"status"`
}

type TermProjection struct {
	TermRef           string `json:"term_ref"`
	TermUUID          string `json:"term_uuid"`
	AcademicYearRef   string `json:"academic_year_ref"`
	AcademicYearUUID  string `json:"academic_year_uuid"`
	Name              string `json:"name"`
	AcademicYearLabel string `json:"academic_year_label"`
	StartDate         string `json:"start_date"`
	EndDate           string `json:"end_date"`
	Status            string `json:"status"`
	CalendarReady     bool   `json:"calendar_ready"`
	IsFrozen          bool   `json:"is_frozen"`
}

type CalendarEventProjection struct {
	EventRef         string `json:"event_ref"`
	EventUUID        string `json:"event_uuid"`
	AcademicYearRef  string `json:"academic_year_ref"`
	AcademicYearUUID string `json:"academic_year_uuid"`
	TermRef          string `json:"term_ref"`
	TermUUID         string `json:"term_uuid"`
	Title            string `json:"title"`
	EventType        string `json:"event_type"`
	StartDate        string `json:"start_date"`
	EndDate          string `json:"end_date"`
	AffectsLearning  bool   `json:"affects_learning"`
	Source           string `json:"source"`
}

type CohortProjection struct {
	CohortRef        string `json:"cohort_ref"`
	CohortUUID       string `json:"cohort_uuid"`
	Name             string `json:"name"`
	Level            string `json:"level"`
	Stream           string `json:"stream"`
	AcademicYearRef  string `json:"academic_year_ref"`
	AcademicYearUUID string `json:"academic_year_uuid"`
	EnrollmentCount  int    `json:"enrollment_count"`
	Status           string `json:"status"`
}

type SubjectProjection struct {
	SubjectRef  string `json:"subject_ref"`
	SubjectUUID string `json:"subject_uuid"`
	Name        string `json:"name"`
	Code        string `json:"code"`
	Level       string `json:"level"`
	Status      string `json:"status"`
}

type CohortSubjectProjection struct {
	CohortSubjectRef  string `json:"cohort_subject_ref"`
	CohortSubjectUUID string `json:"cohort_subject_uuid"`
	CohortUUID        string `json:"cohort_uuid"`
	SubjectUUID       string `json:"subject_uuid"`
	CohortRef         string `json:"cohort_ref"`
	SubjectRef        string `json:"subject_ref"`
	Label             string `json:"label"`
}

type LearnerProjection struct {
	LearnerRef  string `json:"learner_ref"`
	LearnerUUID string `json:"learner_uuid"`
	Status      string `json:"status"`
	SourceVersion string `json:"source_version"`
}

type LearnerCohortMembershipProjection struct {
	MembershipRef  string `json:"membership_ref"`
	MembershipUUID string `json:"membership_uuid"`
	LearnerUUID    string `json:"learner_uuid"`
	CohortUUID     string `json:"cohort_uuid"`
	StartsOn       string `json:"starts_on"`
	EndsOn         string `json:"ends_on"`
	Status         string `json:"status"`
}

type LearnerSubjectEnrollmentProjection struct {
	EnrollmentRef     string `json:"enrollment_ref"`
	EnrollmentUUID    string `json:"enrollment_uuid"`
	LearnerUUID       string `json:"learner_uuid"`
	CohortUUID        string `json:"cohort_uuid"`
	CohortSubjectUUID string `json:"cohort_subject_uuid"`
	SubjectUUID       string `json:"subject_uuid"`
	StartsOn          string `json:"starts_on"`
	EndsOn            string `json:"ends_on"`
	Status            string `json:"status"`
}

type TeachingAssignmentProjection struct {
	TeachingAssignmentRef  string         `json:"teaching_assignment_ref"`
	TeachingAssignmentUUID string         `json:"teaching_assignment_uuid"`
	SourceModel            string         `json:"source_model"`
	SourceID               string         `json:"source_id"`
	TeacherUUID            string         `json:"teacher_uuid"`
	CohortSubjectUUID      string         `json:"cohort_subject_uuid"`
	CohortUUID             string         `json:"cohort_uuid"`
	SubjectUUID            string         `json:"subject_uuid"`
	TeacherRef             string         `json:"teacher_ref"`
	CohortSubjectRef       string         `json:"cohort_subject_ref"`
	CohortRef              string         `json:"cohort_ref"`
	SubjectRef             string         `json:"subject_ref"`
	SubjectName            string         `json:"subject_name"`
	CohortName             string         `json:"cohort_name"`
	AcademicYearRef        string         `json:"academic_year_ref"`
	SchedulingRequirements map[string]any `json:"scheduling_requirements"`
	Status                 string         `json:"status"`
}

type WorkspaceProvisioningResult struct {
	WorkspaceID    uuid.UUID `json:"workspace_uuid"`
	InstallationID uuid.UUID `json:"installation_uuid"`
	Status         string    `json:"status"`
}
