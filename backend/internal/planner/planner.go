// Package planner generates the weekly patrol plan.
//
// For each day we draw 6 workers from the pool and split them across the
// three shifts (2 each). The whole grid is split into four balanced, contiguous
// zones of roughly a quarter of the blocks each, so every worker patrols a
// similar amount. Two of those zones (0 and 1) each straddle the central
// district *and* reach into the periphery; each is handed to one shift-A
// worker and one shift-C worker, so the busy centre is always shared by
// two people (two colours on the map, never one) and stays covered even in the
// thin single-shift windows (08–10 has only shift A, 18–20 has only shift C).
// The two shift-B workers take the two periphery-only zones (2 and 3).
package planner

import (
	"encoding/json"
	"errors"
	"math/rand"
	"sort"
	"time"

	"gridplanner/internal/mapconfig"
)

// ShiftDef describes one shift's label and its active hour intervals.
type ShiftDef struct {
	Label     string   `json:"label"`
	Intervals [][2]int `json:"intervals"` // [start_hour, end_hour) in 24h
}

// Shifts are the three daily shift definitions.
var Shifts = map[string]ShiftDef{
	"A": {Label: "08–16", Intervals: [][2]int{{8, 16}}},
	"B": {Label: "10–18", Intervals: [][2]int{{10, 18}}},
	"C": {Label: "10–14 / 16–20", Intervals: [][2]int{{10, 14}, {16, 20}}},
}

// Colors are distinct, high-contrast colours (one per worker / zone, 6 total).
var Colors = []string{"#e6194B", "#3cb44b", "#4363d8", "#f58231", "#911eb4", "#469990"}

// WeekdaysES are Spanish weekday names, Monday-first.
var WeekdaysES = []string{"Lunes", "Martes", "Miércoles", "Jueves", "Viernes", "Sábado", "Domingo"}

// DaysPerWeek is the number of days planned per week.
const DaysPerWeek = 7

// Grid dimensions and the central district, mirrored from mapconfig. They are
// initialised from the embedded default map and refreshed by Configure once the
// active map is (re)loaded at startup.
var (
	Cols      = mapconfig.NumColumns
	Rows      = mapconfig.NumRows
	NumBlocks = mapconfig.NumBlocks

	// The central district must stay shared by AT LEAST TWO workers at every
	// hour of the day; the map file defines which block columns/rows it spans.
	centralColumns = mapconfig.CentralColumns
	centralRowList = mapconfig.CentralRows
)

// Configure refreshes the grid-derived values from mapconfig. Call it once at
// startup, after the active map has been loaded, so the planner matches the
// grid the frontend renders.
func Configure() {
	Cols = mapconfig.NumColumns
	Rows = mapconfig.NumRows
	NumBlocks = mapconfig.NumBlocks
	centralColumns = mapconfig.CentralColumns
	centralRowList = mapconfig.CentralRows
}

// Zone layout. Zones 0 and 1 straddle the centre and reach into the periphery;
// zones 2 and 3 are periphery-only. Each zone is about a quarter of the grid.
var (
	CentralZoneIDs   = []int{0, 1}
	PeripheryZoneIDs = []int{2, 3}
	NumZones         = 4
)

// Worker is the minimal input the planner needs about a person.
type Worker struct {
	ID       int
	FullName string
}

// Assignment is one worker's assignment for a day.
type Assignment struct {
	WorkerID   int    `json:"worker_id"`
	WorkerName string `json:"worker_name"`
	Shift      string `json:"shift"`
	ShiftLabel string `json:"shift_label"`
	Color      string `json:"color"`
	Zone       int    `json:"zone"`
	Blocks     []int  `json:"blocks"`
	Central    bool   `json:"central"`
}

// Road is one street segment or intersection, serialised as [type, r, c, zone].
type Road struct {
	Type   string // "v" vertical, "h" horizontal, "x" intersection
	Row    int
	Column int
	Zone   int
}

// MarshalJSON renders a Road as the compact [type, r, c, zone] array the
// frontend expects.
func (road Road) MarshalJSON() ([]byte, error) {
	return json.Marshal([]interface{}{road.Type, road.Row, road.Column, road.Zone})
}

