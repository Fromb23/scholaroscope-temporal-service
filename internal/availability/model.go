package availability

import (
	"time"

	"github.com/google/uuid"
)

type TeacherAvailability struct {
	ID          uuid.UUID `json:"id"`
	OrgID       uuid.UUID `json:"org_id"`
	TeacherID   uuid.UUID `json:"teacher_id"`
	TimeslotID  uuid.UUID `json:"timeslot_id"`
	IsAvailable bool      `json:"is_available"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SetAvailabilityInput is used for bulk upsert from kernel events.
type SetAvailabilityInput struct {
	OrgID      uuid.UUID
	TeacherID  uuid.UUID
	TimeslotID uuid.UUID
	Available  bool
}
