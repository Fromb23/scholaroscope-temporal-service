package provisioning

import (
	"context"
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

	ackPayload, _ := json.Marshal(map[string]any{
		"workspace_uuid": workspaceID.String(),
		"external_workspace_uuid": workspaceID.String(),
		"plugin_installation_ref": payload.PluginInstallationRef,
		"status": "READY",
		"actor_count": len(payload.AcademicSync.Actors),
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
		_, err = tx.Exec(ctx, `
			INSERT INTO external_academic_term (id, workspace_id, scholaroscope_term_ref, name, academic_year_label, start_date, end_date, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (workspace_id, scholaroscope_term_ref)
			DO UPDATE SET name = EXCLUDED.name, academic_year_label = EXCLUDED.academic_year_label,
			              start_date = EXCLUDED.start_date, end_date = EXCLUDED.end_date,
			              status = EXCLUDED.status, updated_at = now()`,
			termID, workspaceID, term.TermRef, term.Name, term.AcademicYearLabel, startDate, endDate, term.Status,
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
				id, workspace_id, scholaroscope_event_ref, term_uuid,
				scholaroscope_term_ref, title, event_kind, starts_on, ends_on,
				affects_learning, source, status
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'ACTIVE')
			ON CONFLICT (workspace_id, scholaroscope_event_ref)
			DO UPDATE SET term_uuid = EXCLUDED.term_uuid,
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
		_, err = tx.Exec(ctx, `
			INSERT INTO external_cohort (id, workspace_id, scholaroscope_cohort_ref, name, level, stream, academic_year_ref)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (workspace_id, scholaroscope_cohort_ref)
			DO UPDATE SET name = EXCLUDED.name, level = EXCLUDED.level, stream = EXCLUDED.stream,
			              academic_year_ref = EXCLUDED.academic_year_ref, status = 'ACTIVE', updated_at = now()`,
			cohortID, workspaceID, cohort.CohortRef, cohort.Name, cohort.Level, cohort.Stream, cohort.AcademicYearRef,
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
	for _, assignment := range sync.TeachingAssignments {
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
				cohort_subject_ref, cohort_ref, subject_ref, subject_name, cohort_name, status
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, COALESCE(NULLIF($14, ''), 'ACTIVE'))
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
			              status = EXCLUDED.status,
			              updated_at = now()`,
			id, workspaceID, assignment.TeachingAssignmentRef, teacherID,
			cohortSubjectID, cohortID, subjectID, assignment.TeacherRef,
			assignment.CohortSubjectRef, assignment.CohortRef, assignment.SubjectRef,
			assignment.SubjectName, assignment.CohortName, assignment.Status,
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