// Day is a single day's plan.
type Day struct {
	Date    string       `json:"date"`
	Weekday string       `json:"weekday"`
	Workers []Assignment `json:"workers"`
	Roads   []Road       `json:"roads"`
}

// WeekData is a full week's generated plan.
type WeekData struct {
	WeekStart string              `json:"week_start"`
	Days      []Day               `json:"days"`
	ShiftsDef map[string]ShiftDef `json:"shifts_def"`
	Colors    []string            `json:"colors"`
}

// CentralBlocks returns the set of block indices forming the always-covered
// central district.
func CentralBlocks() map[int]bool {
	centralBlockSet := make(map[int]bool)
	for _, rowIndex := range centralRowList {
		for _, columnIndex := range centralColumns {
			centralBlockSet[rowIndex*Cols+columnIndex] = true
		}
	}
	return centralBlockSet
}

// neighbors returns the 4-connected grid neighbours of a block index.
func neighbors(blockIndex int) []int {
	rowIndex, columnIndex := blockIndex/Cols, blockIndex%Cols
	neighborIndices := make([]int, 0, 4)
	if rowIndex > 0 {
		neighborIndices = append(neighborIndices, blockIndex-Cols)
	}
	if rowIndex < Rows-1 {
		neighborIndices = append(neighborIndices, blockIndex+Cols)
	}
	if columnIndex > 0 {
		neighborIndices = append(neighborIndices, blockIndex-1)
	}
	if columnIndex < Cols-1 {
		neighborIndices = append(neighborIndices, blockIndex+1)
	}
	return neighborIndices
}

// Contiguous reports whether the set of block indices forms one 4-connected
// component.
func Contiguous(blocks map[int]bool) bool {
	if len(blocks) == 0 {
		return true
	}
	var startBlock int
	for block := range blocks {
		startBlock = block
		break
	}
	visited := map[int]bool{startBlock: true}
	stack := []int{startBlock}
	for len(stack) > 0 {
		currentBlock := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, neighborBlock := range neighbors(currentBlock) {
			if blocks[neighborBlock] && !visited[neighborBlock] {
				visited[neighborBlock] = true
				stack = append(stack, neighborBlock)
			}
		}
	}
	return len(visited) == len(blocks)
}

// sortedIntKeys returns the keys of a set sorted ascending.
func sortedIntKeys(set map[int]bool) []int {
	sortedValues := make([]int, 0, len(set))
	for key := range set {
		sortedValues = append(sortedValues, key)
	}
	sort.Ints(sortedValues)
	return sortedValues
}

