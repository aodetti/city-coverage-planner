// Package api wires the HTTP handlers that mirror the original FastAPI backend,
// plus static serving of the embedded single-page frontend. Handlers here are
// deliberately thin: they decode the request, delegate to package service for
// business logic, then encode the result and translate the service's sentinel
// errors into HTTP status codes.
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gridplanner/internal/filedialog"
	"gridplanner/internal/mapconfig"
	"gridplanner/internal/planner"
	"gridplanner/internal/service"
	"gridplanner/internal/store"
)

// Server holds the dependencies shared by every handler.
type Server struct {
	app *service.Service
}

// NewRouter builds the full HTTP handler: /api/* endpoints plus static serving
// of the embedded frontend (frontendFS) with SPA fallback to index.html.
func NewRouter(initialStore *store.Store, frontendFS fs.FS) http.Handler {
	server := &Server{app: service.New(initialStore)}
	router := http.NewServeMux()

	router.HandleFunc("GET /api/map", server.getMap)

	router.HandleFunc("GET /api/workers", server.listWorkers)
	router.HandleFunc("POST /api/workers", server.createWorker)
	router.HandleFunc("DELETE /api/workers/{id}", server.deleteWorker)
	router.HandleFunc("GET /api/workers/{id}/schedule", server.workerSchedule)

	router.HandleFunc("GET /api/weeks", server.listWeeks)
	router.HandleFunc("POST /api/weeks", server.createWeek)
	router.HandleFunc("GET /api/weeks/{id}", server.getWeek)
	router.HandleFunc("POST /api/weeks/{id}/replan", server.replanWeek)
	router.HandleFunc("DELETE /api/weeks/{id}", server.deleteWeek)

	router.HandleFunc("GET /api/settings", server.getSettings)
	router.HandleFunc("POST /api/settings/db", server.setDB)
	router.HandleFunc("POST /api/settings/browse", server.browseDB)

	router.Handle("/", spaHandler(frontendFS))
	return router
}

// ── helpers ───────────────────────────────────────────────────────────────

func writeJSON(responseWriter http.ResponseWriter, statusCode int, payload interface{}) {
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(statusCode)
	_ = json.NewEncoder(responseWriter).Encode(payload)
}

// writeError mirrors FastAPI's {"detail": "..."} error body.
func writeError(responseWriter http.ResponseWriter, statusCode int, detail string) {
	writeJSON(responseWriter, statusCode, map[string]string{"detail": detail})
}

func retrieveIdFromURLPath(request *http.Request, paramName string) (int, bool) {
	parsedID, parseErr := strconv.Atoi(request.PathValue(paramName))
	if parseErr != nil {
		return 0, false
	}
	return parsedID, true
}

func workerToJSON(worker *store.Worker) map[string]interface{} {
	return map[string]interface{}{"id": worker.ID, "full_name": worker.FullName}
}

func weekSummary(weekPlan *store.WeekPlan) map[string]interface{} {
	var createdAt interface{}
	if weekPlan.CreatedAt.IsZero() {
		createdAt = nil
	} else {
		createdAt = weekPlan.CreatedAt.Format(time.RFC3339)
	}
	return map[string]interface{}{
		"id":         weekPlan.ID,
		"week_start": weekPlan.WeekStart,
		"created_at": createdAt,
	}
}

func summaryWithData(weekPlan *store.WeekPlan) map[string]interface{} {
	summary := weekSummary(weekPlan)
	summary["data"] = json.RawMessage(weekPlan.Data)
	return summary
}

// ── Map ───────────────────────────────────────────────────────────────────

func (server *Server) getMap(responseWriter http.ResponseWriter, request *http.Request) {
	writeJSON(responseWriter, http.StatusOK, mapconfig.MapPayload())
}

// ── Workers ────────────────────────────────────────────────────────────

func (server *Server) listWorkers(responseWriter http.ResponseWriter, request *http.Request) {
	workers := server.app.ListWorkers()
	workersJSON := make([]map[string]interface{}, 0, len(workers))
	for _, worker := range workers {
		workersJSON = append(workersJSON, workerToJSON(worker))
	}
	writeJSON(responseWriter, http.StatusOK, workersJSON)
}

