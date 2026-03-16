package satellite

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/shanehull/yieldi/internal/cache"
)

// Service orchestrates satellite data retrieval using STAC with in-memory caching.
type Service struct {
	stacClient *STACClient
	logger     *slog.Logger
	cache      *cache.Cache[[]LandsatScene]
}

// NewService creates a new satellite service using STAC (no credentials needed).
func NewService(logger *slog.Logger, username, password string) *Service {
	// Note: username and password are ignored for STAC
	// STAC is free and public
	return &Service{
		stacClient: NewSTACClient(),
		logger:     logger,
		cache:      cache.New[[]LandsatScene](),
	}
}

// NewServiceWithToken creates a new satellite service using STAC (token ignored).
func NewServiceWithToken(logger *slog.Logger, token string) *Service {
	return &Service{
		stacClient: NewSTACClient(),
		logger:     logger,
		cache:      cache.New[[]LandsatScene](),
	}
}

// VegetationMetrics holds NDVI and cloud cover observations.
type VegetationMetrics struct {
	NDVIMean       float64
	NDVIStdDev     float64
	CloudCover     float64
	DaysCount      int
	CollectionDate string
}



// FetchCurrentNDVI retrieves current season NDVI (last 60 days) with caching.
func (s *Service) FetchCurrentNDVI(ctx context.Context, lat, lon float64) (*VegetationMetrics, error) {
	now := time.Now()
	cacheKey := fmt.Sprintf("current_%.4f_%.4f", lat, lon)

	// Check cache first
	if cachedScenes, ok := s.cache.Get(cacheKey); ok {
		s.logger.Debug("cache hit", "key", cacheKey)
		obs := AggregateObservations(cachedScenes)
		return &VegetationMetrics{
			NDVIMean:       obs.NDVI,
			CloudCover:     obs.CloudCover,
			DaysCount:      len(cachedScenes),
			CollectionDate: now.Format("2006-01-02"),
		}, nil
	}

	from := now.AddDate(0, 0, -60).Format("2006-01-02")
	to := now.Format("2006-01-02")

	startScene := time.Now()
	scenes, err := s.stacClient.SearchScenes(ctx, lat, lon, from, to)
	s.logger.Debug("fetch current scenes", "duration_ms", time.Since(startScene).Milliseconds(), "count", len(scenes), "error", err)
	if err != nil {
		return nil, err
	}

	if len(scenes) == 0 {
		return nil, fmt.Errorf("no Landsat scenes found for location")
	}

	// Cache the scenes with 24-hour TTL
	s.cache.Set(cacheKey, scenes, 24*time.Hour)

	obs := AggregateObservations(scenes)

	return &VegetationMetrics{
		NDVIMean:       obs.NDVI,
		CloudCover:     obs.CloudCover,
		DaysCount:      len(scenes),
		CollectionDate: now.Format("2006-01-02"),
	}, nil
}

// FetchHistoricalNDVI retrieves 5 years of historical NDVI with caching.
func (s *Service) FetchHistoricalNDVI(ctx context.Context, lat, lon float64) (*VegetationMetrics, error) {
	now := time.Now()
	cacheKey := fmt.Sprintf("historical_%.4f_%.4f", lat, lon)

	// Check if all 5 years are cached
	if cachedScenes, ok := s.cache.Get(cacheKey); ok {
		s.logger.Debug("cache hit", "key", cacheKey)
		// Aggregate and return cached historical data
		results := make([]*ObservationData, 1)
		results[0] = AggregateObservations(cachedScenes)
		return aggregateHistoricalMetrics(results), nil
	}

	years := 5
	results := make([]*ObservationData, years)
	allScenes := []LandsatScene{}

	// Fetch data for each of the last 5 years sequentially
	for i := 0; i < years; i++ {
		year := now.Year() - i - 1
		from := fmt.Sprintf("%d-04-01", year)          // Growing season start
		to := fmt.Sprintf("%d-12-31", year)            // Full year through Dec 31

		startYear := time.Now()
		scenes, err := s.stacClient.SearchScenes(ctx, lat, lon, from, to)
		s.logger.Debug("fetch historical scenes", "year", year, "duration_ms", time.Since(startYear).Milliseconds(), "count", len(scenes), "error", err)
		if err != nil {
			s.logger.Warn("historical scenes fetch failed", "year", year, "error", err)
			continue // Allow partial failures
		}

		if len(scenes) > 0 {
			results[i] = AggregateObservations(scenes)
			allScenes = append(allScenes, scenes...)
		}

		// Add small delay between requests to respect rate limits
		time.Sleep(500 * time.Millisecond)
	}

	// Cache all collected scenes with 30-day TTL (scenes change annually, but keep longer for consistency)
	if len(allScenes) > 0 {
		s.cache.Set(cacheKey, allScenes, 30*24*time.Hour)
		s.logger.Debug("cached historical scenes", "key", cacheKey, "count", len(allScenes))
	}

	return aggregateHistoricalMetrics(results), nil
}

// aggregateHistoricalMetrics combines 5 years of vegetation data.
func aggregateHistoricalMetrics(results []*ObservationData) *VegetationMetrics {
	agg := &VegetationMetrics{}

	var ndviValues []float64
	var cloudCovers []float64

	for _, obs := range results {
		if obs != nil && obs.NDVI > 0 {
			ndviValues = append(ndviValues, obs.NDVI)
			cloudCovers = append(cloudCovers, obs.CloudCover)
		}
	}

	if len(ndviValues) == 0 {
		return agg
	}

	// Calculate NDVI mean
	sum := 0.0
	for _, v := range ndviValues {
		sum += v
	}
	agg.NDVIMean = sum / float64(len(ndviValues))

	// Calculate NDVI standard deviation
	sumSq := 0.0
	for _, v := range ndviValues {
		diff := v - agg.NDVIMean
		sumSq += diff * diff
	}
	agg.NDVIStdDev = sumSq / float64(len(ndviValues))

	// Calculate cloud cover mean
	cloudSum := 0.0
	for _, c := range cloudCovers {
		cloudSum += c
	}
	agg.CloudCover = cloudSum / float64(len(cloudCovers))

	agg.DaysCount = len(ndviValues)
	agg.CollectionDate = time.Now().Format("2006-01-02")

	return agg
}
