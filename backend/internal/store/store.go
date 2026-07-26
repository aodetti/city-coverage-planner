// Package store persists workers and week plans to a single JSON file in the
// user's per-OS application-data directory. It replaces the original SQLite
// database with a dependency-free, human-readable file so the whole app ships
// as one static binary.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Worker is a stored worker record.
type Worker struct {
	ID        int       `json:"id"`
	FullName  string    `json:"full_name"`
	CreatedAt time.Time `json:"created_at"`
}

// WeekPlan is a stored week plan. Data holds the generated plan JSON verbatim.
type WeekPlan struct {
	ID        int             `json:"id"`
	WeekStart string          `json:"week_start"`
	Seed      int64           `json:"seed"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a unique constraint would be violated.
var ErrConflict = errors.New("conflict")

// storeContents is the on-disk shape of the whole store.
type storeContents struct {
	Workers      []*Worker   `json:"workers"`
	Weeks        []*WeekPlan `json:"weeks"`
	NextWorkerID int         `json:"next_worker_id"`
	NextWeekID   int         `json:"next_week_id"`
}

// Store is a thread-safe JSON-file-backed data store.
type Store struct {
	mutex    sync.Mutex
	filePath string
	contents storeContents
}

// Open loads the store at filePath, creating an empty one if the file is absent.
func Open(filePath string) (*Store, error) {
	newStore := &Store{filePath: filePath}
	newStore.contents = storeContents{NextWorkerID: 1, NextWeekID: 1}
	rawJSON, readErr := os.ReadFile(filePath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return newStore, newStore.save()
		}
		return nil, readErr
	}
	if len(rawJSON) > 0 {
		if unmarshalErr := json.Unmarshal(rawJSON, &newStore.contents); unmarshalErr != nil {
			return nil, unmarshalErr
		}
	}
	if newStore.contents.NextWorkerID == 0 {
		newStore.contents.NextWorkerID = 1
	}
	if newStore.contents.NextWeekID == 0 {
		newStore.contents.NextWeekID = 1
	}
	return newStore, nil
}

// Path returns the file this store is backed by.
func (store *Store) Path() string {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.filePath
}

// save writes the store atomically (temp file + rename). It re-creates the
// containing directory first, so a save still succeeds if the folder was
// removed while the program was running.
func (store *Store) save() error {
	if makeDirErr := os.MkdirAll(filepath.Dir(store.filePath), 0o755); makeDirErr != nil {
		return makeDirErr
	}
	rawJSON, marshalErr := json.MarshalIndent(&store.contents, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	tempFilePath := store.filePath + ".tmp"
	if writeErr := os.WriteFile(tempFilePath, rawJSON, 0o644); writeErr != nil {
		return writeErr
	}
	return os.Rename(tempFilePath, store.filePath)
}

// ── Workers ────────────────────────────────────────────────────────────

// ListWorkers returns all workers ordered by id.
func (store *Store) ListWorkers() []*Worker {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	sortedWorkers := append([]*Worker(nil), store.contents.Workers...)
	sort.Slice(sortedWorkers, func(first, second int) bool {
		return sortedWorkers[first].ID < sortedWorkers[second].ID
	})
	return sortedWorkers
}

// CreateWorker adds a new worker.
func (store *Store) CreateWorker(fullName string) (*Worker, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	worker := &Worker{ID: store.contents.NextWorkerID, FullName: fullName, CreatedAt: time.Now().UTC()}
	store.contents.NextWorkerID++
	store.contents.Workers = append(store.contents.Workers, worker)
	if saveErr := store.save(); saveErr != nil {
		return nil, saveErr
	}
	return worker, nil
}

// GetWorker returns the worker with the given id.
func (store *Store) GetWorker(workerID int) (*Worker, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for _, worker := range store.contents.Workers {
		if worker.ID == workerID {
			return worker, nil
		}
	}
	return nil, ErrNotFound
}

// DeleteWorker removes the worker with the given id.
func (store *Store) DeleteWorker(workerID int) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for index, worker := range store.contents.Workers {
		if worker.ID == workerID {
			store.contents.Workers = append(store.contents.Workers[:index], store.contents.Workers[index+1:]...)
			return store.save()
		}
	}
	return ErrNotFound
}

// ── Week plans ────────────────────────────────────────────────────────────

// ListWeeks returns all week plans ordered by week_start.
func (store *Store) ListWeeks() []*WeekPlan {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	sortedWeeks := append([]*WeekPlan(nil), store.contents.Weeks...)
	sort.Slice(sortedWeeks, func(first, second int) bool {
		return sortedWeeks[first].WeekStart < sortedWeeks[second].WeekStart
	})
	return sortedWeeks
}

// GetWeek returns the week plan with the given id.
func (store *Store) GetWeek(weekID int) (*WeekPlan, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for _, weekPlan := range store.contents.Weeks {
		if weekPlan.ID == weekID {
			return weekPlan, nil
		}
	}
	return nil, ErrNotFound
}

// WeekByStart returns the plan for a given Monday, or nil if none exists.
func (store *Store) WeekByStart(weekStart string) *WeekPlan {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for _, weekPlan := range store.contents.Weeks {
		if weekPlan.WeekStart == weekStart {
			return weekPlan
		}
	}
	return nil
}

// CreateWeek stores a new plan; returns ErrConflict if week_start is taken.
func (store *Store) CreateWeek(weekStart string, seed int64, planData json.RawMessage) (*WeekPlan, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for _, weekPlan := range store.contents.Weeks {
		if weekPlan.WeekStart == weekStart {
			return nil, ErrConflict
		}
	}
	newWeekPlan := &WeekPlan{ID: store.contents.NextWeekID, WeekStart: weekStart, Seed: seed, Data: planData, CreatedAt: time.Now().UTC()}
	store.contents.NextWeekID++
	store.contents.Weeks = append(store.contents.Weeks, newWeekPlan)
	if saveErr := store.save(); saveErr != nil {
		return nil, saveErr
	}
	return newWeekPlan, nil
}

// UpdateWeek replaces the seed and data of an existing plan (used by replan).
func (store *Store) UpdateWeek(weekID int, seed int64, planData json.RawMessage) (*WeekPlan, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for _, weekPlan := range store.contents.Weeks {
		if weekPlan.ID == weekID {
			weekPlan.Seed = seed
			weekPlan.Data = planData
			if saveErr := store.save(); saveErr != nil {
				return nil, saveErr
			}
			return weekPlan, nil
		}
	}
	return nil, ErrNotFound
}

// DeleteWeek removes the plan with the given id.
func (store *Store) DeleteWeek(weekID int) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for index, weekPlan := range store.contents.Weeks {
		if weekPlan.ID == weekID {
			store.contents.Weeks = append(store.contents.Weeks[:index], store.contents.Weeks[index+1:]...)
			return store.save()
		}
	}
	return ErrNotFound
}
