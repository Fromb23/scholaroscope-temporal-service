package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"scholaroscope-temporal-service/internal/scheduling"
)

func main() {
	profileFlag := flag.String("profiles", "all", "comma-separated profile names or all")
	seedFlag := flag.String("seeds", "7,23,101", "comma-separated deterministic seeds")
	flag.Parse()
	profiles := []scheduling.SimulationProfile{
		{Name: "individual-32", Teachers: 1, Cohorts: 1, Subjects: 8, Days: 4, PeriodsPerDay: 8},
		{Name: "small-10", Teachers: 10, Cohorts: 9, Subjects: 16, Days: 4, PeriodsPerDay: 8},
		{Name: "medium-50", Teachers: 50, Cohorts: 47, Subjects: 50, Days: 4, PeriodsPerDay: 8},
		{Name: "large-200", Teachers: 200, Cohorts: 188, Subjects: 60, Days: 4, PeriodsPerDay: 8},
		{Name: "very-large-300", Teachers: 300, Cohorts: 282, Subjects: 60, Days: 4, PeriodsPerDay: 8},
		{Name: "double-lessons", Teachers: 50, Cohorts: 45, Subjects: 50, Days: 4, PeriodsPerDay: 8, DoubleLessons: true},
	}
	selected := map[string]bool{}
	for _, name := range strings.Split(*profileFlag, ",") {
		selected[strings.TrimSpace(name)] = true
	}
	seeds := []int64{}
	for _, raw := range strings.Split(*seedFlag, ",") {
		var seed int64
		if _, err := fmt.Sscan(strings.TrimSpace(raw), &seed); err != nil {
			fail("invalid seed %q", raw)
		}
		seeds = append(seeds, seed)
	}
	encoder := json.NewEncoder(os.Stdout)
	for _, profile := range profiles {
		if !selected["all"] && !selected[profile.Name] {
			continue
		}
		for _, seed := range seeds {
			observation, result := scheduling.RunSimulation(profile, scheduling.EngineConfig{Seed: seed, TimeBudget: 30 * time.Second, IterationBudget: 5_000_000, Restarts: 4, MaxConsecutive: 8})
			if err := encoder.Encode(observation); err != nil {
				fail("encode: %v", err)
			}
			if !result.Validation.Valid || result.Validation.HardConflictCount != 0 || result.Validation.Unscheduled != 0 {
				fail("%s seed %d failed: status=%s hard=%d unscheduled=%d", profile.Name, seed, result.Status, result.Validation.HardConflictCount, result.Validation.Unscheduled)
			}
		}
	}
}

func fail(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
