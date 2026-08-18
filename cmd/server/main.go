package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"scholaroscope-temporal-service/config"
	"scholaroscope-temporal-service/internal/availability"
	"scholaroscope-temporal-service/internal/calendar"
	"scholaroscope-temporal-service/internal/conflict"
	"scholaroscope-temporal-service/internal/db"
	"scholaroscope-temporal-service/internal/events"
	"scholaroscope-temporal-service/internal/health"
	"scholaroscope-temporal-service/internal/launch"
	"scholaroscope-temporal-service/internal/manifest"
	"scholaroscope-temporal-service/internal/provisioning"
	"scholaroscope-temporal-service/internal/scheduling"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	log.Println("temporal service: db connected")

	// Repos
	calendarRepo     := calendar.NewRepo(pool)
	conflictRepo     := conflict.NewRepo(pool)
	schedulingRepo   := scheduling.NewRepo(pool)
	availabilityRepo := availability.NewRepo(pool)
	provisioningRepo := provisioning.NewRepo(pool)
	launchRepo       := launch.NewRepo(pool)

	// Services
	calendarService   := calendar.NewService(calendarRepo)
	schedulingService := scheduling.NewService(schedulingRepo, conflictRepo, availabilityRepo)

	// Handlers
	calendarHandler     := calendar.NewHandler(calendarService)
	schedulingHandler   := scheduling.NewHandler(schedulingService)
	availabilityHandler := availability.NewHandler(availabilityRepo)
	conflictHandler     := conflict.NewHandler(conflictRepo)
	eventHandler        := events.NewHandler(calendarService, schedulingService, availabilityRepo)
	provisioningHandler := provisioning.NewHandler(
		provisioningRepo,
		cfg.ScholaroscopeWebhookSecret,
		cfg.ScholaroscopeAllowedTimestamp,
	)
	manifestHandler := manifest.NewHandler(manifest.Config{
		PortalPublicURL:         cfg.PortalPublicURL,
		ScholaroscopeWebhookURL: cfg.ScholaroscopeWebhookURL,
	})
	healthHandler := health.NewHandler(pool, cfg)
	launchHandler := launch.NewHandler(
		launchRepo,
		cfg.ScholaroscopeWebhookSecret,
		5*time.Minute,
		cfg.PortalSessionDuration,
	)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health/live", healthHandler.Live)
	mux.HandleFunc("GET /health/ready", healthHandler.Ready)
	mux.HandleFunc("GET /plugin/manifest.json", manifestHandler.PluginManifest)

	// Calendar routes
	mux.HandleFunc("POST /orgs/{orgId}/calendar",                        launchHandler.RequirePortalPermission("timetable.manage", calendarHandler.CreateCalendar))
	mux.HandleFunc("GET /orgs/{orgId}/calendar/active",                  launchHandler.RequirePortalPermission("timetable.manage", calendarHandler.GetActiveCalendar))
	mux.HandleFunc("POST /orgs/{orgId}/calendar/{versionId}/activate",   launchHandler.RequirePortalPermission("timetable.manage", calendarHandler.ActivateCalendar))
	mux.HandleFunc("GET /orgs/{orgId}/calendar/{versionId}/slots",       launchHandler.RequirePortalPermission("timetable.manage", calendarHandler.GetTimeSlots))

	// Scheduling routes
	mux.HandleFunc("POST /orgs/{orgId}/sessions/{sessionId}/schedule",   launchHandler.RequirePortalPermission("timetable.manage", schedulingHandler.ScheduleSession))
	mux.HandleFunc("DELETE /orgs/{orgId}/sessions/{sessionId}/schedule", launchHandler.RequirePortalPermission("timetable.manage", schedulingHandler.UnscheduleSession))
	mux.HandleFunc("GET /orgs/{orgId}/calendar/{versionId}/timetable",   launchHandler.RequirePortalPermission("timetable.manage", schedulingHandler.GetTimetable))

	// Availability routes
	mux.HandleFunc("PUT /orgs/{orgId}/teachers/{teacherId}/availability", launchHandler.RequirePortalPermission("timetable.manage", availabilityHandler.SetAvailability))
	mux.HandleFunc("GET /orgs/{orgId}/teachers/{teacherId}/availability", launchHandler.RequirePortalPermission("timetable.manage", availabilityHandler.GetAvailability))

	// Conflict routes
	mux.HandleFunc("GET /orgs/{orgId}/calendar/{versionId}/conflicts",   launchHandler.RequirePortalPermission("timetable.manage", conflictHandler.ListUnresolved))
	mux.HandleFunc("POST /orgs/{orgId}/conflicts/{conflictId}/resolve",  launchHandler.RequirePortalPermission("timetable.conflicts.resolve", conflictHandler.Resolve))
	mux.HandleFunc("GET /orgs/{orgId}/conflicts/summary",                launchHandler.RequirePortalPermission("timetable.manage", conflictHandler.Summary))

	// Kernel event webhook routes
	mux.HandleFunc("POST /integration/scholaroscope/events", provisioningHandler.HandleScholaroscopeEvent)
	mux.HandleFunc("POST /portal/launch/exchange", launchHandler.Exchange)
	mux.HandleFunc("GET /portal/session", launchHandler.Session)
	mux.HandleFunc("POST /portal/logout", launchHandler.Logout)
	mux.HandleFunc("POST /events/session.created",      eventHandler.Deprecated)
	mux.HandleFunc("POST /events/session.deleted",      eventHandler.Deprecated)
	mux.HandleFunc("POST /events/teacher.assigned",     eventHandler.Deprecated)
	mux.HandleFunc("POST /events/teacher.unassigned",   eventHandler.Deprecated)
	mux.HandleFunc("POST /events/org.calendar.updated", eventHandler.Deprecated)

	log.Printf("temporal service: listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
