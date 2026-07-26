// Package mapconfig is the single source of truth for the street grid that the
// planner reasons about and the frontend renders. The grid is data-driven: a
// JSON map file describes the streets, which of them are avenues, the drawing
// geometry, and the central district. A generic example map is embedded so the
// binary works out of the box; a deployment can supply its own map file to
// cover a different city (see Init).
package mapconfig

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Street describes one street of the grid and whether it is an avenue.
type Street struct {
	Name     string `json:"name"`
	IsAvenue bool   `json:"av"`
}

// Geometry holds the drawing constants (kept identical on the frontend). The
// JSON keys are intentionally uppercase to match the frontend's destructuring.
type Geometry struct {
	BlockWidth         int `json:"BW"`    // block width
	BlockHeight        int `json:"BH"`    // block height
	StreetWidth        int `json:"SW"`    // street width
	AvenueWidth        int `json:"AVW"`   // avenue width
	HalfBlockRing      int `json:"HALF"`  // half-block ring thickness
	MarginTop          int `json:"MT"`    // margin top
	MarginLeft         int `json:"ML"`    // margin left
	MarginRight        int `json:"MR"`    // margin right
	MarginBottom       int `json:"MB"`    // margin bottom
	GridBearingDegrees int `json:"ANGLE"` // grid bearing vs. true north (deg, clockwise)
}

// Central marks the always-prioritised central district as lists of block
// column and row indices. The planner keeps this district covered by two zones
// at every hour, so it is split across the two central zones.
type Central struct {
	Columns []int `json:"columns"`
	Rows    []int `json:"rows"`
}

// Map is a complete street-grid definition, loaded from a JSON file (or the
// embedded example). The same object feeds the planner and the frontend.
type Map struct {
	Label             string   `json:"label"`
	HorizontalStreets []Street `json:"horizontal_streets"`
	VerticalStreets   []Street `json:"vertical_streets"`
	Geometry          Geometry `json:"geometry"`
	Central           Central  `json:"central"`
}

// exampleMapJSON is the built-in map, used when no external map file is found.
// It describes a generic, fictional downtown grid.
//
//go:embed map.default.json
var exampleMapJSON []byte

// The active grid. It is populated from the embedded example at init and can be
// replaced at startup via Init/LoadFile. The rest of the program reads these
// package variables (they are read-only once the process is serving requests).
var (
	// Label is a human-readable name for the mapped area (shown in the UI).
	Label string
	// HorizontalStreets are the horizontal (west-east) streets, north to south.
	HorizontalStreets []Street
	// VerticalStreets are the vertical (north-south) streets, west to east.
	VerticalStreets []Street
	// SharedGeometry is the geometry used by both the planner and the frontend.
	SharedGeometry Geometry
	// CentralColumns/CentralRows are the block indices of the central district.
	CentralColumns []int
	CentralRows    []int

	// NumColumns is the number of city-block columns in the framed study area.
	NumColumns int
	// NumRows is the number of city-block rows.
	NumRows int
	// NumBlocks is the total number of blocks.
	NumBlocks int
)

func init() {
	parsedMap, parseErr := parseMap(exampleMapJSON)
	if parseErr != nil {
		panic("mapconfig: invalid embedded example map: " + parseErr.Error())
	}
	apply(parsedMap)
}

// parseMap decodes a map definition and checks it is internally consistent.
func parseMap(rawJSON []byte) (*Map, error) {
	var parsedMap Map
	if unmarshalErr := json.Unmarshal(rawJSON, &parsedMap); unmarshalErr != nil {
		return nil, unmarshalErr
	}
	if len(parsedMap.VerticalStreets) < 2 || len(parsedMap.HorizontalStreets) < 2 {
		return nil, fmt.Errorf("a map needs at least 2 vertical and 2 horizontal streets")
	}
	columns := len(parsedMap.VerticalStreets) - 1
	rows := len(parsedMap.HorizontalStreets) - 1
	if len(parsedMap.Central.Columns) == 0 || len(parsedMap.Central.Rows) == 0 {
		return nil, fmt.Errorf("central.columns and central.rows must be non-empty")
	}
	for _, column := range parsedMap.Central.Columns {
		if column < 0 || column >= columns {
			return nil, fmt.Errorf("central column %d is outside the grid (0..%d)", column, columns-1)
		}
	}
	for _, row := range parsedMap.Central.Rows {
		if row < 0 || row >= rows {
			return nil, fmt.Errorf("central row %d is outside the grid (0..%d)", row, rows-1)
		}
	}
	return &parsedMap, nil
}

