package events

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"scholaroscope-temporal-service/internal/availability"
	"scholaroscope-temporal-service/internal/calendar"
	"scholaroscope-temporal-service/internal/scheduling"

	"github.com/google/uuid"
)

type Handler struct {
	calendarService   *calendar.Service
	schedulingService *scheduling.Service
	availabilityRepo  *availability.Repo
}

func NewHandler(
	calendarService *calendar.Service,
	schedulingService *scheduling.Service,
	availabilityRepo *availability.Repo,
) *Handler {
	return &Handler{
		calendarService:   calendarService,
		schedulingService: schedulingService,
		availabilityRepo:  availabilityRepo,
	}
}

// POST /events/session.created
func (h *Handler) OnSessionCreated(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		OrgID             string `json:"org_id"`
		SessionID         string `json:"session_id"`
		CalendarVersionID string `json:"calendar_version_id"`
		TeacherID         string `json:"teacher_id"`
		CohortSubjectID   string `json:"cohort_subject_id"`
		DurationSlots     int16  `json:"duration_slots"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	orgID, err := uuid.Parse(payload.OrgID)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid org_id"); return }
	sessionID, err := uuid.Parse(payload.SessionID)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid session_id"); return }
	calendarVersionID, err := uuid.Parse(payload.CalendarVersionID)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid calendar_version_id"); return }
	teacherID, err := uuid.Parse(payload.TeacherID)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid teacher_id"); return }
	cohortSubjectID, err := uuid.Parse(payload.CohortSubjectID)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid cohort_subject_id"); return }

	duration := payload.DurationSlots
	if duration <= 0 {
		duration = 1
	}

	ss, err := h.schedulingService.Schedule(r.Context(), &scheduling.ScheduleRequest{
		OrgID:             orgID,
		SessionID:         sessionID,
		CalendarVersionID: calendarVersionID,
		TeacherID:         teacherID,
		CohortSubjectID:   cohortSubjectID,
		DurationSlots:     duration,
		ScheduleMode:      scheduling.ScheduleModeLearning,
	})
	if err != nil {
		log.Printf("events: session.created: scheduling failed for %s: %v", sessionID, err)
		writeJSON(w, http.StatusOK, map[string]string{
			"status":     "conflict",
			"session_id": sessionID.String(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "scheduled",
		"session_id":  ss.SessionID,
		"timeslot_id": ss.TimeslotID,
	})
}

// POST /events/session.deleted
func (h *Handler) OnSessionDeleted(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		SessionID string `json:"session_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	sessionID, err := uuid.Parse(payload.SessionID)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid session_id"); return }

	if err := h.schedulingService.Unschedule(r.Context(), sessionID); err != nil {
		log.Printf("events: session.deleted: unschedule %s: %v", sessionID, err)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "unscheduled",
		"session_id": sessionID.String(),
	})
}

// POST /events/teacher.assigned
func (h *Handler) OnTeacherAssigned(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		OrgID             string `json:"org_id"`
		TeacherID         string `json:"teacher_id"`
		CalendarVersionID string `json:"calendar_version_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	orgID, err := uuid.Parse(payload.OrgID)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid org_id"); return }
	teacherID, err := uuid.Parse(payload.TeacherID)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid teacher_id"); return }
	calendarVersionID, err := uuid.Parse(payload.CalendarVersionID)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid calendar_version_id"); return }

	slots, err := h.calendarService.GetSlotsForVersion(r.Context(), calendarVersionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not fetch slots")
		return
	}

	inputs := make([]availability.SetAvailabilityInput, 0, len(slots))
	for _, s := range slots {
		inputs = append(inputs, availability.SetAvailabilityInput{
			OrgID:      orgID,
			TeacherID:  teacherID,
			TimeslotID: s.ID,
			Available:  true,
		})
	}

	if err := h.availabilityRepo.BulkUpsert(r.Context(), inputs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "availability_set",
		"teacher_id":      teacherID,
		"slots_available": len(inputs),
	})
}

// POST /events/teacher.unassigned
func (h *Handler) OnTeacherUnassigned(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		OrgID             string   `json:"org_id"`
		TeacherID         string   `json:"teacher_id"`
		CalendarVersionID string   `json:"calendar_version_id"`
		SessionIDs        []string `json:"session_ids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	orgID, err := uuid.Parse(payload.OrgID)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid org_id"); return }
	teacherID, err := uuid.Parse(payload.TeacherID)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid teacher_id"); return }
	calendarVersionID, err := uuid.Parse(payload.CalendarVersionID)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid calendar_version_id"); return }

	slots, err := h.calendarService.GetSlotsForVersion(r.Context(), calendarVersionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not fetch slots")
		return
	}

	inputs := make([]availability.SetAvailabilityInput, 0, len(slots))
	for _, s := range slots {
		inputs = append(inputs, availability.SetAvailabilityInput{
			OrgID:      orgID,
			TeacherID:  teacherID,
			TimeslotID: s.ID,
			Available:  false,
		})
	}

	if err := h.availabilityRepo.BulkUpsert(r.Context(), inputs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	unscheduled := 0
	for _, sid := range payload.SessionIDs {
		sessionID, err := uuid.Parse(sid)
		if err != nil {
			continue
		}
		if err := h.schedulingService.Unschedule(r.Context(), sessionID); err != nil {
			log.Printf("events: teacher.unassigned: unschedule %s: %v", sessionID, err)
			continue
		}
		unscheduled++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "availability_cleared",
		"teacher_id":  teacherID,
		"unscheduled": unscheduled,
	})
}

// POST /events/org.calendar.updated
// Creates a new calendar version with fresh slots.
// Does NOT activate — admin activates explicitly via the calendar API.
func (h *Handler) OnOrgCalendarUpdated(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		OrgID               string                 `json:"org_id"`
		LearningDays        []string               `json:"learning_days"`
		DayStartTime        string                 `json:"day_start_time"`
		DayEndTime          string                 `json:"day_end_time"`
		SlotDurationMinutes int16                  `json:"slot_duration_minutes"`
		BreakStructure      []calendar.BreakWindow `json:"break_structure"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	orgID, err := uuid.Parse(payload.OrgID)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid org_id"); return }

	startTime, err := time.Parse("15:04", payload.DayStartTime)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid day_start_time"); return }
	endTime, err := time.Parse("15:04", payload.DayEndTime)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid day_end_time"); return }

	version, slots, err := h.calendarService.CreateCalendarWithSlots(r.Context(), orgID, &calendar.CreateCalendarInput{
		LearningDays:        payload.LearningDays,
		DayStartTime:        startTime,
		DayEndTime:          endTime,
		SlotDurationMinutes: payload.SlotDurationMinutes,
		BreakStructure:      payload.BreakStructure,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"status":             "version_created",
		"calendar_version":   version.ID,
		"version_number":     version.VersionNumber,
		"slots_generated":    len(slots),
		"note":               "activate via POST /orgs/:orgId/calendar/:versionId/activate",
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