func (server *Server) createWorker(responseWriter http.ResponseWriter, request *http.Request) {
	var requestBody struct {
		FullName string `json:"full_name"`
	}
	if decodeErr := json.NewDecoder(request.Body).Decode(&requestBody); decodeErr != nil {
		writeError(responseWriter, http.StatusBadRequest, "Invalid request body.")
		return
	}
	createdWorker, createErr := server.app.CreateWorker(requestBody.FullName)
	if errors.Is(createErr, service.ErrEmptyName) {
		writeError(responseWriter, http.StatusBadRequest, "Full name is required.")
		return
	}
	if createErr != nil {
		writeError(responseWriter, http.StatusInternalServerError, createErr.Error())
		return
	}
	writeJSON(responseWriter, http.StatusCreated, workerToJSON(createdWorker))
}

func (server *Server) deleteWorker(responseWriter http.ResponseWriter, request *http.Request) {
	workerID, hasID := retrieveIdFromURLPath(request, "id")
	if !hasID {
		writeError(responseWriter, http.StatusNotFound, "Worker not found.")
		return
	}
	if deleteErr := server.app.DeleteWorker(workerID); deleteErr != nil {
		writeError(responseWriter, http.StatusNotFound, "Worker not found.")
		return
	}
	responseWriter.WriteHeader(http.StatusNoContent)
}

// storedPlanData is the minimal shape we read back out of a stored plan for the
// worker schedule view.
type storedPlanData struct {
	Days []struct {
		Date    string `json:"date"`
		Weekday string `json:"weekday"`
		Workers []struct {
			WorkerID   int    `json:"worker_id"`
			Shift      string `json:"shift"`
			ShiftLabel string `json:"shift_label"`
			Zone       int    `json:"zone"`
			Color      string `json:"color"`
		} `json:"workers"`
	} `json:"days"`
}

func (server *Server) workerSchedule(responseWriter http.ResponseWriter, request *http.Request) {
	workerID, hasID := retrieveIdFromURLPath(request, "id")
	if !hasID {
		writeError(responseWriter, http.StatusNotFound, "Worker not found.")
		return
	}
	worker, getErr := server.app.GetWorker(workerID)
	if getErr != nil {
		writeError(responseWriter, http.StatusNotFound, "Worker not found.")
		return
	}

	weeksJSON := make([]map[string]interface{}, 0)
	for _, weekPlan := range server.app.ListWeeks() {
		var parsedPlan storedPlanData
		if unmarshalErr := json.Unmarshal(weekPlan.Data, &parsedPlan); unmarshalErr != nil {
			writeError(responseWriter, http.StatusInternalServerError, "Corrupt plan data.")
			return
		}
		daysJSON := make([]map[string]interface{}, 0, len(parsedPlan.Days))
		daysWorked := 0
		for _, day := range parsedPlan.Days {
			var assignment interface{}
			for _, worker := range day.Workers {
				if worker.WorkerID == workerID {
					daysWorked++
					assignment = map[string]interface{}{
						"shift":       worker.Shift,
						"shift_label": worker.ShiftLabel,
						"zone":        worker.Zone,
						"color":       worker.Color,
					}
					break
				}
			}
			daysJSON = append(daysJSON, map[string]interface{}{
				"date":       day.Date,
				"weekday":    day.Weekday,
				"assignment": assignment,
			})
		}
		weeksJSON = append(weeksJSON, map[string]interface{}{
			"plan_id":     weekPlan.ID,
			"week_start":  weekPlan.WeekStart,
			"days_worked": daysWorked,
			"days":        daysJSON,
		})
	}

	writeJSON(responseWriter, http.StatusOK, map[string]interface{}{
		"worker":     workerToJSON(worker),
		"shifts_def": planner.Shifts,
		"weeks":      weeksJSON,
	})
}

// ── Week plans ────────────────────────────────────────────────────────────

