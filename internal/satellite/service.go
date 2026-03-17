package satellite

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/shanehull/yieldi.shanehull.com/internal/cache"
)

// Service orchestrates satellite data retrieval using STAC with in-memory caching.
type Service struct {
	stacClient *STACClient
	logger     *slog.Logger
	cache      *cache.Cache[*VegetationMetrics]
}

// NewService creates a new satellite service using STAC (no credentials needed).
func NewService(logger *slog.Logger) *Service {
	return &Service{
		stacClient: NewSTACClient(),
		logger:     logger,
		cache:      cache.New[*VegetationMetrics](),
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

// FetchCurrentNDVI retrieves NDVI for the 60 days preceding asOf with caching.
func (s *Service) FetchCurrentNDVI(ctx context.Context, lat, lon float64, asOf time.Time) (*VegetationMetrics, error) {
	if asOf.IsZero() {
		asOf = time.Now()
	}
	// Round to 4 decimal places (~11m precision) to improve cache hits
	cacheKey := fmt.Sprintf("current_%.4f_%.4f_%s", lat, lon, asOf.Format("2006-01-02"))

	// Check cache first
	if metrics, ok := s.cache.Get(cacheKey); ok {
		s.logger.Debug("cache hit", "key", cacheKey)
		return metrics, nil
	}

	from := asOf.AddDate(0, 0, -60).Format("2006-01-02")
	to := asOf.Format("2006-01-02")

	startScene := time.Now()
	scenes, err := s.stacClient.SearchScenes(ctx, lat, lon, from, to)
	s.logger.Debug("fetch current scenes", "asOf", asOf.Format("2006-01-02"), "duration_ms", time.Since(startScene).Milliseconds(), "count", len(scenes), "error", err)
	if err != nil {
		return nil, err
	}

	if len(scenes) == 0 {
		return nil, fmt.Errorf("no Landsat scenes found for location at %s", asOf.Format("2006-01-02"))
	}

	obs := AggregateObservations(scenes)
	metrics := &VegetationMetrics{
		NDVIMean:       obs.NDVI,
		CloudCover:     obs.CloudCover,
		DaysCount:      len(scenes),
		CollectionDate: asOf.Format("2006-01-02"),
	}

	// Cache the processed metrics with 24-hour TTL
	s.cache.Set(cacheKey, metrics, 24*time.Hour)

	return metrics, nil
}

// FetchHistoricalNDVI retrieves 5 years of historical NDVI preceding the year of asOf.
func (s *Service) FetchHistoricalNDVI(ctx context.Context, lat, lon float64, asOf time.Time) (*VegetationMetrics, error) {
	if asOf.IsZero() {
		asOf = time.Now()
	}
	// Round to 4 decimal places (~11m precision) to improve cache hits
	cacheKey := fmt.Sprintf("historical_%.4f_%.4f_%d", lat, lon, asOf.Year())

	// Check cache first
	if metrics, ok := s.cache.Get(cacheKey); ok {
		s.logger.Debug("cache hit", "key", cacheKey)
		return metrics, nil
	}

	years := 5
	results := make([]*ObservationData, years)

	// Fetch data for each of the 5 years preceding the asOf year concurrently
	var wg sync.WaitGroup
	for i := range years {
		// Capture loop variables
		idx := i
		year := asOf.Year() - i - 1
		from := fmt.Sprintf("%d-04-01", year) // Growing season start
		to := fmt.Sprintf("%d-12-31", year)   // Full year through Dec 31

		wg.Go(func() {
			startYear := time.Now()
			scenes, err := s.stacClient.SearchScenes(ctx, lat, lon, from, to)
			s.logger.Debug("fetch historical scenes", "year", year, "duration_ms", time.Since(startYear).Milliseconds(), "count", len(scenes), "error", err)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					s.logger.Warn("historical scenes fetch failed", "year", year, "error", err)
				}
				return
			}

			if len(scenes) > 0 {
				results[idx] = AggregateObservations(scenes)
			}
		})
	}

	wg.Wait()

	metrics := aggregateHistoricalMetrics(results)

	// Cache the final averaged metrics with 30-day TTL
	if metrics.DaysCount > 0 {
		s.cache.Set(cacheKey, metrics, 30*24*time.Hour)
		s.logger.Debug("cached historical metrics", "key", cacheKey)
	}

	return metrics, nil
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
