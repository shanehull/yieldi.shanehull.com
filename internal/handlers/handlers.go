package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/shanehull/yieldi.shanehull.com/internal/quant"
	"github.com/shanehull/yieldi.shanehull.com/internal/satellite"
	"github.com/shanehull/yieldi.shanehull.com/internal/weather"
)

// Server holds the application state and dependencies.
type Server struct {
	model            *quant.YieldModel
	satelliteService *satellite.Service
	weatherService   *weather.Service
	logger           *slog.Logger
}

// NewServer creates a new HTTP server with dependencies.
func NewServer(model *quant.YieldModel, satelliteService *satellite.Service, weatherService *weather.Service, logger *slog.Logger) *Server {
	return &Server{
		model:            model,
		satelliteService: satelliteService,
		weatherService:   weatherService,
		logger:           logger,
	}
}

// AssessRequest represents the incoming polygon assessment request.
type AssessRequest struct {
	Geometry         string  `json:"geometry"`
	AssessmentDate   string  `json:"assessment_date,omitempty"` // RFC3339 format, e.g., "2026-08-01T00:00:00Z"
	HarvestDate      string  `json:"harvest_date"`              // YYYY-MM-DD format
	SeasonDays       int     `json:"season_days"`               // Days from assessment to harvest
	FieldSizeHa      float64 `json:"field_size_ha"`             // Field size in hectares
	BaselineYield    float64 `json:"baseline_yield"`            // t/ha
	TargetHedgeRatio float64 `json:"target_hedge_ratio"`        // 0-1 scale
	Alpha            float64 `json:"alpha"`                     // Model coefficient
	Beta1            float64 `json:"beta1"`                     // Model coefficient
	Beta2            float64 `json:"beta2"`                     // Model coefficient
}

// AssessResponse represents the risk assessment result.
type AssessResponse struct {
	YieldEstimate      float64 `json:"yield_estimate"`
	YieldBaseline      float64 `json:"yield_baseline"`
	YieldDeltaPercent  float64 `json:"yield_delta_percent"`
	HedgeRatio         float64 `json:"hedge_ratio"`
	TargetHedgeRatio   float64 `json:"target_hedge_ratio"`
	TotalYieldEstimate float64 `json:"total_yield_estimate"` // Field size × yield estimate
	TotalHedgeVolume   float64 `json:"total_hedge_volume"`   // Field size × t/ha to protect
	NDVIAnomaly        float64 `json:"ndvi_anomaly"`
	RainfallDelta      float64 `json:"rainfall_delta"`
	CloudCover         float64 `json:"cloud_cover"`
	LowConfidence      bool    `json:"low_confidence"`
	Confidence         float64 `json:"confidence"`
	DaysToHarvest      int     `json:"days_to_harvest"`
	Error              string  `json:"error,omitempty"`
}