// apply installs a parsed map as the active grid and recomputes the dimensions.
func apply(parsedMap *Map) {
	Label = parsedMap.Label
	HorizontalStreets = parsedMap.HorizontalStreets
	VerticalStreets = parsedMap.VerticalStreets
	SharedGeometry = parsedMap.Geometry
	CentralColumns = parsedMap.Central.Columns
	CentralRows = parsedMap.Central.Rows
	NumColumns = len(parsedMap.VerticalStreets) - 1
	NumRows = len(parsedMap.HorizontalStreets) - 1
	NumBlocks = NumColumns * NumRows
}

// LoadFile loads and installs a map from a JSON file path.
func LoadFile(path string) error {
	rawJSON, readErr := os.ReadFile(path)
	if readErr != nil {
		return readErr
	}
	parsedMap, parseErr := parseMap(rawJSON)
	if parseErr != nil {
		return fmt.Errorf("%s: %w", path, parseErr)
	}
	apply(parsedMap)
	return nil
}

// Init resolves which map to use, installs it, and returns a human-readable
// description of the source. Resolution order:
//
//  1. the MAP_CONFIG environment variable, if set;
//  2. a map.json in the current working directory (next to the binary);
//  3. a map.json in the application-data directory (appDir);
//  4. the built-in example map (embedded).
func Init(appDir string) (string, error) {
	if envPath := strings.TrimSpace(os.Getenv("MAP_CONFIG")); envPath != "" {
		if loadErr := LoadFile(envPath); loadErr != nil {
			return "", loadErr
		}
		return envPath + " (MAP_CONFIG)", nil
	}
	candidatePaths := []string{"map.json"}
	if appDir != "" {
		candidatePaths = append(candidatePaths, filepath.Join(appDir, "map.json"))
	}
	for _, candidatePath := range candidatePaths {
		if _, statErr := os.Stat(candidatePath); statErr == nil {
			if loadErr := LoadFile(candidatePath); loadErr != nil {
				return "", loadErr
			}
			return candidatePath, nil
		}
	}
	return "built-in example map", nil
}

// BlockCenters returns the (col, row) centre coords of every block, with
// index = row*cols + col.
func BlockCenters() [][2]float64 {
	blockCenters := make([][2]float64, 0, NumBlocks)
	for rowIndex := 0; rowIndex < NumRows; rowIndex++ {
		for columnIndex := 0; columnIndex < NumColumns; columnIndex++ {
			blockCenters = append(blockCenters, [2]float64{float64(columnIndex) + 0.5, float64(rowIndex) + 0.5})
		}
	}
	return blockCenters
}

// FrontendPayload is the JSON object sent to the frontend at GET /api/map.
type FrontendPayload struct {
	Label             string   `json:"label"`
	HorizontalStreets []Street `json:"h_streets"`
	VerticalStreets   []Street `json:"v_streets"`
	Geometry          Geometry `json:"geometry"`
	Cols              int      `json:"cols"`
	Rows              int      `json:"rows"`
	NumBlocks         int      `json:"num_blocks"`
}

// MapPayload builds the payload for the frontend.
func MapPayload() FrontendPayload {
	return FrontendPayload{
		Label:             Label,
		HorizontalStreets: HorizontalStreets,
		VerticalStreets:   VerticalStreets,
		Geometry:          SharedGeometry,
		Cols:              NumColumns,
		Rows:              NumRows,
		NumBlocks:         NumBlocks,
	}
}