// writeWeekError maps a service error from week creation/replan to a response.
func writeWeekError(responseWriter http.ResponseWriter, weekErr error) {
	switch {
	case errors.Is(weekErr, service.ErrInvalidWeekStart):
		writeError(responseWriter, http.StatusBadRequest, "week_start must be an ISO date (YYYY-MM-DD).")
	case errors.Is(weekErr, service.ErrWeekExists):
		writeError(responseWriter, http.StatusConflict, "A plan for that week already exists.")
	case errors.Is(weekErr, service.ErrTooFewWorkers):
		writeError(responseWriter, http.StatusBadRequest, "At least 6 workers are required to plan a week.")
	case errors.Is(weekErr, store.ErrNotFound):
		writeError(responseWriter, http.StatusNotFound, "Plan not found.")
	default:
		writeError(responseWriter, http.StatusInternalServerError, weekErr.Error())
	}
}

func (server *Server) listWeeks(responseWriter http.ResponseWriter, request *http.Request) {
	weekPlans := server.app.ListWeeks()
	weekSummariesJSON := make([]map[string]interface{}, 0, len(weekPlans))
	for _, weekPlan := range weekPlans {
		weekSummariesJSON = append(weekSummariesJSON, weekSummary(weekPlan))
	}
	writeJSON(responseWriter, http.StatusOK, weekSummariesJSON)
}

func (server *Server) createWeek(responseWriter http.ResponseWriter, request *http.Request) {
	var requestBody struct {
		WeekStart string `json:"week_start"`
	}
	if decodeErr := json.NewDecoder(request.Body).Decode(&requestBody); decodeErr != nil {
		writeError(responseWriter, http.StatusBadRequest, "Invalid request body.")
		return
	}
	createdWeek, createErr := server.app.CreateWeek(requestBody.WeekStart)
	if createErr != nil {
		writeWeekError(responseWriter, createErr)
		return
	}
	writeJSON(responseWriter, http.StatusCreated, summaryWithData(createdWeek))
}

func (server *Server) getWeek(responseWriter http.ResponseWriter, request *http.Request) {
	weekID, hasID := retrieveIdFromURLPath(request, "id")
	if !hasID {
		writeError(responseWriter, http.StatusNotFound, "Plan not found.")
		return
	}
	weekPlan, getErr := server.app.GetWeek(weekID)
	if getErr != nil {
		writeError(responseWriter, http.StatusNotFound, "Plan not found.")
		return
	}
	writeJSON(responseWriter, http.StatusOK, summaryWithData(weekPlan))
}

func (server *Server) replanWeek(responseWriter http.ResponseWriter, request *http.Request) {
	weekID, hasID := retrieveIdFromURLPath(request, "id")
	if !hasID {
		writeError(responseWriter, http.StatusNotFound, "Plan not found.")
		return
	}
	updatedWeek, replanErr := server.app.ReplanWeek(weekID)
	if replanErr != nil {
		writeWeekError(responseWriter, replanErr)
		return
	}
	writeJSON(responseWriter, http.StatusOK, summaryWithData(updatedWeek))
}

func (server *Server) deleteWeek(responseWriter http.ResponseWriter, request *http.Request) {
	weekID, hasID := retrieveIdFromURLPath(request, "id")
	if !hasID {
		writeError(responseWriter, http.StatusNotFound, "Plan not found.")
		return
	}
	if deleteErr := server.app.DeleteWeek(weekID); deleteErr != nil {
		writeError(responseWriter, http.StatusNotFound, "Plan not found.")
		return
	}
	responseWriter.WriteHeader(http.StatusNoContent)
}

// ── Settings ──────────────────────────────────────────────────────────────

func (server *Server) getSettings(responseWriter http.ResponseWriter, request *http.Request) {
	defaultDataPath, defaultPathErr := server.app.DefaultDataPath()
	if defaultPathErr != nil {
		writeError(responseWriter, http.StatusInternalServerError, defaultPathErr.Error())
		return
	}
	writeJSON(responseWriter, http.StatusOK, map[string]interface{}{
		"data_path":         server.app.DataPath(),
		"default_data_path": defaultDataPath,
	})
}

