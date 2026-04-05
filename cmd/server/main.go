package main

import (
	"context"
	"log"
	"net/http"

	"scholaroscope-temporal-service/config"
	"scholaroscope-temporal-service/internal/calendar"
	"scholaroscope-temporal-service/internal/db"
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

	calendarRepo := calendar.NewRepo(pool)
	calendarService := calendar.NewService(calendarRepo)

	mux := http.NewServeMux()

	// Calendar routes
	calendarHandler := calendar.NewHandler(calendarService)
	mux.HandleFunc("POST /orgs/{orgId}/calendar", calendarHandler.CreateCalendar)
	mux.HandleFunc("GET /orgs/{orgId}/calendar/active", calendarHandler.GetActiveCalendar)
	mux.HandleFunc("POST /orgs/{orgId}/calendar/{versionId}/activate", calendarHandler.ActivateCalendar)
	mux.HandleFunc("GET /orgs/{orgId}/calendar/{versionId}/slots", calendarHandler.GetTimeSlots)

	log.Printf("temporal service: listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
