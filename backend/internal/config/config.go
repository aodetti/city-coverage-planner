// Package config manages the small pointer file that records which data file
// the application should use. The pointer lives at a fixed location in the OS
// application-data directory, while the actual database (data.json) may sit
// anywhere the user chooses via the Settings page.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings is the persisted application configuration.
type Settings struct {
	DataPath string `json:"data_path"`
}

// AppDir returns (and creates) the per-OS application-data directory.
func AppDir() (string, error) {
	baseConfigDir, configDirErr := os.UserConfigDir()
	if configDirErr != nil {
		return "", configDirErr
	}
	applicationDir := filepath.Join(baseConfigDir, "GridPlanner")
	if makeDirErr := os.MkdirAll(applicationDir, 0o755); makeDirErr != nil {
		return "", makeDirErr
	}
	return applicationDir, nil
}

// pointerFilePath returns the path to the pointer file.
func pointerFilePath() (string, error) {
	applicationDir, appDirErr := AppDir()
	if appDirErr != nil {
		return "", appDirErr
	}
	return filepath.Join(applicationDir, "config.json"), nil
}

// DefaultDataPath returns the default database location (AppDir/data.json).
func DefaultDataPath() (string, error) {
	applicationDir, appDirErr := AppDir()
	if appDirErr != nil {
		return "", appDirErr
	}
	return filepath.Join(applicationDir, "data.json"), nil
}

// Load reads the pointer file, falling back to the default data path when it is
// missing or blank.
func Load() (*Settings, error) {
	defaultDataPath, defaultPathErr := DefaultDataPath()
	if defaultPathErr != nil {
		return nil, defaultPathErr
	}
	pointerPath, pointerPathErr := pointerFilePath()
	if pointerPathErr != nil {
		return nil, pointerPathErr
	}
	rawJSON, readErr := os.ReadFile(pointerPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return &Settings{DataPath: defaultDataPath}, nil
		}
		return nil, readErr
	}
	var settings Settings
	if len(rawJSON) > 0 {
		if unmarshalErr := json.Unmarshal(rawJSON, &settings); unmarshalErr != nil {
			return nil, unmarshalErr
		}
	}
	if settings.DataPath == "" {
		settings.DataPath = defaultDataPath
	}
	return &settings, nil
}

// Save writes the pointer file atomically.
func Save(settings *Settings) error {
	pointerPath, pointerPathErr := pointerFilePath()
	if pointerPathErr != nil {
		return pointerPathErr
	}
	rawJSON, marshalErr := json.MarshalIndent(settings, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	tempFilePath := pointerPath + ".tmp"
	if writeErr := os.WriteFile(tempFilePath, rawJSON, 0o644); writeErr != nil {
		return writeErr
	}
	return os.Rename(tempFilePath, pointerPath)
}
