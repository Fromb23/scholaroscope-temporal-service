package main

import (
	"context"
	"log"
	"net/http"

	"scholaroscope-temporal-service/config"
	"scholaroscope-temporal-service/internal/calendar"
	"scholaroscope-temporal-service/internal/conflict"
	"scholaroscope-temporal-service/internal/db"
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

	// Services
	calendarService := calendar.NewService(calendarRepo)
	schedulingService := scheduling.NewService(schedulingRepo, conflictRepo)

	// Handlers
	calendarHandler := calendar.NewHandler(calendarService)
	schedulingHandler := scheduling.NewHandler(schedulingService)

	mux := http.NewServeMux()

	// Calendar routes
	mux.HandleFunc("POST /orgs/{orgId}/calendar", calendarHandler.CreateCalendar)
	mux.HandleFunc("GET /orgs/{orgId}/calendar/active", calendarHandler.GetActiveCalendar)
	mux.HandleFunc("POST /orgs/{orgId}/calendar/{versionId}/activate", calendarHandler.ActivateCalendar)
	mux.HandleFunc("GET /orgs/{orgId}/calendar/{versionId}/slots", calendarHandler.GetTimeSlots)

	// Scheduling routes
	mux.HandleFunc("POST /orgs/{orgId}/sessions/{sessionId}/schedule", schedulingHandler.ScheduleSession)
	mux.HandleFunc("DELETE /orgs/{orgId}/sessions/{sessionId}/schedule", schedulingHandler.UnscheduleSession)
	mux.HandleFunc("GET /orgs/{orgId}/calendar/{versionId}/timetable", schedulingHandler.GetTimetable)

	log.Printf("temporal service: listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