// growBalancedZones region-grows k contiguous, roughly balanced zones over the
// whole grid, confining the central district to zones 0 and 1.
func growBalancedZones(rng *rand.Rand, blockCenters [][2]float64, centralBlocks map[int]bool) []int {
	blockCount := NumBlocks
	zoneCount := NumZones

	squaredDistance := func(firstBlock, secondBlock int) float64 {
		deltaX := blockCenters[firstBlock][0] - blockCenters[secondBlock][0]
		deltaY := blockCenters[firstBlock][1] - blockCenters[secondBlock][1]
		return deltaX*deltaX + deltaY*deltaY
	}

	// Two well-separated seeds inside the centre (zones 0, 1)...
	centralBlockList := sortedIntKeys(centralBlocks)
	firstCentralSeed := centralBlockList[rng.Intn(len(centralBlockList))]
	secondCentralSeed, bestSeedDistance := -1, -1.0
	for _, block := range centralBlockList {
		if block == firstCentralSeed {
			continue
		}
		if candidateDistance := squaredDistance(block, firstCentralSeed); secondCentralSeed < 0 || candidateDistance > bestSeedDistance {
			bestSeedDistance, secondCentralSeed = candidateDistance, block
		}
	}
	// ...and two periphery seeds, far from the centre and each other (2, 3).
	peripheryBlocks := make([]int, 0, blockCount)
	for block := 0; block < blockCount; block++ {
		if !centralBlocks[block] {
			peripheryBlocks = append(peripheryBlocks, block)
		}
	}
	thirdSeed := blockMaximizing(peripheryBlocks, func(block int) float64 {
		return min(squaredDistance(block, firstCentralSeed), squaredDistance(block, secondCentralSeed))
	})
	fourthSeed := blockMaximizing(peripheryBlocks, func(block int) float64 {
		return min(squaredDistance(block, firstCentralSeed), squaredDistance(block, secondCentralSeed), squaredDistance(block, thirdSeed))
	})
	zoneSeeds := []int{firstCentralSeed, secondCentralSeed, thirdSeed, fourthSeed}

	canAssign := func(zone, block int) bool { return !(zone >= 2 && centralBlocks[block]) }

	zoneOf := make([]int, blockCount)
	for block := range zoneOf {
		zoneOf[block] = -1
	}
	zoneSizes := make([]int, zoneCount)
	zoneFrontiers := make([]map[int]bool, zoneCount)
	for zone := range zoneFrontiers {
		zoneFrontiers[zone] = make(map[int]bool)
	}
	for zone, seed := range zoneSeeds {
		zoneOf[seed] = zone
		zoneSizes[zone] = 1
		for _, neighbor := range neighbors(seed) {
			if zoneOf[neighbor] == -1 && canAssign(zone, neighbor) {
				zoneFrontiers[zone][neighbor] = true
			}
		}
	}

	remainingBlocks := blockCount - zoneCount
	zoneOrder := []int{0, 1, 2, 3}
	for remainingBlocks > 0 {
		for zone := 0; zone < zoneCount; zone++ {
			prunedFrontier := make(map[int]bool)
			for block := range zoneFrontiers[zone] {
				if zoneOf[block] == -1 && canAssign(zone, block) {
					prunedFrontier[block] = true
				}
			}
			zoneFrontiers[zone] = prunedFrontier
		}
		rng.Shuffle(len(zoneOrder), func(first, second int) { zoneOrder[first], zoneOrder[second] = zoneOrder[second], zoneOrder[first] })
		smallestGrowableZone := -1
		for _, zone := range zoneOrder {
			if len(zoneFrontiers[zone]) > 0 && (smallestGrowableZone < 0 || zoneSizes[zone] < zoneSizes[smallestGrowableZone]) {
				smallestGrowableZone = zone
			}
		}
		if smallestGrowableZone < 0 {
			break
		}
		nearestBlock, nearestDistance := -1, 0.0
		for _, block := range sortedIntKeys(zoneFrontiers[smallestGrowableZone]) {
			candidateDistance := squaredDistance(block, zoneSeeds[smallestGrowableZone])
			if nearestBlock < 0 || candidateDistance < nearestDistance {
				nearestBlock, nearestDistance = block, candidateDistance
			}
		}
		zoneOf[nearestBlock] = smallestGrowableZone
		zoneSizes[smallestGrowableZone]++
		remainingBlocks--
		for _, neighbor := range neighbors(nearestBlock) {
			if zoneOf[neighbor] == -1 && canAssign(smallestGrowableZone, neighbor) {
				zoneFrontiers[smallestGrowableZone][neighbor] = true
			}
		}
	}

	// Stragglers: a central block joins its nearer central zone; anything else
	// joins any assigned neighbour.
	for block := 0; block < blockCount; block++ {
		if zoneOf[block] == -1 {
			if centralBlocks[block] {
				if squaredDistance(block, firstCentralSeed) <= squaredDistance(block, secondCentralSeed) {
					zoneOf[block] = 0
				} else {
					zoneOf[block] = 1
				}
			} else {
				fallbackZone := 0
				for _, neighbor := range neighbors(block) {
					if zoneOf[neighbor] != -1 {
						fallbackZone = zoneOf[neighbor]
						break
					}
				}
				zoneOf[block] = fallbackZone
			}
		}
	}
	return zoneOf
}

// blockMaximizing returns the element of candidates maximising score, keeping
// the first on ties.
func blockMaximizing(candidates []int, score func(int) float64) int {
	bestBlock, bestScore := -1, 0.0
	for _, candidate := range candidates {
		candidateScore := score(candidate)
		if bestBlock < 0 || candidateScore > bestScore {
			bestBlock, bestScore = candidate, candidateScore
		}
	}
	return bestBlock
}

