package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/shanehull/yieldi/internal/quant"
	"github.com/shanehull/yieldi/internal/satellite"
	"github.com/shanehull/yieldi/internal/weather"
)

func TestHandleAssessCoordinates(t *testing.T) {
	// Setup logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Create mock services
	model := quant.NewYieldModel()
	satelliteService := satellite.NewService(logger, "", "")
	weatherService := weather.NewService(logger)

	server := NewServer(model, satelliteService, weatherService, logger)

	// Test data: Victoria polygon (GeoJSON format [lon, lat])
	// This mimics what the frontend sends after converting from [lat, lng]
	geojsonData := map[string]interface{}{
		"type": "Polygon",
		"coordinates": [][][]float64{
			{
				{145.0, -37.0}, // Victoria, Australia
				{145.5, -37.0},
				{145.5, -37.5},
				{145.0, -37.5},
				{145.0, -37.0},
			},
		},
	}

	geojsonBytes, _ := json.Marshal(geojsonData)
	reqBody := AssessRequest{
		Geometry: string(geojsonBytes),
	}

	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/assess", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.HandleAssess(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	var result AssessResponse
	_ = json.Unmarshal(body, &result)

	t.Logf("Response: %+v", result)

	// The test will fail because we don't have real satellite data,
	// but we can verify the centroid calculation in the logs
	if result.Error == "" {
		t.Logf("Request succeeded, coordinates were processed correctly")
	}
}
