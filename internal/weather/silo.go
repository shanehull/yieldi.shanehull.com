package weather

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client manages requests to the SILO API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new SILO API client.
// Uses the DataDrill endpoint with publicly available credentials (email + 'apirequest').
func NewClient() *Client {
	return &Client{
		baseURL: "https://www.longpaddock.qld.gov.au/cgi-bin/silo/DataDrillDataset.php",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// RainfallData holds aggregated rainfall statistics.
type RainfallData struct {
	DailyValues []float64
	MeanDaily   float64
	TotalRain   float64
	DaysCount   int
}

// FetchRainfall retrieves daily rainfall data from SILO DataDrill API.
// Uses public credentials: username is a placeholder email, password is 'apirequest'.
func (c *Client) FetchRainfall(ctx context.Context, lat, lon float64, start, end string) (*RainfallData, error) {
	// Convert dates from YYYY-MM-DD to YYYYMMDD format for SILO
	startDate := strings.ReplaceAll(start, "-", "")
	endDate := strings.ReplaceAll(end, "-", "")

	// Build SILO query parameters
	// SILO DataDrill requires format, lat, lon, start, finish, username (email), password
	params := url.Values{}
	params.Set("format", "standard")
	params.Set("lat", fmt.Sprintf("%.4f", lat))
	params.Set("lon", fmt.Sprintf("%.4f", lon))
	params.Set("start", startDate)
	params.Set("finish", endDate)
	params.Set("username", "apirequest@qld.gov.au")
	params.Set("password", "apirequest")

	queryURL := c.baseURL + "?" + params.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, queryURL, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("silo request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.ReadAll(resp.Body)
		return nil, fmt.Errorf("silo api error: %d", resp.StatusCode)
	}

	// Parse SILO standard format (CSV-like text)
	body, _ := io.ReadAll(resp.Body)
	return parseSILOResponse(string(body)), nil
}

// parseSILOResponse parses the standard CSV-like text format from SILO DataDrill.
// Format: date (YYYYMMDD) daily_rain (mm) other columns...
// Lines starting with # are headers or metadata.
func parseSILOResponse(body string) *RainfallData {
	result := &RainfallData{
		DailyValues: make([]float64, 0),
	}

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split by whitespace
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		// Parse daily_rain (second column)
		rainfall, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || rainfall < 0 {
			// SILO uses -999 for missing data
			continue
		}

		result.DailyValues = append(result.DailyValues, rainfall)
		result.TotalRain += rainfall
		result.DaysCount++
	}

	if result.DaysCount > 0 {
		result.MeanDaily = result.TotalRain / float64(result.DaysCount)
	}

	return result
}

// RetryConfig holds retry parameters.
type RetryConfig struct {
	MaxAttempts int
	InitialWait time.Duration
	MaxWait     time.Duration
}

// DefaultRetryConfig returns standard retry settings.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		InitialWait: 1 * time.Second,
		MaxWait:     5 * time.Second,
	}
}

// FetchRainfallWithRetry retrieves rainfall data with exponential backoff retry.
func (c *Client) FetchRainfallWithRetry(ctx context.Context, lat, lon float64, start, end string, cfg RetryConfig) (*RainfallData, error) {
	var lastErr error
	wait := cfg.InitialWait

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		data, err := c.FetchRainfall(ctx, lat, lon, start, end)
		if err == nil {
			return data, nil
		}

		lastErr = err

		if attempt < cfg.MaxAttempts-1 {
			select {
			case <-time.After(wait):
				// Continue to next attempt
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			// Exponential backoff
			wait = wait * 2
			if wait > cfg.MaxWait {
				wait = cfg.MaxWait
			}
		}
	}

	return nil, fmt.Errorf("rainfall fetch failed after %d attempts: %w", cfg.MaxAttempts, lastErr)
}