func (server *Server) setDB(responseWriter http.ResponseWriter, request *http.Request) {
	var requestBody struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
	}
	if decodeErr := json.NewDecoder(request.Body).Decode(&requestBody); decodeErr != nil {
		writeError(responseWriter, http.StatusBadRequest, "Invalid request body.")
		return
	}
	activeDataPath, switchErr := server.app.SwitchDB(requestBody.Path, requestBody.Mode)
	if switchErr != nil {
		writeError(responseWriter, http.StatusBadRequest, switchErr.Error())
		return
	}
	writeJSON(responseWriter, http.StatusOK, map[string]interface{}{"data_path": activeDataPath})
}

// browseDB opens the native OS file dialog so the user can pick a database file
// visually. mode "new" shows a save dialog, "open" shows an open dialog. The
// response is {"path": "..."}; path is empty when the user cancels.
func (server *Server) browseDB(responseWriter http.ResponseWriter, request *http.Request) {
	var requestBody struct {
		Mode string `json:"mode"`
	}
	if decodeErr := json.NewDecoder(request.Body).Decode(&requestBody); decodeErr != nil {
		writeError(responseWriter, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if requestBody.Mode != filedialog.ModeNew && requestBody.Mode != filedialog.ModeOpen {
		writeError(responseWriter, http.StatusBadRequest, "mode must be “new” or “open”.")
		return
	}
	pickedPath, pickErr := filedialog.Pick(requestBody.Mode)
	if pickErr != nil {
		if errors.Is(pickErr, filedialog.ErrUnavailable) {
			writeError(responseWriter, http.StatusNotImplemented,
				"No hay un explorador de archivos disponible en este sistema. Escribí la ruta manualmente.")
			return
		}
		writeError(responseWriter, http.StatusInternalServerError, pickErr.Error())
		return
	}
	writeJSON(responseWriter, http.StatusOK, map[string]string{"path": pickedPath})
}

// ── Static SPA serving ────────────────────────────────────────────────────

// staticContentTypes maps file extensions to the Content-Type we serve. We set
// these explicitly rather than relying on the OS, because on Windows Go's
// mime.TypeByExtension reads from the registry, where ".js" is frequently mapped
// to "text/plain". That makes browsers refuse to run the bundled ES modules and
// surfaces as a "SyntaxError: Unexpected token {" — see the fix for this bug.
var staticContentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".map":   "application/json; charset=utf-8",
	".svg":   "image/svg+xml",
	".ico":   "image/x-icon",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".txt":   "text/plain; charset=utf-8",
}

func init() {
	// Belt-and-suspenders: also override the process-wide mime table so any
	// other code path picks up the correct types too.
	for fileExtension, contentType := range staticContentTypes {
		_ = mime.AddExtensionType(fileExtension, contentType)
	}
}

// setContentType applies our explicit type for a path's extension, if known.
// It must be called before the file server writes the response, because
// http.ServeContent only guesses a type when Content-Type is not already set.
func setContentType(responseWriter http.ResponseWriter, fileName string) {
	if contentType := staticContentTypes[strings.ToLower(filepath.Ext(fileName))]; contentType != "" {
		responseWriter.Header().Set("Content-Type", contentType)
	}
}

// spaHandler serves files from frontendFS, falling back to index.html for any
// path that does not map to an existing embedded file (client-side routing).
func spaHandler(frontendFS fs.FS) http.Handler {
	fileServer := http.FileServerFS(frontendFS)
	serveIndexShell := func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Type", "text/html; charset=utf-8")
		indexRequest := request.Clone(request.Context())
		indexRequest.URL.Path = "/"
		fileServer.ServeHTTP(responseWriter, indexRequest)
	}
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		cleanedPath := strings.TrimPrefix(path.Clean("/"+request.URL.Path), "/")
		if cleanedPath == "" {
			serveIndexShell(responseWriter, request)
			return
		}
		if _, statErr := fs.Stat(frontendFS, cleanedPath); statErr != nil {
			// Unknown path -> serve the SPA shell.
			serveIndexShell(responseWriter, request)
			return
		}
		setContentType(responseWriter, cleanedPath)
		fileServer.ServeHTTP(responseWriter, request)
	})
}
