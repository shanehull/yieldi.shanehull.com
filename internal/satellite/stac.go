package satellite

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// STACClient queries the USGS Landsat STAC catalog.
// No authentication required - STAC is free and public.
type STACClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewSTACClient creates a new STAC client for Landsat data.
// Uses Element84's Earth Search STAC API which provides free, queryable Landsat Collection 2.
func NewSTACClient() *STACClient {
	return &STACClient{
		baseURL: "https://earth-search.aws.element84.com/v1",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SearchScenes queries S3 for Landsat Collection 2 Level 2 scenes.
// Uses known WRS paths for Victoria, Australia (Path 91-93, Row 85-86).
func (s *STACClient) SearchScenes(ctx context.Context, lat, lon float64, startDate, endDate string) ([]LandsatScene, error) {
	// Validate date range
	if startDate > endDate {
		return []LandsatScene{}, nil
	}

	// Query Earth Search STAC API for Landsat Collection 2 scenes
	// Build request for any location globally
	bbox := fmt.Sprintf("%.4f,%.4f,%.4f,%.4f", lon-0.05, lat-0.05, lon+0.05, lat+0.05)
	searchURL := fmt.Sprintf("%s/search?collections=landsat-c2-l2&bbox=%s&datetime=%sT00:00:00Z/%sT23:59:59Z&limit=50",
		s.baseURL, bbox, startDate, endDate)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("STAC search failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		// If search fails, return empty to use fallback
		return []LandsatScene{}, nil
	}

	body, _ := io.ReadAll(resp.Body)

	// Parse STAC GeoJSON response
	var response struct {
		Features []struct {
			ID         string `json:"id"`
			Properties struct {
				DateTime   string  `json:"datetime"`
				CloudCover float64 `json:"eo:cloud_cover"`
			} `json:"properties"`
			Assets map[string]struct {
				HRef string `json:"href"`
			} `json:"assets"`
		} `json:"features"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse STAC response: %w", err)
	}

	var scenes []LandsatScene
	for _, feature := range response.Features {
		acqDate := feature.Properties.DateTime
		if len(acqDate) >= 10 {
			acqDate = acqDate[:10]
		}

		redURL := ""
		nirURL := ""
		if b4, ok := feature.Assets["red"]; ok {
			redURL = b4.HRef
		}
		if b5, ok := feature.Assets["nir08"]; ok {
			nirURL = b5.HRef
		}

		if redURL != "" && nirURL != "" {
			scenes = append(scenes, LandsatScene{
				AcquisitionDate: acqDate,
				CloudCover:      feature.Properties.CloudCover, // STAC returns 0-1 scale
				EntityID:        feature.ID,
				RedBandURL:      redURL,
				NIRBandURL:      nirURL,
			})
		}
	}

	return scenes, nil
}