// zoneAdjacency builds the zone-to-zone adjacency graph.
func zoneAdjacency(zoneOf []int, zoneCount int) []map[int]bool {
	adjacency := make([]map[int]bool, zoneCount)
	for zone := range adjacency {
		adjacency[zone] = make(map[int]bool)
	}
	for block := 0; block < len(zoneOf); block++ {
		for _, neighbor := range neighbors(block) {
			if zoneOf[neighbor] != zoneOf[block] {
				adjacency[zoneOf[block]][zoneOf[neighbor]] = true
			}
		}
	}
	return adjacency
}

// rebalance evens out zone sizes to within one block, keeping every zone
// contiguous. forbid(block, zone) (optional) blocks any move that would place
// block into zone; used to keep central blocks out of periphery-only zones.
func rebalance(rng *rand.Rand, zoneOf []int, zoneMembers []map[int]bool, zoneCount int, forbid func(block, zone int) bool) {
	blockCount := len(zoneOf)

	diffuse := func() {
		for iteration := 0; iteration < 10*blockCount; iteration++ {
			zoneSizes := make([]int, zoneCount)
			for zone := 0; zone < zoneCount; zone++ {
				zoneSizes[zone] = len(zoneMembers[zone])
			}
			type blockMove struct{ sizeGap, block, fromZone, toZone int }
			var candidateMoves []blockMove
			for block := 0; block < blockCount; block++ {
				fromZone := zoneOf[block]
				for _, neighbor := range neighbors(block) {
					toZone := zoneOf[neighbor]
					if toZone != fromZone && zoneSizes[fromZone]-zoneSizes[toZone] >= 2 && !(forbid != nil && forbid(block, toZone)) {
						candidateMoves = append(candidateMoves, blockMove{zoneSizes[fromZone] - zoneSizes[toZone], block, fromZone, toZone})
					}
				}
			}
			if len(candidateMoves) == 0 {
				return
			}
			sort.Slice(candidateMoves, func(first, second int) bool {
				left, right := candidateMoves[first], candidateMoves[second]
				if left.sizeGap != right.sizeGap {
					return left.sizeGap > right.sizeGap
				}
				if left.block != right.block {
					return left.block > right.block
				}
				if left.fromZone != right.fromZone {
					return left.fromZone > right.fromZone
				}
				return left.toZone > right.toZone
			})
			moved := false
			for _, candidateMove := range candidateMoves {
				delete(zoneMembers[candidateMove.fromZone], candidateMove.block)
				stillContiguous := Contiguous(zoneMembers[candidateMove.fromZone])
				zoneMembers[candidateMove.fromZone][candidateMove.block] = true // restore for the contiguity test
				if stillContiguous {
					delete(zoneMembers[candidateMove.fromZone], candidateMove.block)
					zoneMembers[candidateMove.toZone][candidateMove.block] = true
					zoneOf[candidateMove.block] = candidateMove.toZone
					moved = true
					break
				}
			}
			if !moved {
				return
			}
		}
	}

	chainFix := func() bool {
		zoneSizes := make([]int, zoneCount)
		for zone := 0; zone < zoneCount; zone++ {
			zoneSizes[zone] = len(zoneMembers[zone])
		}
		largest, smallest := zoneSizes[0], zoneSizes[0]
		for _, size := range zoneSizes {
			largest = max(largest, size)
			smallest = min(smallest, size)
		}
		if largest-smallest <= 1 {
			return false
		}
		var largestZones []int
		smallestZones := make(map[int]bool)
		for zone := 0; zone < zoneCount; zone++ {
			if zoneSizes[zone] == largest {
				largestZones = append(largestZones, zone)
			}
			if zoneSizes[zone] == smallest {
				smallestZones[zone] = true
			}
		}
		adjacency := zoneAdjacency(zoneOf, zoneCount)
		for _, sourceZone := range largestZones {
			cameFrom := map[int]int{sourceZone: -1}
			queue := []int{sourceZone}
			targetZone := -1
			for len(queue) > 0 {
				currentZone := queue[0]
				queue = queue[1:]
				if smallestZones[currentZone] && currentZone != sourceZone {
					targetZone = currentZone
					break
				}
				for _, adjacentZone := range sortedIntKeys(adjacency[currentZone]) {
					if _, alreadySeen := cameFrom[adjacentZone]; !alreadySeen {
						cameFrom[adjacentZone] = currentZone
						queue = append(queue, adjacentZone)
					}
				}
			}
			if targetZone < 0 {
				continue
			}
			var zonePath []int
			for zone := targetZone; zone != -1; zone = cameFrom[zone] {
				zonePath = append(zonePath, zone)
			}
			for first, second := 0, len(zonePath)-1; first < second; first, second = first+1, second-1 {
				zonePath[first], zonePath[second] = zonePath[second], zonePath[first]
			}

			membersSnapshot := make(map[int]map[int]bool)
			for _, zone := range zonePath {
				copiedMembers := make(map[int]bool, len(zoneMembers[zone]))
				for block := range zoneMembers[zone] {
					copiedMembers[block] = true
				}
				membersSnapshot[zone] = copiedMembers
			}
			savedAssignment := append([]int(nil), zoneOf...)

			pathFullyShifted := true
			for step := 0; step+1 < len(zonePath); step++ {
				fromZone, toZone := zonePath[step], zonePath[step+1]
				moved := false
				for _, block := range sortedIntKeys(zoneMembers[fromZone]) {
					touchesTarget := false
					for _, neighbor := range neighbors(block) {
						if zoneOf[neighbor] == toZone {
							touchesTarget = true
							break
						}
					}
					if !touchesTarget {
						continue
					}
					if forbid != nil && forbid(block, toZone) {
						continue
					}
					delete(zoneMembers[fromZone], block)
					stillContiguous := Contiguous(zoneMembers[fromZone])
					zoneMembers[fromZone][block] = true
					if stillContiguous {
						delete(zoneMembers[fromZone], block)
						zoneMembers[toZone][block] = true
						zoneOf[block] = toZone
						moved = true
						break
					}
				}
				if !moved {
					pathFullyShifted = false
					break
				}
			}
			if pathFullyShifted {
				return true
			}
			for _, zone := range zonePath {
				zoneMembers[zone] = membersSnapshot[zone]
			}
			copy(zoneOf, savedAssignment)
		}
		return false
	}

	for round := 0; round < zoneCount*zoneCount; round++ {
		diffuse()
		if !chainFix() {
			break
		}
	}
}

