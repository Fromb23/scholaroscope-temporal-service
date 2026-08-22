BEGIN;

DROP TABLE IF EXISTS timetable_demand_override;

ALTER TABLE time_slot DROP CONSTRAINT IF EXISTS time_slot_type_check;
ALTER TABLE time_slot
    ADD CONSTRAINT time_slot_type_check
    CHECK (slot_type IN ('LESSON', 'BREAK', 'LUNCH', 'ASSEMBLY', 'NON_TEACHING', 'PREP', 'EXAM'));

COMMIT;
