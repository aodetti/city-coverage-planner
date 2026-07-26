package planner

import (
	"math/rand"
	"testing"
	"time"
)

// halfHours lists every half hour the map slider can show, across the
// 08:00–20:00 patrol day.
func halfHours() []float64 {
	hours := make([]float64, 24)
	for index := range hours {
		hours[index] = 8 + 0.5*float64(index)
	}
	return hours
}

const numSeeds = 80

func makeWorkers(count int) []Worker {
	workers := make([]Worker, count)
	for index := 0; index < count; index++ {
		workers[index] = Worker{ID: index, FullName: "Worker " + string(rune('0'+index))}
	}
	return workers
}

func planDayForSeed(seed int) Day {
	rng := rand.New(rand.NewSource(int64(seed)))
	return PlanDay(rng, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), makeWorkers(8))
}

// blocksByZone returns {zone: set(block indices)} from the day's workers.
func blocksByZone(day Day) map[int]map[int]bool {
	zoneToBlocks := make(map[int]map[int]bool)
	for _, worker := range day.Workers {
		if _, alreadySeen := zoneToBlocks[worker.Zone]; !alreadySeen {
			blockSet := make(map[int]bool)
			for _, block := range worker.Blocks {
				blockSet[block] = true
			}
			zoneToBlocks[worker.Zone] = blockSet
		}
	}
	return zoneToBlocks
}

func activeWorkers(day Day, hour float64) []Assignment {
	var active []Assignment
	for _, worker := range day.Workers {
		if ActiveAt(worker.Shift, hour) {
			active = append(active, worker)
		}
	}
	return active
}

func intersects(blocks []int, blockSet map[int]bool) bool {
	for _, block := range blocks {
		if blockSet[block] {
			return true
		}
	}
	return false
}

// ── Central coverage ──────────────────────────────────────────────────────

func TestCentralSharedByTwoDistinctZonesEveryHour(t *testing.T) {
	centralBlocks := CentralBlocks()
	for seed := 0; seed < numSeeds; seed++ {
		day := planDayForSeed(seed)
		for _, hour := range halfHours() {
			zones := make(map[int]bool)
			for _, worker := range activeWorkers(day, hour) {
				if intersects(worker.Blocks, centralBlocks) {
					zones[worker.Zone] = true
				}
			}
			if len(zones) < 2 {
				t.Fatalf("centre held by <2 zones at %gh (seed %d)", hour, seed)
			}
		}
	}
}

func TestCentralFullyCoveredEveryHour(t *testing.T) {
	centralBlocks := CentralBlocks()
	for seed := 0; seed < numSeeds; seed++ {
		day := planDayForSeed(seed)
		for _, hour := range halfHours() {
			coveredBlocks := make(map[int]bool)
			for _, worker := range activeWorkers(day, hour) {
				for _, block := range worker.Blocks {
					if centralBlocks[block] {
						coveredBlocks[block] = true
					}
				}
			}
			if len(coveredBlocks) != len(centralBlocks) {
				t.Fatalf("centre not fully covered at %gh (seed %d): %d/%d", hour, seed, len(coveredBlocks), len(centralBlocks))
			}
		}
	}
}

func TestCentralSharedByTwoDistinctColoursEveryHour(t *testing.T) {
	centralBlocks := CentralBlocks()
	for seed := 0; seed < numSeeds; seed++ {
		day := planDayForSeed(seed)
		for _, hour := range halfHours() {
			colourByBlock := make(map[int]string)
			for _, worker := range activeWorkers(day, hour) {
				for _, block := range worker.Blocks {
					colourByBlock[block] = worker.Color
				}
			}
			colours := make(map[string]bool)
			for block := range centralBlocks {
				if colour, ok := colourByBlock[block]; ok {
					colours[colour] = true
				}
			}
			if len(colours) < 2 {
				t.Fatalf("centre shows <2 colours at %gh (seed %d)", hour, seed)
			}
		}
	}
}

func TestCentralConfinedToCentralZones(t *testing.T) {
	centralBlocks := CentralBlocks()
	for seed := 0; seed < numSeeds; seed++ {
		zoneToBlocks := blocksByZone(planDayForSeed(seed))
		for _, zone := range PeripheryZoneIDs {
			for block := range zoneToBlocks[zone] {
				if centralBlocks[block] {
					t.Fatalf("central block leaked into periphery zone %d (seed %d)", zone, seed)
				}
			}
		}
		for _, zone := range CentralZoneIDs {
			holdsCentral := false
			for block := range zoneToBlocks[zone] {
				if centralBlocks[block] {
					holdsCentral = true
					break
				}
			}
			if !holdsCentral {
				t.Fatalf("central zone %d holds no central block (seed %d)", zone, seed)
			}
		}
	}
}

// ── Fairness ──────────────────────────────────────────────────────────────

func TestWorkerBlockCountsWithinOne(t *testing.T) {
	for seed := 0; seed < numSeeds; seed++ {
		day := planDayForSeed(seed)
		highest, lowest := -1, -1
		for _, worker := range day.Workers {
			blockCount := len(worker.Blocks)
			if highest < 0 || blockCount > highest {
				highest = blockCount
			}
			if lowest < 0 || blockCount < lowest {
				lowest = blockCount
			}
		}
		if highest-lowest > 1 {
			t.Fatalf("unfair spread hi=%d lo=%d (seed %d)", highest, lowest, seed)
		}
	}
}