// partitionIntoBalancedZones splits the whole grid into four balanced,
// contiguous zones with the central district split across zones 0 and 1.
func partitionIntoBalancedZones(rng *rand.Rand, centralBlocks map[int]bool) []int {
	blockCenters := mapconfig.BlockCenters()
	forbid := func(block, zone int) bool { return zone >= 2 && centralBlocks[block] }
	var bestAssignment []int
	bestScore := -1
	for attempt := 0; attempt < 8; attempt++ {
		zoneOf := growBalancedZones(rng, blockCenters, centralBlocks)
		zoneMembers := make([]map[int]bool, NumZones)
		for zone := range zoneMembers {
			zoneMembers[zone] = make(map[int]bool)
		}
		for block, zone := range zoneOf {
			zoneMembers[zone][block] = true
		}
		rebalance(rng, zoneOf, zoneMembers, NumZones, forbid)
		zoneSizes := make([]int, NumZones)
		for zone := 0; zone < NumZones; zone++ {
			zoneSizes[zone] = len(zoneMembers[zone])
		}
		largestSize := zoneSizes[0]
		smallestSize := zoneSizes[0]
		for _, size := range zoneSizes {
			largestSize = max(largestSize, size)
			smallestSize = min(smallestSize, size)
		}
		sizeSpread := largestSize - smallestSize
		bothCentralZonesCovered := zoneHasCentral(zoneMembers[0], centralBlocks) && zoneHasCentral(zoneMembers[1], centralBlocks)
		score := sizeSpread
		if !bothCentralZonesCovered {
			score += 1000
		}
		if bestScore < 0 || score < bestScore {
			bestScore = score
			bestAssignment = append([]int(nil), zoneOf...)
		}
		if sizeSpread <= 1 && bothCentralZonesCovered {
			break
		}
	}
	return bestAssignment
}

func zoneHasCentral(zoneMembers, centralBlocks map[int]bool) bool {
	for block := range zoneMembers {
		if centralBlocks[block] {
			return true
		}
	}
	return false
}