// HandleAssess processes an assessment request for a polygon.
func (s *Server) HandleAssess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AssessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.logger.Error("failed to decode request", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(AssessResponse{Error: "Invalid request"})
		return
	}

	// Parse assessment date if provided
	var assessmentDate time.Time
	if req.AssessmentDate != "" {
		var err error
		assessmentDate, err = time.Parse(time.RFC3339, req.AssessmentDate)
		if err != nil {
			s.logger.Warn("invalid assessment date format, using current time", "date", req.AssessmentDate)
			assessmentDate = time.Time{}
		}
	}

	// Parse geometry
	var geometry map[string]any
	if err := json.Unmarshal([]byte(req.Geometry), &geometry); err != nil {
		s.logger.Error("failed to parse geometry", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(AssessResponse{Error: "Invalid geometry"})
		return
	}

	// Default to current time if no assessment date provided
	if assessmentDate.IsZero() {
		assessmentDate = time.Now().UTC()
	}

	// Extract centroid from polygon for satellite and weather data
	var centroidLat, centroidLon float64
	polygonCoords, ok := geometry["coordinates"].([]any)
	if ok && len(polygonCoords) > 0 {
		// Convert to float coordinates
		var coords [][][]float64
		coordBytes, _ := json.Marshal(polygonCoords)
		if json.Unmarshal(coordBytes, &coords) == nil {
			if len(coords) > 0 {
				centroidLat, centroidLon, _ = weather.CalculateCentroid(coords)
				s.logger.Debug("polygon centroid calculated", "lat", centroidLat, "lon", centroidLon)
			}
		}
	}

	// Fetch satellite and weather data concurrently
	startFetch := time.Now()
	var wg sync.WaitGroup
	var historical, current *satellite.VegetationMetrics
	var rainfallMetrics *weather.RainfallMetrics
	var historicalErr, currentErr, rainfallErr error

	// Fetch historical NDVI
	wg.Go(func() {
		startHistorical := time.Now()
		h, err := s.satelliteService.FetchHistoricalNDVI(r.Context(), centroidLat, centroidLon, assessmentDate)
		s.logger.Debug("historical NDVI fetch", "duration_ms", time.Since(startHistorical).Milliseconds(), "error", err)
		if err != nil {
			historicalErr = err
			return
		}
		historical = h
	})

	// Fetch current NDVI
	wg.Go(func() {
		startCurrent := time.Now()
		c, err := s.satelliteService.FetchCurrentNDVI(r.Context(), centroidLat, centroidLon, assessmentDate)
		s.logger.Debug("current NDVI fetch", "duration_ms", time.Since(startCurrent).Milliseconds(), "error", err)
		if err != nil {
			currentErr = err
			return
		}
		current = c
	})

	// Fetch rainfall metrics
	wg.Go(func() {
		startRainfall := time.Now()
		r, err := s.weatherService.GetRainfallMetrics(r.Context(), centroidLat, centroidLon, assessmentDate)
		s.logger.Debug("rainfall metrics fetch", "duration_ms", time.Since(startRainfall).Milliseconds(), "error", err)
		if err != nil {
			rainfallErr = err
			return
		}
		rainfallMetrics = r
	})

	wg.Wait()
	s.logger.Debug("all data fetches completed", "duration_ms", time.Since(startFetch).Milliseconds())

	// Check for errors
	if historicalErr != nil {
		if errors.Is(historicalErr, context.Canceled) {
			s.logger.Debug("historical NDVI fetch canceled by client")
			return
		}
		s.logger.Error("failed to fetch historical ndvi", "error", historicalErr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(AssessResponse{Error: "Failed to fetch satellite data"})
		return
	}

	if currentErr != nil {
		if errors.Is(currentErr, context.Canceled) {
			s.logger.Debug("current NDVI fetch canceled by client")
			return
		}
		s.logger.Error("failed to fetch current ndvi", "error", currentErr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(AssessResponse{Error: "Failed to fetch current NDVI"})
		return
	}

	if rainfallErr != nil {
		if errors.Is(rainfallErr, context.Canceled) {
			s.logger.Debug("weather data fetch canceled by client")
			return
		}
		s.logger.Error("weather data fetch failed", "error", rainfallErr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(AssessResponse{Error: "Failed to fetch rainfall data"})
		return
	}

	// Calculate NDVI anomaly
	ndviAnomalyVal := 0.0
	if historical.NDVIMean > 0 {
		ndviAnomalyVal = current.NDVIMean / historical.NDVIMean
	}

	rainfallDeltaVal := rainfallMetrics.RainfallDelta

	// Parse harvest date from request
	harvestDate, err := time.Parse("2006-01-02", req.HarvestDate)
	if err != nil {
		s.logger.Warn("invalid harvest date format", "date", req.HarvestDate, "error", err)
		harvestDate = time.Now().UTC().AddDate(0, 0, req.SeasonDays)
	}

	// Create a local clone of the model for this request to ensure thread safety
	model := *s.model

	// Use coefficients from request
	model.Alpha = req.Alpha
	model.Beta1 = req.Beta1
	model.Beta2 = req.Beta2
	model.BaseHedgeTarget = req.TargetHedgeRatio
	model.HarvestDate = harvestDate
	model.TotalSeasonDays = req.SeasonDays

	// Set reference date on model if provided
	if !assessmentDate.IsZero() {
		model.ReferenceDate = assessmentDate
	}

	// Assess risk with baseline yield from request
	s.logger.Info("assessing production risk",
		"asOf", assessmentDate.Format("2006-01-02"),
		"lat", centroidLat,
		"lon", centroidLon,
		"ndvi_curr", current.NDVIMean,
		"ndvi_hist", historical.NDVIMean,
		"rain_curr", rainfallMetrics.CurrentTotal,
		"rain_hist", rainfallMetrics.HistoricalMean)

	risk := model.AssessRisk(
		req.BaselineYield,
		historical.NDVIMean,
		current.NDVIMean,
		rainfallMetrics.HistoricalMean,
		rainfallMetrics.CurrentTotal,
		current.CloudCover)

	// Calculate total yield and hedge volume based on field size
	totalYieldEstimate := risk.YieldEstimate * req.FieldSizeHa
	totalHedgeVolume := risk.YieldEstimate * risk.TargetHedgeRatio * req.FieldSizeHa

	// Return response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(AssessResponse{
		YieldEstimate:      risk.YieldEstimate,
		YieldBaseline:      risk.YieldBaseline,
		YieldDeltaPercent:  risk.YieldDeltaPercent,
		HedgeRatio:         risk.HedgeRatio,
		TargetHedgeRatio:   risk.TargetHedgeRatio,
		TotalYieldEstimate: totalYieldEstimate,
		TotalHedgeVolume:   totalHedgeVolume,
		NDVIAnomaly:        ndviAnomalyVal,
		RainfallDelta:      rainfallDeltaVal,
		CloudCover:         risk.CloudCover,
		LowConfidence:      risk.LowConfidence,
		Confidence:         risk.Confidence,
		DaysToHarvest:      risk.DaysToHarvest,
	})
}

// HandleHealth returns a simple health check.
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