func TestEachZoneIsAboutAQuarter(t *testing.T) {
	lowerBound := NumBlocks / NumZones
	upperBound := (NumBlocks + NumZones - 1) / NumZones
	for seed := 0; seed < numSeeds; seed++ {
		zoneToBlocks := blocksByZone(planDayForSeed(seed))
		for zone, blocks := range zoneToBlocks {
			if len(blocks) < lowerBound || len(blocks) > upperBound {
				t.Fatalf("zone %d has %d blocks, expected %d-%d (seed %d)", zone, len(blocks), lowerBound, upperBound, seed)
			}
		}
	}
}

// ── Structure ─────────────────────────────────────────────────────────────

func TestFourZonesTileTheGrid(t *testing.T) {
	for seed := 0; seed < numSeeds; seed++ {
		zoneToBlocks := blocksByZone(planDayForSeed(seed))
		if len(zoneToBlocks) != 4 {
			t.Fatalf("expected 4 zones, got %d (seed %d)", len(zoneToBlocks), seed)
		}
		for _, zone := range []int{0, 1, 2, 3} {
			if _, ok := zoneToBlocks[zone]; !ok {
				t.Fatalf("missing zone %d (seed %d)", zone, seed)
			}
		}
		unionOfBlocks := make(map[int]bool)
		totalBlocks := 0
		for _, blocks := range zoneToBlocks {
			for block := range blocks {
				unionOfBlocks[block] = true
			}
			totalBlocks += len(blocks)
		}
		if len(unionOfBlocks) != NumBlocks {
			t.Fatalf("zones overlap or miss blocks: union=%d (seed %d)", len(unionOfBlocks), seed)
		}
		if totalBlocks != NumBlocks {
			t.Fatalf("zones overlap: total=%d (seed %d)", totalBlocks, seed)
		}
	}
}

func TestEveryZoneIsContiguous(t *testing.T) {
	for seed := 0; seed < numSeeds; seed++ {
		zoneToBlocks := blocksByZone(planDayForSeed(seed))
		for zone, blocks := range zoneToBlocks {
			if !Contiguous(blocks) {
				t.Fatalf("zone %d is not contiguous (seed %d)", zone, seed)
			}
		}
	}
}

func TestSixWorkersTwoPerShift(t *testing.T) {
	for seed := 0; seed < numSeeds; seed++ {
		day := planDayForSeed(seed)
		if len(day.Workers) != 6 {
			t.Fatalf("expected 6 workers, got %d (seed %d)", len(day.Workers), seed)
		}
		workersPerShift := make(map[string]int)
		for _, worker := range day.Workers {
			workersPerShift[worker.Shift]++
		}
		for _, shift := range []string{"A", "B", "C"} {
			if workersPerShift[shift] != 2 {
				t.Fatalf("shift %s has %d workers (seed %d)", shift, workersPerShift[shift], seed)
			}
		}
	}
}

func TestCentralZonesGoToShiftsAAndC(t *testing.T) {
	for seed := 0; seed < numSeeds; seed++ {
		day := planDayForSeed(seed)
		for _, zone := range CentralZoneIDs {
			var shifts []string
			for _, worker := range day.Workers {
				if worker.Zone == zone {
					shifts = append(shifts, worker.Shift)
				}
			}
			if len(shifts) != 2 {
				t.Fatalf("central zone %d has %d workers (seed %d)", zone, len(shifts), seed)
			}
			shiftPresent := map[string]bool{shifts[0]: true, shifts[1]: true}
			if !shiftPresent["A"] || !shiftPresent["C"] {
				t.Fatalf("central zone %d not A+C: %v (seed %d)", zone, shifts, seed)
			}
		}
		for _, zone := range PeripheryZoneIDs {
			for _, worker := range day.Workers {
				if worker.Zone == zone && worker.Shift != "B" {
					t.Fatalf("periphery zone %d not B-only (seed %d)", zone, seed)
				}
			}
		}
	}
}

// ── Generate ──────────────────────────────────────────────────────────────

func TestGenerateFullWeek(t *testing.T) {
	plan, generateErr := Generate(makeWorkers(6), "2026-01-05", 1)
	if generateErr != nil {
		t.Fatal(generateErr)
	}
	if len(plan.Days) != DaysPerWeek {
		t.Fatalf("expected %d days, got %d", DaysPerWeek, len(plan.Days))
	}
	if plan.ShiftsDef == nil {
		t.Fatal("missing shifts_def")
	}
	for _, day := range plan.Days {
		if len(day.Workers) != 6 {
			t.Fatalf("day %s has %d workers", day.Date, len(day.Workers))
		}
	}
}

func TestGenerateRequiresSixWorkers(t *testing.T) {
	if _, generateErr := Generate(makeWorkers(5), "2026-01-05", 1); generateErr == nil {
		t.Fatal("expected error for <6 workers")
	}
}
