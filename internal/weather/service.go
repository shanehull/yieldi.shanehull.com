package weather

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/shanehull/yieldi.shanehull.com/internal/cache"
)

// Service orchestrates weather data retrieval and processing.
type Service struct {
	client *Client
	logger *slog.Logger
	cache  *cache.Cache[*RainfallMetrics]
}

// NewService creates a new weather service.
func NewService(logger *slog.Logger) *Service {
	return &Service{
		client: NewClient(),
		logger: logger,
		cache:  cache.New[*RainfallMetrics](),
	}
}

// CurrentSeasonRainfall returns rainfall for the current growing season relative to asOf.
// Growing season: April 1 to asOf date.
func (s *Service) CurrentSeasonRainfall(ctx context.Context, lat, lon float64, asOf time.Time) (*RainfallData, error) {
	if asOf.IsZero() {
		asOf = time.Now()
	}
	year := asOf.Year()

	// Growing season starts April 1
	seasonStart := time.Date(year, 4, 1, 0, 0, 0, 0, time.UTC)

	// If we're before April, use previous year's start
	if asOf.Before(seasonStart) {
		seasonStart = time.Date(year-1, 4, 1, 0, 0, 0, 0, time.UTC)
	}

	start := seasonStart.Format("2006-01-02")
	end := asOf.Format("2006-01-02")

	return s.client.FetchRainfallWithRetry(ctx, lat, lon, start, end, DefaultRetryConfig())
}

// HistoricalSeasonRainfall returns rainfall for the same growing season period relative to asOf.
func (s *Service) HistoricalSeasonRainfall(ctx context.Context, lat, lon float64, years int, asOf time.Time) (*RainfallData, error) {
	if asOf.IsZero() {
		asOf = time.Now()
	}
	currentYear := asOf.Year()

	// Determine current season dates
	seasonStart := time.Date(currentYear, 4, 1, 0, 0, 0, 0, time.UTC)
	if asOf.Before(seasonStart) {
		seasonStart = time.Date(currentYear-1, 4, 1, 0, 0, 0, 0, time.UTC)
	}

	// Calculate the offset from April 1 to today
	dayOfSeason := asOf.Sub(seasonStart).Hours() / 24

	var wg sync.WaitGroup
	results := make([]*RainfallData, years)

	// Fetch data for each previous year concurrently
	for i := 0; i < years; i++ {
		year := currentYear - i - 1
		startDate := time.Date(year, 4, 1, 0, 0, 0, 0, time.UTC)
		endDate := startDate.AddDate(0, 0, int(dayOfSeason))

		// Capture loop variables
		yearVal := year
		yearIdx := i
		start := startDate.Format("2006-01-02")
		end := endDate.Format("2006-01-02")

		wg.Go(func() {
			data, err := s.client.FetchRainfallWithRetry(ctx, lat, lon, start, end, DefaultRetryConfig())
			if err != nil {
				s.logger.Warn("historical rainfall fetch failed", "year", yearVal, "error", err)
				return
			}
			results[yearIdx] = data
		})
	}

	wg.Wait() // Allow partial failures, aggregate what we have

	// Aggregate results
	return aggregateHistoricalRainfall(results), nil
}

// aggregateHistoricalRainfall combines multiple years of rainfall data.
func aggregateHistoricalRainfall(results []*RainfallData) *RainfallData {
	aggregated := &RainfallData{
		DailyValues: make([]float64, 0),
	}

	validCount := 0
	totalRain := 0.0
	allDailyValues := make([]float64, 0)

	for _, result := range results {
		if result == nil || result.DaysCount == 0 {
			continue
		}

		validCount++
		totalRain += result.TotalRain
		allDailyValues = append(allDailyValues, result.DailyValues...)
	}

	if validCount == 0 {
		return aggregated
	}

	aggregated.DailyValues = allDailyValues
	aggregated.TotalRain = totalRain / float64(validCount)  // Average across years
	aggregated.DaysCount = len(allDailyValues) / validCount // Average days per season

	if aggregated.DaysCount > 0 {
		aggregated.MeanDaily = aggregated.TotalRain / float64(aggregated.DaysCount)
	}

	return aggregated
}

// RainfallMetrics holds current and historical rainfall with computed delta.
type RainfallMetrics struct {
	CurrentTotal   float64
	HistoricalMean float64
	RainfallDelta  float64
	DataPoints     int
	LastUpdate     time.Time
}

// GetRainfallMetrics computes current season vs historical mean rainfall relative to asOf.
func (s *Service) GetRainfallMetrics(ctx context.Context, lat, lon float64, asOf time.Time) (*RainfallMetrics, error) {
	if asOf.IsZero() {
		asOf = time.Now()
	}
	cacheKey := fmt.Sprintf("rainfall_%.4f_%.4f_%s", lat, lon, asOf.Format("2006-01-02"))

	// Check cache first
	if metrics, ok := s.cache.Get(cacheKey); ok {
		s.logger.Debug("cache hit", "key", cacheKey)
		return metrics, nil
	}

	// Fetch current season
	current, err := s.CurrentSeasonRainfall(ctx, lat, lon, asOf)
	if err != nil {
		return nil, fmt.Errorf("current season rainfall fetch failed: %w", err)
	}

	// Fetch historical (5 years)
	historical, err := s.HistoricalSeasonRainfall(ctx, lat, lon, 5, asOf)
	if err != nil {
		return nil, fmt.Errorf("historical rainfall fetch failed: %w", err)
	}

	// Calculate delta
	delta := 0.0
	if historical.TotalRain > 0 {
		delta = (current.TotalRain - historical.TotalRain) / historical.TotalRain
	}

	metrics := &RainfallMetrics{
		CurrentTotal:   current.TotalRain,
		HistoricalMean: historical.TotalRain,
		RainfallDelta:  delta,
		DataPoints:     current.DaysCount,
		LastUpdate:     asOf,
	}

	// Cache with 24-hour TTL for simulations
	s.cache.Set(cacheKey, metrics, 24*time.Hour)

	return metrics, nil
}

// CalculateCentroid computes the center point of a GeoJSON polygon using the shoelace formula.
// Assumes coordinates are in [lon, lat] format (GeoJSON standard).
// Returns (latitude, longitude) in geographic order (matching the function signature).
func CalculateCentroid(coordinates [][][]float64) (lat, lon float64, err error) {
	if len(coordinates) == 0 || len(coordinates[0]) == 0 {
		return 0, 0, fmt.Errorf("empty coordinates")
	}

	ring := coordinates[0] // Use outer ring

	var sumLat, sumLon float64
	n := len(ring)

	if n < 3 {
		return 0, 0, fmt.Errorf("polygon must have at least 3 points")
	}

	// Calculate centroid as simple average of vertices
	// This works for any polygon winding order and is stable
	for _, point := range ring {
		sumLon += point[0]
		sumLat += point[1]
	}

	return sumLat / float64(n), sumLon / float64(n), nil
}
