// Package service holds the application's business logic, sitting between the
// HTTP layer (package api) and the persistence layer (package store). It owns
// the active data store — which can be swapped at runtime from the Settings
// page — the random source used to seed plans, and the orchestration of week
// planning and database switching. Handlers in package api stay thin: they
// decode requests, call a Service method, and encode the result, translating
// the sentinel errors below into HTTP status codes.
package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gridplanner/internal/config"
	"gridplanner/internal/planner"
	"gridplanner/internal/store"
)

// MinWorkers is the number of workers a week plan requires.
const MinWorkers = 6

// Sentinel errors returned by Service methods so the HTTP layer can map them to
// status codes without knowing the underlying reasons.
var (
	// ErrEmptyName is returned when a worker name is blank.
	ErrEmptyName = errors.New("full name is required")
	// ErrTooFewWorkers is returned when fewer than MinWorkers exist.
	ErrTooFewWorkers = errors.New("at least 6 workers are required to plan a week")
	// ErrInvalidWeekStart is returned when the week_start is not an ISO date.
	ErrInvalidWeekStart = errors.New("week_start must be an ISO date (YYYY-MM-DD)")
	// ErrWeekExists is returned when a plan already exists for the week.
	ErrWeekExists = errors.New("a plan for that week already exists")
)

// Service bundles the dependencies shared by every operation. The active store
// can be swapped at runtime; the mutex guards that swap.
type Service struct {
	mutex        sync.RWMutex
	activeStore  *store.Store
	randomSource *rand.Rand
}