func zoneAtRowCol(zoneOf []int, rowIndex, columnIndex int) (int, bool) {
	if rowIndex >= 0 && rowIndex < Rows && columnIndex >= 0 && columnIndex < Cols {
		return zoneOf[rowIndex*Cols+columnIndex], true
	}
	return 0, false
}

// assignRoads assigns every street segment and intersection to exactly one zone.
func assignRoads(rng *rand.Rand, zoneOf []int) []Road {
	zoneRoadCounts := make(map[int]int)
	roads := []Road{}
	verticalOwner := make(map[[2]int]int)
	horizontalOwner := make(map[[2]int]int)

	chooseZone := func(candidateZones map[int]bool) int {
		candidateList := sortedIntKeys(candidateZones)
		if len(candidateList) > 1 {
			rng.Shuffle(len(candidateList), func(first, second int) {
				candidateList[first], candidateList[second] = candidateList[second], candidateList[first]
			})
			sort.SliceStable(candidateList, func(first, second int) bool {
				return zoneRoadCounts[candidateList[first]] < zoneRoadCounts[candidateList[second]]
			})
		}
		chosenZone := candidateList[0]
		zoneRoadCounts[chosenZone]++
		return chosenZone
	}

	for columnIndex := 0; columnIndex <= Cols; columnIndex++ {
		for rowIndex := 0; rowIndex < Rows; rowIndex++ {
			candidateZones := make(map[int]bool)
			if zone, ok := zoneAtRowCol(zoneOf, rowIndex, columnIndex-1); ok {
				candidateZones[zone] = true
			}
			if zone, ok := zoneAtRowCol(zoneOf, rowIndex, columnIndex); ok {
				candidateZones[zone] = true
			}
			if len(candidateZones) > 0 {
				chosenZone := chooseZone(candidateZones)
				verticalOwner[[2]int{rowIndex, columnIndex}] = chosenZone
				roads = append(roads, Road{"v", rowIndex, columnIndex, chosenZone})
			}
		}
	}

	for rowIndex := 0; rowIndex <= Rows; rowIndex++ {
		for columnIndex := 0; columnIndex < Cols; columnIndex++ {
			candidateZones := make(map[int]bool)
			if zone, ok := zoneAtRowCol(zoneOf, rowIndex-1, columnIndex); ok {
				candidateZones[zone] = true
			}
			if zone, ok := zoneAtRowCol(zoneOf, rowIndex, columnIndex); ok {
				candidateZones[zone] = true
			}
			if len(candidateZones) > 0 {
				chosenZone := chooseZone(candidateZones)
				horizontalOwner[[2]int{rowIndex, columnIndex}] = chosenZone
				roads = append(roads, Road{"h", rowIndex, columnIndex, chosenZone})
			}
		}
	}

	for rowIndex := 0; rowIndex <= Rows; rowIndex++ {
		for columnIndex := 0; columnIndex <= Cols; columnIndex++ {
			candidateZones := make(map[int]bool)
			if zone, ok := verticalOwner[[2]int{rowIndex - 1, columnIndex}]; ok {
				candidateZones[zone] = true
			}
			if zone, ok := verticalOwner[[2]int{rowIndex, columnIndex}]; ok {
				candidateZones[zone] = true
			}
			if zone, ok := horizontalOwner[[2]int{rowIndex, columnIndex - 1}]; ok {
				candidateZones[zone] = true
			}
			if zone, ok := horizontalOwner[[2]int{rowIndex, columnIndex}]; ok {
				candidateZones[zone] = true
			}
			if len(candidateZones) > 0 {
				roads = append(roads, Road{"x", rowIndex, columnIndex, chooseZone(candidateZones)})
			}
		}
	}

	return roads
}

// sampleWorkers draws n distinct workers uniformly at random.
func sampleWorkers(rng *rand.Rand, workers []Worker, count int) []Worker {
	chosenIndices := rng.Perm(len(workers))[:count]
	sampled := make([]Worker, count)
	for position, workerIndex := range chosenIndices {
		sampled[position] = workers[workerIndex]
	}
	return sampled
}

func shuffleWorkers(rng *rand.Rand, workers []Worker) {
	rng.Shuffle(len(workers), func(first, second int) { workers[first], workers[second] = workers[second], workers[first] })
}

