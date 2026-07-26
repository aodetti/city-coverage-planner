package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"

	"gridplanner/internal/store"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	testStore, openErr := store.Open(filepath.Join(t.TempDir(), "data.json"))
	if openErr != nil {
		t.Fatalf("open store: %v", openErr)
	}
	frontendFS := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><title>app</title>")},
	}
	return NewRouter(testStore, frontendFS)
}

func sendRequest(t *testing.T, handler http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var bodyBuffer bytes.Buffer
	if body != nil {
		if encodeErr := json.NewEncoder(&bodyBuffer).Encode(body); encodeErr != nil {
			t.Fatal(encodeErr)
		}
	}
	request := httptest.NewRequest(method, path, &bodyBuffer)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestFullAPIFlow(t *testing.T) {
	handler := newTestServer(t)

	// Map config.
	recorder := sendRequest(t, handler, "GET", "/api/map", nil)
	if recorder.Code != 200 {
		t.Fatalf("GET /api/map = %d", recorder.Code)
	}
	var mapPayload struct {
		Cols      int `json:"cols"`
		Rows      int `json:"rows"`
		NumBlocks int `json:"num_blocks"`
	}
	if unmarshalErr := json.Unmarshal(recorder.Body.Bytes(), &mapPayload); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	// Map-agnostic: the grid must be non-empty and internally consistent
	// (num_blocks = cols * rows), whatever map is embedded.
	if mapPayload.Cols <= 0 || mapPayload.Rows <= 0 {
		t.Fatalf("empty grid: cols=%d rows=%d", mapPayload.Cols, mapPayload.Rows)
	}
	if mapPayload.NumBlocks != mapPayload.Cols*mapPayload.Rows {
		t.Fatalf("num_blocks = %d, want cols*rows = %d", mapPayload.NumBlocks, mapPayload.Cols*mapPayload.Rows)
	}

	// Empty worker list.
	recorder = sendRequest(t, handler, "GET", "/api/workers", nil)
	if recorder.Code != 200 || recorder.Body.String() != "[]\n" {
		t.Fatalf("empty workers = %d %q", recorder.Code, recorder.Body.String())
	}

	// Create 6 workers.
	var workerIDs []int
	for index := 0; index < 6; index++ {
		recorder = sendRequest(t, handler, "POST", "/api/workers", map[string]string{"full_name": "Insp " + string(rune('A'+index))})
		if recorder.Code != 201 {
			t.Fatalf("create worker = %d", recorder.Code)
		}
		var created struct {
			ID       int    `json:"id"`
			FullName string `json:"full_name"`
		}
		json.Unmarshal(recorder.Body.Bytes(), &created)
		workerIDs = append(workerIDs, created.ID)
	}

	// Blank name rejected.
	recorder = sendRequest(t, handler, "POST", "/api/workers", map[string]string{"full_name": "   "})
	if recorder.Code != 400 {
		t.Fatalf("blank name = %d, want 400", recorder.Code)
	}

	// Create a week (Tuesday input should normalise to Monday).
	recorder = sendRequest(t, handler, "POST", "/api/weeks", map[string]string{"week_start": "2026-01-06"})
	if recorder.Code != 201 {
		t.Fatalf("create week = %d: %s", recorder.Code, recorder.Body.String())
	}
	var week struct {
		ID        int    `json:"id"`
		WeekStart string `json:"week_start"`
		Data      struct {
			Days []struct {
				Workers []struct {
					WorkerID int `json:"worker_id"`
				} `json:"workers"`
			} `json:"days"`
		} `json:"data"`
	}
	if unmarshalErr := json.Unmarshal(recorder.Body.Bytes(), &week); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	if week.WeekStart != "2026-01-05" {
		t.Fatalf("week_start = %s, want 2026-01-05 (Monday)", week.WeekStart)
	}
	if len(week.Data.Days) != 7 {
		t.Fatalf("days = %d, want 7", len(week.Data.Days))
	}
	for _, day := range week.Data.Days {
		if len(day.Workers) != 6 {
			t.Fatalf("workers = %d, want 6", len(day.Workers))
		}
	}

	// Duplicate week -> 409.
	recorder = sendRequest(t, handler, "POST", "/api/weeks", map[string]string{"week_start": "2026-01-05"})
	if recorder.Code != 409 {
		t.Fatalf("duplicate week = %d, want 409", recorder.Code)
	}

	// Get week.
	recorder = sendRequest(t, handler, "GET", "/api/weeks/1", nil)
	if recorder.Code != 200 {
		t.Fatalf("get week = %d", recorder.Code)
	}

	// Replan changes the plan seed/data but keeps the id.
	recorder = sendRequest(t, handler, "POST", "/api/weeks/1/replan", nil)
	if recorder.Code != 200 {
		t.Fatalf("replan = %d", recorder.Code)
	}

	// Worker schedule.
	recorder = sendRequest(t, handler, "GET", "/api/workers/"+intToString(workerIDs[0])+"/schedule", nil)
	if recorder.Code != 200 {
		t.Fatalf("schedule = %d", recorder.Code)
	}
	var schedule struct {
		Weeks []struct {
			DaysWorked int `json:"days_worked"`
			Days       []struct {
				Assignment *struct {
					Shift string `json:"shift"`
				} `json:"assignment"`
			} `json:"days"`
		} `json:"weeks"`
	}
	if unmarshalErr := json.Unmarshal(recorder.Body.Bytes(), &schedule); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	if len(schedule.Weeks) != 1 {
		t.Fatalf("schedule weeks = %d, want 1", len(schedule.Weeks))
	}

	// List weeks.
	recorder = sendRequest(t, handler, "GET", "/api/weeks", nil)
	if recorder.Code != 200 {
		t.Fatalf("list weeks = %d", recorder.Code)
	}

	// Delete worker 404 for unknown id.
	recorder = sendRequest(t, handler, "DELETE", "/api/workers/9999", nil)
	if recorder.Code != 404 {
		t.Fatalf("delete unknown worker = %d, want 404", recorder.Code)
	}

	// Delete a real worker.
	recorder = sendRequest(t, handler, "DELETE", "/api/workers/"+intToString(workerIDs[5]), nil)
	if recorder.Code != 204 {
		t.Fatalf("delete worker = %d, want 204", recorder.Code)
	}

	// Delete week.
	recorder = sendRequest(t, handler, "DELETE", "/api/weeks/1", nil)
	if recorder.Code != 204 {
		t.Fatalf("delete week = %d, want 204", recorder.Code)
	}

	// SPA fallback serves index.html for unknown routes.
	recorder = sendRequest(t, handler, "GET", "/some/client/route", nil)
	if recorder.Code != 200 {
		t.Fatalf("spa fallback = %d", recorder.Code)
	}
}

func TestSettingsSwitchDB(t *testing.T) {
	// Redirect the OS config dir so switching the DB writes the pointer file
	// under a temp folder instead of the real user configuration.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))

	handler := newTestServer(t)
	workDir := t.TempDir()

	// Add a worker to the initial DB.
	sendRequest(t, handler, "POST", "/api/workers", map[string]string{"full_name": "Original"})

	// Current settings expose the active data path.
	recorder := sendRequest(t, handler, "GET", "/api/settings", nil)
	if recorder.Code != 200 {
		t.Fatalf("GET /api/settings = %d", recorder.Code)
	}

	// Create a brand-new empty DB in a fresh folder.
	newPath := filepath.Join(workDir, "fresh", "data.json")
	recorder = sendRequest(t, handler, "POST", "/api/settings/db", map[string]string{"path": newPath, "mode": "new"})
	if recorder.Code != 200 {
		t.Fatalf("new DB = %d: %s", recorder.Code, recorder.Body.String())
	}

	// The new DB is empty — the Original worker must not be there.
	recorder = sendRequest(t, handler, "GET", "/api/workers", nil)
	if recorder.Body.String() != "[]\n" {
		t.Fatalf("new DB not empty: %s", recorder.Body.String())
	}

	// Creating "new" again at the same path must fail (file exists).
	recorder = sendRequest(t, handler, "POST", "/api/settings/db", map[string]string{"path": newPath, "mode": "new"})
	if recorder.Code != 400 {
		t.Fatalf("re-create existing = %d, want 400", recorder.Code)
	}

	// Add a worker to the new DB, then reopen it with mode "open".
	sendRequest(t, handler, "POST", "/api/workers", map[string]string{"full_name": "Fresh"})
	recorder = sendRequest(t, handler, "POST", "/api/settings/db", map[string]string{"path": newPath, "mode": "open"})
	if recorder.Code != 200 {
		t.Fatalf("open existing = %d: %s", recorder.Code, recorder.Body.String())
	}
	recorder = sendRequest(t, handler, "GET", "/api/workers", nil)
	if !bytes.Contains(recorder.Body.Bytes(), []byte("Fresh")) {
		t.Fatalf("reopened DB missing data: %s", recorder.Body.String())
	}

	// Opening a non-existent DB must fail.
	recorder = sendRequest(t, handler, "POST", "/api/settings/db", map[string]string{"path": filepath.Join(workDir, "nope", "data.json"), "mode": "open"})
	if recorder.Code != 400 {
		t.Fatalf("open missing = %d, want 400", recorder.Code)
	}

	// A relative path is rejected.
	recorder = sendRequest(t, handler, "POST", "/api/settings/db", map[string]string{"path": "relative/data.json", "mode": "new"})
	if recorder.Code != 400 {
		t.Fatalf("relative path = %d, want 400", recorder.Code)
	}
}

func TestStaticContentTypes(t *testing.T) {
	testStore, openErr := store.Open(filepath.Join(t.TempDir(), "data.json"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	frontendFS := fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>app</title>")},
		"assets/index-abc.js":  {Data: []byte("export const x = 1;")},
		"assets/index-abc.css": {Data: []byte("body{color:red}")},
		"assets/logo.svg":      {Data: []byte("<svg/>")},
	}
	handler := NewRouter(testStore, frontendFS)

	contentTypeCases := map[string]string{
		"/assets/index-abc.js":  "text/javascript; charset=utf-8",
		"/assets/index-abc.css": "text/css; charset=utf-8",
		"/assets/logo.svg":      "image/svg+xml",
	}
	for path, wantType := range contentTypeCases {
		recorder := sendRequest(t, handler, "GET", path, nil)
		if recorder.Code != 200 {
			t.Fatalf("GET %s = %d", path, recorder.Code)
		}
		if gotType := recorder.Header().Get("Content-Type"); gotType != wantType {
			t.Fatalf("GET %s content-type = %q, want %q", path, gotType, wantType)
		}
	}

	// The SPA fallback still serves HTML for unknown client routes.
	recorder := sendRequest(t, handler, "GET", "/deep/link", nil)
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("fallback content-type = %q", contentType)
	}
	if recorder.Body.String() != "<!doctype html><title>app</title>" {
		t.Fatalf("fallback body = %q", recorder.Body.String())
	}
}

func TestBrowseRejectsBadMode(t *testing.T) {
	handler := newTestServer(t)
	// An invalid mode must be rejected before any native dialog is opened.
	recorder := sendRequest(t, handler, "POST", "/api/settings/browse", map[string]string{"mode": "bogus"})
	if recorder.Code != 400 {
		t.Fatalf("browse bad mode = %d, want 400", recorder.Code)
	}
}

func TestCreateWeekNeedsSixWorkers(t *testing.T) {
	handler := newTestServer(t)
	for index := 0; index < 5; index++ {
		sendRequest(t, handler, "POST", "/api/workers", map[string]string{"full_name": "X"})
	}
	recorder := sendRequest(t, handler, "POST", "/api/weeks", map[string]string{"week_start": "2026-01-05"})
	if recorder.Code != 400 {
		t.Fatalf("create week with 5 workers = %d, want 400", recorder.Code)
	}
}

func intToString(value int) string {
	if value == 0 {
		return "0"
	}
	isNegative := value < 0
	if isNegative {
		value = -value
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	if isNegative {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