// New creates a Service backed by the given initial store.
func New(initialStore *store.Store) *Service {
	return &Service{
		activeStore:  initialStore,
		randomSource: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// store returns the currently active data store.
func (service *Service) store() *store.Store {
	service.mutex.RLock()
	defer service.mutex.RUnlock()
	return service.activeStore
}

// randomSeed returns a fresh positive seed for a plan generation.
func (service *Service) randomSeed() int64 {
	return int64(service.randomSource.Intn(2_000_000_000) + 1)
}

// ── Workers ────────────────────────────────────────────────────────────

// ListWorkers returns all workers ordered by id.
func (service *Service) ListWorkers() []*store.Worker {
	return service.store().ListWorkers()
}

// GetWorker returns the worker with the given id, or store.ErrNotFound.
func (service *Service) GetWorker(workerID int) (*store.Worker, error) {
	return service.store().GetWorker(workerID)
}

// CreateWorker validates and stores a new worker. A blank name yields
// ErrEmptyName.
func (service *Service) CreateWorker(fullName string) (*store.Worker, error) {
	trimmedName := strings.TrimSpace(fullName)
	if trimmedName == "" {
		return nil, ErrEmptyName
	}
	return service.store().CreateWorker(trimmedName)
}

// DeleteWorker removes a worker, or returns store.ErrNotFound.
func (service *Service) DeleteWorker(workerID int) error {
	return service.store().DeleteWorker(workerID)
}

// workerPool converts stored workers into the planner's input shape.
func (service *Service) workerPool() []planner.Worker {
	workerRecords := service.store().ListWorkers()
	pool := make([]planner.Worker, 0, len(workerRecords))
	for _, record := range workerRecords {
		pool = append(pool, planner.Worker{ID: record.ID, FullName: record.FullName})
	}
	return pool
}

// ── Week plans ────────────────────────────────────────────────────────────

// ListWeeks returns all week plans ordered by week_start.
func (service *Service) ListWeeks() []*store.WeekPlan {
	return service.store().ListWeeks()
}

// GetWeek returns the plan with the given id, or store.ErrNotFound.
func (service *Service) GetWeek(weekID int) (*store.WeekPlan, error) {
	return service.store().GetWeek(weekID)
}

// DeleteWeek removes the plan with the given id, or returns store.ErrNotFound.
func (service *Service) DeleteWeek(weekID int) error {
	return service.store().DeleteWeek(weekID)
}

// CreateWeek generates and stores a plan for the week containing weekStart
// (normalised to its Monday). It fails with ErrInvalidWeekStart for a bad date,
// ErrWeekExists if a plan already covers the week, or ErrTooFewWorkers when
// there are not enough workers.
func (service *Service) CreateWeek(weekStart string) (*store.WeekPlan, error) {
	normalisedStart, mondayErr := mondayOf(weekStart)
	if mondayErr != nil {
		return nil, ErrInvalidWeekStart
	}
	if service.store().WeekByStart(normalisedStart) != nil {
		return nil, ErrWeekExists
	}
	pool := service.workerPool()
	if len(pool) < MinWorkers {
		return nil, ErrTooFewWorkers
	}
	seed := service.randomSeed()
	rawPlanJSON, generateErr := service.generatePlan(pool, normalisedStart, seed)
	if generateErr != nil {
		return nil, generateErr
	}
	createdWeek, createErr := service.store().CreateWeek(normalisedStart, seed, rawPlanJSON)
	if errors.Is(createErr, store.ErrConflict) {
		return nil, ErrWeekExists
	}
	if createErr != nil {
		return nil, createErr
	}
	return createdWeek, nil
}

// ReplanWeek regenerates the plan for an existing week, keeping its id but
// producing a new seed and data. Returns store.ErrNotFound for an unknown id or
// ErrTooFewWorkers when there are not enough workers.
func (service *Service) ReplanWeek(weekID int) (*store.WeekPlan, error) {
	weekPlan, getErr := service.store().GetWeek(weekID)
	if getErr != nil {
		return nil, getErr
	}
	pool := service.workerPool()
	if len(pool) < MinWorkers {
		return nil, ErrTooFewWorkers
	}
	seed := service.randomSeed()
	rawPlanJSON, generateErr := service.generatePlan(pool, weekPlan.WeekStart, seed)
	if generateErr != nil {
		return nil, generateErr
	}
	return service.store().UpdateWeek(weekID, seed, rawPlanJSON)
}

// generatePlan runs the planner and marshals the result to JSON.
func (service *Service) generatePlan(pool []planner.Worker, weekStart string, seed int64) (json.RawMessage, error) {
	weekData, generateErr := planner.Generate(pool, weekStart, seed)
	if generateErr != nil {
		return nil, generateErr
	}
	return json.Marshal(weekData)
}

// mondayOf normalises an ISO date string to the Monday of its week.
func mondayOf(weekStart string) (string, error) {
	parsedDate, parseErr := time.Parse("2006-01-02", weekStart)
	if parseErr != nil {
		return "", parseErr
	}
	daysSinceMonday := (int(parsedDate.Weekday()) + 6) % 7
	return parsedDate.AddDate(0, 0, -daysSinceMonday).Format("2006-01-02"), nil
}

// ── Settings / database switching ───────────────────────────────────────────

// DataPath returns the file path of the active data store.
func (service *Service) DataPath() string {
	return service.store().Path()
}

// DefaultDataPath returns the platform default database location.
func (service *Service) DefaultDataPath() (string, error) {
	return config.DefaultDataPath()
}

// SwitchDB points the application at a different database file and persists the
// choice. mode "new" creates an empty database (failing if a file already
// exists there); mode "open" loads an existing one (failing if it is missing).
// It returns the resolved absolute path.
func (service *Service) SwitchDB(rawPath, mode string) (string, error) {
	resolvedPath, resolveErr := resolveDBPath(rawPath)
	if resolveErr != nil {
		return "", resolveErr
	}
	switch mode {
	case "new":
		if _, statErr := os.Stat(resolvedPath); statErr == nil {
			return "", fmt.Errorf("A file already exists at %s. Use “open existing” to use it, or choose a different location.", resolvedPath)
		}
		if makeDirErr := os.MkdirAll(filepath.Dir(resolvedPath), 0o755); makeDirErr != nil {
			return "", makeDirErr
		}
	case "open":
		if _, statErr := os.Stat(resolvedPath); statErr != nil {
			return "", fmt.Errorf("No database file was found at %s.", resolvedPath)
		}
	default:
		return "", errors.New("mode must be “new” or “open”.")
	}

	newStore, openErr := store.Open(resolvedPath)
	if openErr != nil {
		return "", openErr
	}
	if saveErr := config.Save(&config.Settings{DataPath: resolvedPath}); saveErr != nil {
		return "", saveErr
	}

	service.mutex.Lock()
	service.activeStore = newStore
	service.mutex.Unlock()
	return resolvedPath, nil
}

// resolveDBPath validates the requested path and, if it points at an existing
// directory (or a path with no file extension), appends the default data.json
// filename.
func resolveDBPath(rawPath string) (string, error) {
	resolvedPath := expandHomeDir(strings.TrimSpace(rawPath))
	if resolvedPath == "" {
		return "", errors.New("Please provide a path for the database file.")
	}
	if !filepath.IsAbs(resolvedPath) {
		return "", errors.New("Please provide an absolute path (e.g. /Users/you/gridplanner/data.json).")
	}
	resolvedPath = filepath.Clean(resolvedPath)
	if fileInfo, statErr := os.Stat(resolvedPath); statErr == nil && fileInfo.IsDir() {
		resolvedPath = filepath.Join(resolvedPath, "data.json")
	} else if strings.EqualFold(filepath.Ext(resolvedPath), "") {
		// A non-existing path with no extension is treated as a folder.
		resolvedPath = filepath.Join(resolvedPath, "data.json")
	}
	return resolvedPath, nil
}

// expandHomeDir expands a leading ~ to the user's home directory.
func expandHomeDir(inputPath string) string {
	if inputPath == "~" || strings.HasPrefix(inputPath, "~/") {
		if homeDir, homeErr := os.UserHomeDir(); homeErr == nil {
			if inputPath == "~" {
				return homeDir
			}
			return filepath.Join(homeDir, inputPath[2:])
		}
	}
	return inputPath
}
