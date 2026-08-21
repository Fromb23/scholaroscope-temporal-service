package provisioning

import (
	"os"
	"strings"
	"testing"
)

func TestActorRoleMigrationDeduplicatesTeachersWithManyAssignments(t *testing.T) {
	source, err := os.ReadFile("../../migrations/000006_academic_years_actor_roles.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.Join(strings.Fields(strings.ToUpper(string(source))), " ")
	if !strings.Contains(normalized, "SELECT DISTINCT ETA.WORKSPACE_ID, ETA.TEACHER_UUID, 'TEACHER', 'ACTIVE'") {
		t.Fatal("teacher role migration must deduplicate assignment rows before ON CONFLICT")
	}
}

func TestProjectionIntegrityMigrationGuardsAllOccupancyDimensions(t *testing.T) {
	source, err := os.ReadFile("../../migrations/000007_solver_projection_integrity.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, index := range []string{"timetable_entry_occupancy_teacher_idx", "timetable_entry_occupancy_cohort_idx", "timetable_entry_occupancy_room_idx", "timetable_entry_occupancy_resource_idx"} {
		if !strings.Contains(text, index) {
			t.Fatalf("missing database occupancy guard %s", index)
		}
	}
}
