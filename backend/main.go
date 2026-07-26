// Command gridplanner runs Grid Planner as a self-contained desktop
// application: it starts a local web server on a free port, opens the user's
// default browser at that address, and keeps running until the console window
// is closed. All data lives in a single JSON file under the OS app-data folder,
// and the frontend is embedded in the binary, so there is nothing to install.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"gridplanner/internal/api"
	"gridplanner/internal/config"
	"gridplanner/internal/mapconfig"
	"gridplanner/internal/planner"
	"gridplanner/internal/store"
	"gridplanner/internal/web"
)

func main() {
	log.SetFlags(0)

	settings, loadErr := config.Load()
	if loadErr != nil {
		log.Fatalf("could not read settings: %v", loadErr)
	}

	// Resolve and load the street-grid map (external file or the embedded
	// example), then let the planner adopt its dimensions.
	applicationDir, _ := config.AppDir()
	mapSource, mapErr := mapconfig.Init(applicationDir)
	if mapErr != nil {
		log.Fatalf("could not load map: %v", mapErr)
	}
	planner.Configure()

	dataPath := settings.DataPath
	dataStore, openErr := store.Open(dataPath)
	if openErr != nil {
		log.Fatalf("could not open data file: %v", openErr)
	}

	frontendFS, embedErr := web.FrontendFileSystem()
	if embedErr != nil {
		log.Fatalf("could not load embedded frontend: %v", embedErr)
	}

	handler := api.NewRouter(dataStore, frontendFS)

	// Bind to a free port on localhost.
	listenAddress := "127.0.0.1:0"
	if portEnv := os.Getenv("PORT"); portEnv != "" {
		listenAddress = "127.0.0.1:" + portEnv
	}
	listener, listenErr := net.Listen("tcp", listenAddress)
	if listenErr != nil {
		log.Fatalf("could not start server: %v", listenErr)
	}
	browserURL := fmt.Sprintf("http://%s/", listener.Addr().String())

	httpServer := &http.Server{Handler: handler}
	go func() {
		if serveErr := httpServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Fatalf("server error: %v", serveErr)
		}
	}()

	fmt.Println("========================================")
	fmt.Println("  Grid Planner is running.")
	fmt.Printf("  Open in your browser:  %s\n", browserURL)
	fmt.Printf("  Map:                   %s (%s)\n", mapconfig.Label, mapSource)
	fmt.Printf("  Your data is saved in: %s\n", dataPath)
	fmt.Println()
	fmt.Println("  Close this window to stop the program.")
	fmt.Println("========================================")

	openBrowser(browserURL)

	// Wait for Ctrl+C / window close, then shut down gracefully.
	shutdownSignalCtx, stopListening := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopListening()
	<-shutdownSignalCtx.Done()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelShutdown()
	_ = httpServer.Shutdown(shutdownCtx)
	fmt.Println("Grid Planner stopped.")
}

// openBrowser opens targetURL in the user's default browser (best effort).
func openBrowser(targetURL string) {
	var command string
	var args []string
	switch runtime.GOOS {
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", targetURL}
	case "darwin":
		command, args = "open", []string{targetURL}
	default: // linux, bsd, ...
		command, args = "xdg-open", []string{targetURL}
	}
	if startErr := exec.Command(command, args...).Start(); startErr != nil {
		log.Printf("could not open browser automatically: %v", startErr)
	}
}