func weekdayIndex(date time.Time) int {
	// Go: Sunday=0..Saturday=6; we want Monday=0.
	return (int(date.Weekday()) + 6) % 7
}

// PlanDay builds one day's plan: shift assignment plus zones for 6 workers.
func PlanDay(rng *rand.Rand, dayDate time.Time, workers []Worker) Day {
	chosenWorkers := sampleWorkers(rng, workers, 6)
	shuffleWorkers(rng, chosenWorkers)
	shiftAWorkers := []Worker{chosenWorkers[0], chosenWorkers[1]}
	shiftBWorkers := []Worker{chosenWorkers[2], chosenWorkers[3]}
	shiftCWorkers := []Worker{chosenWorkers[4], chosenWorkers[5]}

	centralBlocks := CentralBlocks()
	zoneOf := partitionIntoBalancedZones(rng, centralBlocks)

	blocksByZone := make([][]int, NumZones)
	for block := 0; block < NumBlocks; block++ {
		blocksByZone[zoneOf[block]] = append(blocksByZone[zoneOf[block]], block)
	}

	roads := assignRoads(rng, zoneOf)

	shiftAOrder := append([]Worker(nil), shiftAWorkers...)
	shuffleWorkers(rng, shiftAOrder)
	shiftCOrder := append([]Worker(nil), shiftCWorkers...)
	shuffleWorkers(rng, shiftCOrder)
	centralZone0, centralZone1 := CentralZoneIDs[0], CentralZoneIDs[1]
	peripheryZone0, peripheryZone1 := PeripheryZoneIDs[0], PeripheryZoneIDs[1]

	assignments := []struct {
		worker Worker
		shift  string
		zone   int
	}{
		{shiftAOrder[0], "A", centralZone0},
		{shiftAOrder[1], "A", centralZone1},
		{shiftBWorkers[0], "B", peripheryZone0},
		{shiftBWorkers[1], "B", peripheryZone1},
		{shiftCOrder[0], "C", centralZone0},
		{shiftCOrder[1], "C", centralZone1},
	}

	dayAssignments := make([]Assignment, 0, len(assignments))
	for assignmentIndex, assignment := range assignments {
		isCentral := assignment.zone == CentralZoneIDs[0] || assignment.zone == CentralZoneIDs[1]
		dayAssignments = append(dayAssignments, Assignment{
			WorkerID:   assignment.worker.ID,
			WorkerName: assignment.worker.FullName,
			Shift:      assignment.shift,
			ShiftLabel: Shifts[assignment.shift].Label,
			Color:      Colors[assignmentIndex],
			Zone:       assignment.zone,
			Blocks:     blocksByZone[assignment.zone],
			Central:    isCentral,
		})
	}

	return Day{
		Date:    dayDate.Format("2006-01-02"),
		Weekday: WeekdaysES[weekdayIndex(dayDate)],
		Workers: dayAssignments,
		Roads:   roads,
	}
}

// Generate builds a full week plan. workers must have >= 6 entries;
// weekStart is the ISO date of the Monday; seed drives reproducible randomness.
func Generate(workers []Worker, weekStart string, seed int64) (*WeekData, error) {
	if len(workers) < 6 {
		return nil, errors.New("Need at least 6 workers to build a plan.")
	}
	rng := rand.New(rand.NewSource(seed))
	weekStartDate, parseErr := time.Parse("2006-01-02", weekStart)
	if parseErr != nil {
		return nil, parseErr
	}
	days := make([]Day, 0, DaysPerWeek)
	for dayOffset := 0; dayOffset < DaysPerWeek; dayOffset++ {
		dayDate := weekStartDate.AddDate(0, 0, dayOffset)
		days = append(days, PlanDay(rng, dayDate, workers))
	}
	return &WeekData{
		WeekStart: weekStart,
		Days:      days,
		ShiftsDef: Shifts,
		Colors:    Colors,
	}, nil
}

// ActiveAt reports whether a shift is on the street at the given (float) hour.
func ActiveAt(shiftKey string, hour float64) bool {
	for _, interval := range Shifts[shiftKey].Intervals {
		if float64(interval[0]) <= hour && hour < float64(interval[1]) {
			return true
		}
	}
	return false
}
