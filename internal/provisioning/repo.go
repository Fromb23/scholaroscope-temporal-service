package provisioning

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

func (r *Repo) InstallationSecret(ctx context.Context, installationRef string) (string, bool, error) {
	var secret string
	err := r.pool.QueryRow(ctx, `
		SELECT signing_secret
		FROM integration_installation
		WHERE scholaroscope_installation_ref = $1
		  AND status = 'ACTIVE'`,
		installationRef,
	).Scan(&secret)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("provisioning repo: installation secret: %w", err)
	}
	return secret, true, nil
}

func (r *Repo) BootstrapWorkspace(ctx context.Context, payload BootstrapPayload) (*WorkspaceProvisioningResult, error) {
	if strings.TrimSpace(payload.SigningSecret) == "" {
		return nil, fmt.Errorf("provisioning repo: signing secret is required")
	}
	signingKeyID := strings.TrimSpace(payload.SigningKeyID)
	if signingKeyID == "" {
		signingKeyID = payload.PluginInstallationRef
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("provisioning repo: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	workspaceID := uuid.New()
	err = tx.QueryRow(ctx, `
		INSERT INTO external_workspace (
			id, scholaroscope_workspace_ref, scholaroscope_organization_ref,
			display_name, timezone, status, provisioning_state, integration_health
		)
		VALUES ($1, $2, $3, $4, $5, 'ACTIVE', 'READY', 'HEALTHY')
		ON CONFLICT (scholaroscope_workspace_ref, scholaroscope_organization_ref)
		DO UPDATE SET
			display_name = EXCLUDED.display_name,
			timezone = EXCLUDED.timezone,
			status = 'ACTIVE',
			provisioning_state = 'READY',
			integration_health = 'HEALTHY',
			last_successful_sync_at = now(),
			last_error = NULL,
			updated_at = now()
		RETURNING id`,
		workspaceID,
		payload.ScholaroscopeWorkspaceRef,
		payload.ScholaroscopeOrganizationRef,
		payload.DisplayName,
		payload.Timezone,
	).Scan(&workspaceID)
	if err != nil {
		return nil, fmt.Errorf("provisioning repo: upsert workspace: %w", err)
	}

	installationID := uuid.New()
	err = tx.QueryRow(ctx, `
		INSERT INTO integration_installation (
			id, workspace_id, scholaroscope_installation_ref, signing_key_id,
			signing_secret, callback_url, status, enabled_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'ACTIVE', now())
		ON CONFLICT (scholaroscope_installation_ref)
		DO UPDATE SET
			workspace_id = EXCLUDED.workspace_id,
			signing_key_id = EXCLUDED.signing_key_id,
			signing_secret = EXCLUDED.signing_secret,
			callback_url = COALESCE(NULLIF(EXCLUDED.callback_url, ''), integration_installation.callback_url),
			status = 'ACTIVE',
			enabled_at = now(),
			disabled_at = NULL,
			revoked_at = NULL,
			updated_at = now()
		RETURNING id`,
		installationID,
		workspaceID,
		payload.PluginInstallationRef,
		signingKeyID,
		payload.SigningSecret,
		payload.CallbackURL,
	).Scan(&installationID)
	if err != nil {
		return nil, fmt.Errorf("provisioning repo: upsert installation: %w", err)
	}

	if err := r.upsertAcademicSync(ctx, tx, workspaceID, payload.AcademicSync); err != nil {
		return nil, err
	}
	snapshotJSON, err := json.Marshal(payload.AcademicSync)
	if err != nil {
		return nil, fmt.Errorf("provisioning repo: marshal academic snapshot: %w", err)
	}
	snapshotHash := fmt.Sprintf("%x", sha256.Sum256(snapshotJSON))
	learnerSnapshotJSON, err := json.Marshal(map[string]any{
		"learners":                   payload.AcademicSync.Learners,
		"learner_cohort_memberships": payload.AcademicSync.LearnerMemberships,
		"learner_subject_enrollments": payload.AcademicSync.LearnerEnrollments,
	})
	if err != nil {
		return nil, fmt.Errorf("provisioning repo: marshal learner snapshot: %w", err)
	}
	learnerSnapshotHash := fmt.Sprintf("%x", sha256.Sum256(learnerSnapshotJSON))
	if _, err := tx.Exec(ctx, `
		UPDATE external_workspace
		SET academic_snapshot_hash = $1,
		    source_assignment_count = $3,
		    eligible_assignment_count = $4,
		    source_learner_count = $5,
		    eligible_learner_count = $6,
		    learner_enrollment_snapshot_hash = $7,
		    last_successful_sync_at = now(),
		    integration_health = 'HEALTHY',
		    reconciliation_required = false,
		    last_error = NULL,
		    updated_at = now()
		WHERE id = $2`, snapshotHash, workspaceID, payload.AcademicSync.AssignmentReadiness.SourceAssignmentCount, payload.AcademicSync.AssignmentReadiness.EligibleAssignmentCount, payload.AcademicSync.LearnerReadiness.SourceLearnerCount, payload.AcademicSync.LearnerReadiness.EligibleLearnerCount, learnerSnapshotHash); err != nil {
		return nil, fmt.Errorf("provisioning repo: record academic snapshot: %w", err)
	}

	ackPayload, _ := json.Marshal(map[string]any{
		"workspace_uuid":            workspaceID.String(),
		"external_workspace_uuid":   workspaceID.String(),
		"plugin_installation_ref":   payload.PluginInstallationRef,
		"status":                    "READY",
		"actor_count":               len(payload.AcademicSync.Actors),
		"teaching_assignment_count": len(payload.AcademicSync.TeachingAssignments),
	})
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_event (
			id, workspace_id, installation_id, event_type, schema_version,
			aggregate_type, aggregate_uuid, aggregate_version, correlation_id,
			idempotency_key, payload
		)
		VALUES ($1, $2, $3, 'temporal.timetable.workspace.provisioned.v1', '1.0',
		        'WORKSPACE', $2, 1, $4, $5, $6::jsonb)
		ON CONFLICT (installation_id, idempotency_key) DO NOTHING`,
		uuid.New(),
		workspaceID,
		installationID,
		uuid.New(),
		"workspace-provisioned:"+payload.PluginInstallationRef,
		string(ackPayload),
	)
	if err != nil {
		return nil, fmt.Errorf("provisioning repo: enqueue provisioning ack: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("provisioning repo: commit: %w", err)
	}
	return &WorkspaceProvisioningResult{
		WorkspaceID:    workspaceID,
		InstallationID: installationID,
		Status:         "ready",
	}, nil
}

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func (r *Repo) upsertAcademicSync(ctx context.Context, tx execer, workspaceID uuid.UUID, sync AcademicSyncPayload) error {
	if _, err := tx.Exec(ctx, `
		UPDATE external_actor_role
		SET status = 'DISABLED', updated_at = now()
		WHERE workspace_id = $1`, workspaceID); err != nil {
		return fmt.Errorf("provisioning repo: disable stale actor roles: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE external_teaching_assignment
		SET status = 'DISABLED', updated_at = now()
		WHERE workspace_id = $1`, workspaceID); err != nil {
		return fmt.Errorf("provisioning repo: disable stale assignments: %w", err)
	}
	for _, statement := range []string{
		"UPDATE external_academic_year SET is_current = false, status = 'ARCHIVED', updated_at = now() WHERE workspace_id = $1",
		"UPDATE external_academic_term SET status = 'CLOSED_HISTORICAL', updated_at = now() WHERE workspace_id = $1",
		"UPDATE external_calendar_event SET status = 'REMOVED', updated_at = now() WHERE workspace_id = $1",
		"UPDATE external_cohort SET status = 'DISABLED', updated_at = now() WHERE workspace_id = $1",
		"UPDATE external_subject SET status = 'DISABLED', updated_at = now() WHERE workspace_id = $1",
		"UPDATE external_cohort_subject SET status = 'DISABLED', updated_at = now() WHERE workspace_id = $1",
		"UPDATE external_learner SET status = 'DISABLED', updated_at = now() WHERE workspace_id = $1",
		"UPDATE external_learner_cohort_membership SET status = 'DISABLED', updated_at = now() WHERE workspace_id = $1",
		"UPDATE external_learner_subject_enrollment SET status = 'DISABLED', updated_at = now() WHERE workspace_id = $1",
	} {
		if _, err := tx.Exec(ctx, statement, workspaceID); err != nil {
			return fmt.Errorf("provisioning repo: disable stale academic projection: %w", err)
		}
	}
	for _, actor := range sync.Actors {
		actorID, err := uuid.Parse(actor.ActorUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid actor uuid: %w", err)
		}
		kind := "MANAGER"
		for _, actorKind := range actor.ActorKinds {
			if actorKind == "TEACHER" {
				kind = "TEACHER"
				break
			}
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO external_actor (id, workspace_id, scholaroscope_user_ref, display_name, actor_kind, status)
			VALUES ($1, $2, $3, $4, $5, COALESCE(NULLIF($6, ''), 'ACTIVE'))
			ON CONFLICT (workspace_id, scholaroscope_user_ref)
			DO UPDATE SET display_name = EXCLUDED.display_name,
			              actor_kind = EXCLUDED.actor_kind,
			              status = EXCLUDED.status,
			              updated_at = now()`,
			actorID,
			workspaceID,
			actor.UserRef,
			actor.DisplayName,
			kind,
			actor.Status,
		)
		if err != nil {
			return fmt.Errorf("provisioning repo: upsert actor: %w", err)
		}
		if len(actor.ActorKinds) == 0 {
			actor.ActorKinds = []string{kind}
		}
		for _, actorKind := range actor.ActorKinds {
			actorKind = strings.ToUpper(strings.TrimSpace(actorKind))
			if actorKind == "" {
				continue
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO external_actor_role (workspace_id, actor_id, actor_kind, status)
				VALUES ($1, $2, $3, COALESCE(NULLIF($4, ''), 'ACTIVE'))
				ON CONFLICT (workspace_id, actor_id, actor_kind)
				DO UPDATE SET status = EXCLUDED.status, updated_at = now()`,
				workspaceID, actorID, actorKind, actor.Status,
			)
			if err != nil {
				return fmt.Errorf("provisioning repo: upsert actor role: %w", err)
			}
		}
	}
	for _, academicYear := range sync.AcademicYears {
		academicYearID, err := uuid.Parse(academicYear.AcademicYearUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid academic year uuid: %w", err)
		}
		startDate, err := time.Parse("2006-01-02", academicYear.StartDate)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid academic year start date: %w", err)
		}
		endDate, err := time.Parse("2006-01-02", academicYear.EndDate)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid academic year end date: %w", err)
		}
		status := strings.ToUpper(strings.TrimSpace(academicYear.Status))
		if status == "" {
			if academicYear.IsCurrent {
				status = "CURRENT"
			} else {
				status = "ACTIVE"
			}
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO external_academic_year (
				id, workspace_id, scholaroscope_academic_year_ref, name, start_date, end_date,
				is_current, status, curriculum_ref, curriculum_name
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (workspace_id, scholaroscope_academic_year_ref)
			DO UPDATE SET name = EXCLUDED.name,
			              start_date = EXCLUDED.start_date,
			              end_date = EXCLUDED.end_date,
			              is_current = EXCLUDED.is_current,
			              status = EXCLUDED.status,
			              curriculum_ref = EXCLUDED.curriculum_ref,
			              curriculum_name = EXCLUDED.curriculum_name,
			              updated_at = now()`,
			academicYearID, workspaceID, academicYear.AcademicYearRef, academicYear.Name,
			startDate, endDate, academicYear.IsCurrent, status, academicYear.CurriculumRef,
			academicYear.CurriculumName,
		)
		if err != nil {
			return fmt.Errorf("provisioning repo: upsert academic year: %w", err)
		}
	}
	for _, term := range sync.Terms {
		termID, err := uuid.Parse(term.TermUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid term uuid: %w", err)
		}
		startDate, err := time.Parse("2006-01-02", term.StartDate)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid term start date: %w", err)
		}
		endDate, err := time.Parse("2006-01-02", term.EndDate)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid term end date: %w", err)
		}
		var academicYearID *uuid.UUID
		if strings.TrimSpace(term.AcademicYearUUID) != "" {
			parsed, err := uuid.Parse(term.AcademicYearUUID)
			if err != nil {
				return fmt.Errorf("provisioning repo: invalid term academic year uuid: %w", err)
			}
			academicYearID = &parsed
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO external_academic_term (
				id, workspace_id, scholaroscope_term_ref, name, academic_year_label,
				start_date, end_date, status, academic_year_uuid,
				scholaroscope_academic_year_ref, calendar_ready, is_frozen
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (workspace_id, scholaroscope_term_ref)
			DO UPDATE SET name = EXCLUDED.name, academic_year_label = EXCLUDED.academic_year_label,
			              start_date = EXCLUDED.start_date, end_date = EXCLUDED.end_date,
			              status = EXCLUDED.status,
			              academic_year_uuid = EXCLUDED.academic_year_uuid,
			              scholaroscope_academic_year_ref = EXCLUDED.scholaroscope_academic_year_ref,
			              calendar_ready = EXCLUDED.calendar_ready,
			              is_frozen = EXCLUDED.is_frozen,
			              updated_at = now()`,
			termID, workspaceID, term.TermRef, term.Name, term.AcademicYearLabel,
			startDate, endDate, term.Status, academicYearID, term.AcademicYearRef,
			term.CalendarReady, term.IsFrozen,
		)
		if err != nil {
			return fmt.Errorf("provisioning repo: upsert term: %w", err)
		}
	}
	for _, event := range sync.CalendarEvents {
		eventID, err := uuid.Parse(event.EventUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid calendar event uuid: %w", err)
		}
		var termID *uuid.UUID
		if strings.TrimSpace(event.TermUUID) != "" {
			parsed, err := uuid.Parse(event.TermUUID)
			if err != nil {
				return fmt.Errorf("provisioning repo: invalid calendar event term uuid: %w", err)
			}
			termID = &parsed
		}
		var academicYearID *uuid.UUID
		if strings.TrimSpace(event.AcademicYearUUID) != "" {
			parsed, err := uuid.Parse(event.AcademicYearUUID)
			if err != nil {
				return fmt.Errorf("provisioning repo: invalid calendar event academic year uuid: %w", err)
			}
			academicYearID = &parsed
		}
		startDate, err := time.Parse("2006-01-02", event.StartDate)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid calendar event start date: %w", err)
		}
		endDate, err := time.Parse("2006-01-02", event.EndDate)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid calendar event end date: %w", err)
		}
		source := strings.TrimSpace(event.Source)
		if source == "" {
			source = "SCHOLAROSCOPE_TERM_CALENDAR"
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO external_calendar_event (
				id, workspace_id, scholaroscope_event_ref, academic_year_uuid,
				scholaroscope_academic_year_ref, term_uuid,
				scholaroscope_term_ref, title, event_kind, starts_on, ends_on,
				affects_learning, source, status
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'ACTIVE')
			ON CONFLICT (workspace_id, scholaroscope_event_ref)
			DO UPDATE SET academic_year_uuid = EXCLUDED.academic_year_uuid,
			              scholaroscope_academic_year_ref = EXCLUDED.scholaroscope_academic_year_ref,
			              term_uuid = EXCLUDED.term_uuid,
			              scholaroscope_term_ref = EXCLUDED.scholaroscope_term_ref,
			              title = EXCLUDED.title,
			              event_kind = EXCLUDED.event_kind,
			              starts_on = EXCLUDED.starts_on,
			              ends_on = EXCLUDED.ends_on,
			              affects_learning = EXCLUDED.affects_learning,
			              source = EXCLUDED.source,
			              status = 'ACTIVE',
			              updated_at = now()`,
			eventID,
			workspaceID,
			event.EventRef,
			academicYearID,
			event.AcademicYearRef,
			termID,
			event.TermRef,
			event.Title,
			event.EventType,
			startDate,
			endDate,
			event.AffectsLearning,
			source,
		)
		if err != nil {
			return fmt.Errorf("provisioning repo: upsert calendar event: %w", err)
		}
	}
	for _, cohort := range sync.Cohorts {
		cohortID, err := uuid.Parse(cohort.CohortUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid cohort uuid: %w", err)
		}
		var academicYearID *uuid.UUID
		if strings.TrimSpace(cohort.AcademicYearUUID) != "" {
			parsed, err := uuid.Parse(cohort.AcademicYearUUID)
			if err != nil {
				return fmt.Errorf("provisioning repo: invalid cohort academic year uuid: %w", err)
			}
			academicYearID = &parsed
		}
		status := strings.ToUpper(strings.TrimSpace(cohort.Status))
		if status == "" {
			status = "ACTIVE"
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO external_cohort (id, workspace_id, scholaroscope_cohort_ref, name, level, stream, academic_year_ref, academic_year_uuid, enrollment_count, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (workspace_id, scholaroscope_cohort_ref)
			DO UPDATE SET name = EXCLUDED.name, level = EXCLUDED.level, stream = EXCLUDED.stream,
			              academic_year_ref = EXCLUDED.academic_year_ref,
			              academic_year_uuid = EXCLUDED.academic_year_uuid,
			              enrollment_count = EXCLUDED.enrollment_count,
			              status = EXCLUDED.status, updated_at = now()`,
			cohortID, workspaceID, cohort.CohortRef, cohort.Name, cohort.Level, cohort.Stream,
			cohort.AcademicYearRef, academicYearID, cohort.EnrollmentCount, status,
		)
		if err != nil {
			return fmt.Errorf("provisioning repo: upsert cohort: %w", err)
		}
	}
	for _, subject := range sync.Subjects {
		subjectID, err := uuid.Parse(subject.SubjectUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid subject uuid: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO external_subject (id, workspace_id, scholaroscope_subject_ref, name, code, level, status)
			VALUES ($1, $2, $3, $4, $5, $6, COALESCE(NULLIF($7, ''), 'ACTIVE'))
			ON CONFLICT (workspace_id, scholaroscope_subject_ref)
			DO UPDATE SET name = EXCLUDED.name, code = EXCLUDED.code, level = EXCLUDED.level,
			              status = EXCLUDED.status, updated_at = now()`,
			subjectID, workspaceID, subject.SubjectRef, subject.Name, subject.Code, subject.Level, subject.Status,
		)
		if err != nil {
			return fmt.Errorf("provisioning repo: upsert subject: %w", err)
		}
	}
	for _, cohortSubject := range sync.CohortSubjects {
		id, err := uuid.Parse(cohortSubject.CohortSubjectUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid cohort subject uuid: %w", err)
		}
		cohortID, err := uuid.Parse(cohortSubject.CohortUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid cohort uuid: %w", err)
		}
		subjectID, err := uuid.Parse(cohortSubject.SubjectUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid subject uuid: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO external_cohort_subject (id, workspace_id, scholaroscope_cohort_subject_ref, cohort_uuid, subject_uuid, label)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (workspace_id, scholaroscope_cohort_subject_ref)
			DO UPDATE SET cohort_uuid = EXCLUDED.cohort_uuid, subject_uuid = EXCLUDED.subject_uuid,
			              label = EXCLUDED.label, status = 'ACTIVE', updated_at = now()`,
			id, workspaceID, cohortSubject.CohortSubjectRef, cohortID, subjectID, cohortSubject.Label,
		)
		if err != nil {
			return fmt.Errorf("provisioning repo: upsert cohort subject: %w", err)
		}
	}
	for _, learner := range sync.Learners {
		learnerID, err := uuid.Parse(learner.LearnerUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid learner uuid: %w", err)
		}
		status := strings.ToUpper(strings.TrimSpace(learner.Status))
		if status == "" {
			status = "ACTIVE"
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO external_learner (
				id, workspace_id, scholaroscope_learner_ref, status, source_version
			)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (workspace_id, scholaroscope_learner_ref)
			DO UPDATE SET status = EXCLUDED.status,
			              source_version = EXCLUDED.source_version,
			              updated_at = now()`,
			learnerID, workspaceID, learner.LearnerRef, status, learner.SourceVersion,
		)
		if err != nil {
			return fmt.Errorf("provisioning repo: upsert learner: %w", err)
		}
	}
	for _, membership := range sync.LearnerMemberships {
		id, err := uuid.Parse(membership.MembershipUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid learner membership uuid: %w", err)
		}
		learnerID, err := uuid.Parse(membership.LearnerUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid membership learner uuid: %w", err)
		}
		cohortID, err := uuid.Parse(membership.CohortUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid membership cohort uuid: %w", err)
		}
		status := strings.ToUpper(strings.TrimSpace(membership.Status))
		if status == "" {
			status = "ACTIVE"
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO external_learner_cohort_membership (
				id, workspace_id, learner_uuid, cohort_uuid,
				scholaroscope_membership_ref, starts_on, ends_on, status
			)
			VALUES ($1, $2, $3, $4, $5, $6::date, $7::date, $8)
			ON CONFLICT (workspace_id, scholaroscope_membership_ref)
			DO UPDATE SET learner_uuid = EXCLUDED.learner_uuid,
			              cohort_uuid = EXCLUDED.cohort_uuid,
			              starts_on = EXCLUDED.starts_on,
			              ends_on = EXCLUDED.ends_on,
			              status = EXCLUDED.status,
			              updated_at = now()`,
			id, workspaceID, learnerID, cohortID, membership.MembershipRef,
			nullableDateString(membership.StartsOn), nullableDateString(membership.EndsOn), status,
		)
		if err != nil {
			return fmt.Errorf("provisioning repo: upsert learner cohort membership: %w", err)
		}
	}
	for _, enrollment := range sync.LearnerEnrollments {
		id, err := uuid.Parse(enrollment.EnrollmentUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid learner enrollment uuid: %w", err)
		}
		learnerID, err := uuid.Parse(enrollment.LearnerUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid enrollment learner uuid: %w", err)
		}
		cohortID, err := uuid.Parse(enrollment.CohortUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid enrollment cohort uuid: %w", err)
		}
		cohortSubjectID, err := uuid.Parse(enrollment.CohortSubjectUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid enrollment cohort subject uuid: %w", err)
		}
		subjectID, err := uuid.Parse(enrollment.SubjectUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid enrollment subject uuid: %w", err)
		}
		status := strings.ToUpper(strings.TrimSpace(enrollment.Status))
		if status == "" {
			status = "ACTIVE"
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO external_learner_subject_enrollment (
				id, workspace_id, learner_uuid, cohort_uuid, cohort_subject_uuid,
				subject_uuid, scholaroscope_enrollment_ref, starts_on, ends_on, status
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9::date, $10)
			ON CONFLICT (workspace_id, scholaroscope_enrollment_ref)
			DO UPDATE SET learner_uuid = EXCLUDED.learner_uuid,
			              cohort_uuid = EXCLUDED.cohort_uuid,
			              cohort_subject_uuid = EXCLUDED.cohort_subject_uuid,
			              subject_uuid = EXCLUDED.subject_uuid,
			              starts_on = EXCLUDED.starts_on,
			              ends_on = EXCLUDED.ends_on,
			              status = EXCLUDED.status,
			              updated_at = now()`,
			id, workspaceID, learnerID, cohortID, cohortSubjectID, subjectID,
			enrollment.EnrollmentRef, nullableDateString(enrollment.StartsOn), nullableDateString(enrollment.EndsOn), status,
		)
		if err != nil {
			return fmt.Errorf("provisioning repo: upsert learner subject enrollment: %w", err)
		}
	}
	for _, assignment := range sync.TeachingAssignments {
		if strings.TrimSpace(assignment.SourceModel) == "" {
			assignment.SourceModel = "academic.CohortSubjectInstructor"
		}
		requirements, err := json.Marshal(assignment.SchedulingRequirements)
		if err != nil {
			return fmt.Errorf("provisioning repo: marshal scheduling requirements: %w", err)
		}
		id, err := uuid.Parse(assignment.TeachingAssignmentUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid assignment uuid: %w", err)
		}
		teacherID, err := uuid.Parse(assignment.TeacherUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid assignment teacher uuid: %w", err)
		}
		cohortSubjectID, err := uuid.Parse(assignment.CohortSubjectUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid assignment cohort subject uuid: %w", err)
		}
		cohortID, err := uuid.Parse(assignment.CohortUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid assignment cohort uuid: %w", err)
		}
		subjectID, err := uuid.Parse(assignment.SubjectUUID)
		if err != nil {
			return fmt.Errorf("provisioning repo: invalid assignment subject uuid: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO external_teaching_assignment (
				id, workspace_id, scholaroscope_teaching_assignment_ref, teacher_uuid,
				cohort_subject_uuid, cohort_uuid, subject_uuid, teacher_ref,
				cohort_subject_ref, cohort_ref, subject_ref, subject_name, cohort_name,
				source_model, source_id, academic_year_ref, scheduling_requirements, status
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			        $14, $15, $16, $17::jsonb, COALESCE(NULLIF($18, ''), 'ACTIVE'))
			ON CONFLICT (workspace_id, scholaroscope_teaching_assignment_ref)
			DO UPDATE SET teacher_uuid = EXCLUDED.teacher_uuid,
			              cohort_subject_uuid = EXCLUDED.cohort_subject_uuid,
			              cohort_uuid = EXCLUDED.cohort_uuid,
			              subject_uuid = EXCLUDED.subject_uuid,
			              teacher_ref = EXCLUDED.teacher_ref,
			              cohort_subject_ref = EXCLUDED.cohort_subject_ref,
			              cohort_ref = EXCLUDED.cohort_ref,
			              subject_ref = EXCLUDED.subject_ref,
			              subject_name = EXCLUDED.subject_name,
			              cohort_name = EXCLUDED.cohort_name,
			              source_model = EXCLUDED.source_model,
			              source_id = EXCLUDED.source_id,
			              academic_year_ref = EXCLUDED.academic_year_ref,
			              scheduling_requirements = EXCLUDED.scheduling_requirements,
			              status = EXCLUDED.status,
			              updated_at = now()`,
			id, workspaceID, assignment.TeachingAssignmentRef, teacherID,
			cohortSubjectID, cohortID, subjectID, assignment.TeacherRef,
			assignment.CohortSubjectRef, assignment.CohortRef, assignment.SubjectRef,
			assignment.SubjectName, assignment.CohortName, assignment.SourceModel,
			assignment.SourceID, assignment.AcademicYearRef, string(requirements), assignment.Status,
		)
		if err != nil {
			return fmt.Errorf("provisioning repo: upsert teaching assignment: %w", err)
		}
	}
	return nil
}

func (r *Repo) DisableWorkspace(ctx context.Context, scholaroscopeInstallationRef string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE integration_installation
		SET status = 'DISABLED',
		    disabled_at = now(),
		    updated_at = now()
		WHERE scholaroscope_installation_ref = $1`,
		scholaroscopeInstallationRef,
	)
	if err != nil {
		return fmt.Errorf("provisioning repo: disable installation: %w", err)
	}
	return nil
}

func nullableDateString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
