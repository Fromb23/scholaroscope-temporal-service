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
	"scholaroscope-temporal-service/internal/portalapi"
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
	calendarRepo := calendar.NewRepo(pool)
	conflictRepo := conflict.NewRepo(pool)
	schedulingRepo := scheduling.NewRepo(pool)
	availabilityRepo := availability.NewRepo(pool)
	provisioningRepo := provisioning.NewRepo(pool)
	launchRepo := launch.NewRepo(pool)

	// Services
	calendarService := calendar.NewService(calendarRepo)
	schedulingService := scheduling.NewService(schedulingRepo, conflictRepo, availabilityRepo)

	// Handlers
	calendarHandler := calendar.NewHandler(calendarService)
	schedulingHandler := scheduling.NewHandler(schedulingService)
	availabilityHandler := availability.NewHandler(availabilityRepo)
	conflictHandler := conflict.NewHandler(conflictRepo)
	eventHandler := events.NewHandler(calendarService, schedulingService, availabilityRepo)
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
		cfg.PortalCookieSecure,
	)
	portalAPIHandler := portalapi.NewHandler(pool, calendarService)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health/live", healthHandler.Live)
	mux.HandleFunc("GET /health/ready", healthHandler.Ready)
	mux.HandleFunc("GET /plugin/manifest.json", manifestHandler.PluginManifest)

	// Calendar routes
	mux.HandleFunc("POST /orgs/{orgId}/calendar", launchHandler.RequirePortalPermission("timetable.manage", calendarHandler.CreateCalendar))
	mux.HandleFunc("GET /orgs/{orgId}/calendar/active", launchHandler.RequirePortalPermission("timetable.manage", calendarHandler.GetActiveCalendar))
	mux.HandleFunc("POST /orgs/{orgId}/calendar/{versionId}/activate", launchHandler.RequirePortalPermission("timetable.manage", calendarHandler.ActivateCalendar))
	mux.HandleFunc("GET /orgs/{orgId}/calendar/{versionId}/slots", launchHandler.RequirePortalPermission("timetable.manage", calendarHandler.GetTimeSlots))

	// Scheduling routes
	mux.HandleFunc("POST /orgs/{orgId}/sessions/{sessionId}/schedule", launchHandler.RequirePortalPermission("timetable.manage", schedulingHandler.ScheduleSession))
	mux.HandleFunc("DELETE /orgs/{orgId}/sessions/{sessionId}/schedule", launchHandler.RequirePortalPermission("timetable.manage", schedulingHandler.UnscheduleSession))
	mux.HandleFunc("GET /orgs/{orgId}/calendar/{versionId}/timetable", launchHandler.RequirePortalPermission("timetable.manage", schedulingHandler.GetTimetable))

	// Availability routes
	mux.HandleFunc("PUT /orgs/{orgId}/teachers/{teacherId}/availability", launchHandler.RequirePortalPermission("timetable.manage", availabilityHandler.SetAvailability))
	mux.HandleFunc("GET /orgs/{orgId}/teachers/{teacherId}/availability", launchHandler.RequirePortalPermission("timetable.manage", availabilityHandler.GetAvailability))

	// Conflict routes
	mux.HandleFunc("GET /orgs/{orgId}/calendar/{versionId}/conflicts", launchHandler.RequirePortalPermission("timetable.manage", conflictHandler.ListUnresolved))
	mux.HandleFunc("POST /orgs/{orgId}/conflicts/{conflictId}/resolve", launchHandler.RequirePortalPermission("timetable.conflicts.resolve", conflictHandler.Resolve))
	mux.HandleFunc("GET /orgs/{orgId}/conflicts/summary", launchHandler.RequirePortalPermission("timetable.manage", conflictHandler.Summary))

	// Kernel event webhook routes
	mux.HandleFunc("POST /integration/scholaroscope/events", provisioningHandler.HandleScholaroscopeEvent)
	mux.HandleFunc("POST /portal/launch/exchange", launchHandler.Exchange)
	mux.HandleFunc("GET /portal/session", launchHandler.Session)
	mux.HandleFunc("POST /portal/logout", launchHandler.Logout)

	// Workspace-implicit portal API routes.
	mux.HandleFunc("GET /api/v1/workspace", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.Workspace))
	mux.HandleFunc("GET /api/v1/academic-context", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.AcademicContext))
	mux.HandleFunc("GET /api/v1/classes-spaces", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.ClassesSpaces))
	mux.HandleFunc("GET /api/v1/workflow", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.Workflow))
	mux.HandleFunc("GET /api/v1/calendar", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.GetCalendar))
	mux.HandleFunc("PUT /api/v1/calendar", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.PutCalendar))
	mux.HandleFunc("POST /api/v1/versions/{versionId}/generate", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.GenerateVersion))
	mux.HandleFunc("GET /api/v1/exceptions", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.CalendarExceptions))
	mux.HandleFunc("GET /api/v1/teachers", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.Teachers))
	mux.HandleFunc("GET /api/v1/availability", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.Availability))
	mux.HandleFunc("GET /api/v1/teaching-demands", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.TeachingDemands))
	mux.HandleFunc("GET /api/v1/rooms", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.Rooms))
	mux.HandleFunc("POST /api/v1/rooms", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.Rooms))
	mux.HandleFunc("PATCH /api/v1/rooms/{roomId}", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.RoomDetail))
	mux.HandleFunc("DELETE /api/v1/rooms/{roomId}", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.RoomDetail))
	mux.HandleFunc("PATCH /api/v1/classes/{cohortId}/default-room", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.ClassDefaultRoom))
	mux.HandleFunc("GET /api/v1/timetables", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.Timetables))
	mux.HandleFunc("POST /api/v1/timetables", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.Timetables))
	mux.HandleFunc("GET /api/v1/timetables/{timetableId}", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.TimetableDetail))
	mux.HandleFunc("POST /api/v1/timetables/{timetableId}/versions", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.CreateVersion))
	mux.HandleFunc("GET /api/v1/timetable-versions/{versionId}", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.VersionDetail))
	mux.HandleFunc("POST /api/v1/timetable-versions/{versionId}/entries", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.VersionEntries))
	mux.HandleFunc("PATCH /api/v1/timetable-versions/{versionId}/entries/{entryId}", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.EntryDetail))
	mux.HandleFunc("DELETE /api/v1/timetable-versions/{versionId}/entries/{entryId}", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.EntryDetail))
	mux.HandleFunc("GET /api/v1/conflicts", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.Conflicts))
	mux.HandleFunc("POST /api/v1/timetable-versions/{versionId}/validate", launchHandler.RequirePortalSession("timetable.manage", portalAPIHandler.ValidateVersion))
	mux.HandleFunc("POST /api/v1/timetable-versions/{versionId}/publish", launchHandler.RequirePortalSession("timetable.publish", portalAPIHandler.PublishVersion))

	mux.HandleFunc("POST /events/session.created", eventHandler.Deprecated)
	mux.HandleFunc("POST /events/session.deleted", eventHandler.Deprecated)
	mux.HandleFunc("POST /events/teacher.assigned", eventHandler.Deprecated)
	mux.HandleFunc("POST /events/teacher.unassigned", eventHandler.Deprecated)
	mux.HandleFunc("POST /events/org.calendar.updated", eventHandler.Deprecated)

	log.Printf("temporal service: listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, withCORS(mux, cfg.CORSAllowedOrigins)); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func withCORS(next http.Handler, allowedOrigins []string) http.Handler {
	allowed := map[string]bool{}
	for _, origin := range allowedOrigins {
		allowed[origin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Scholaroscope-Timestamp, X-Scholaroscope-Signature")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
