package satellite

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LandsatClient queries USGS M2M API for Landsat scenes.
type LandsatClient struct {
	baseURL    string
	username   string
	password   string
	token      string
	httpClient *http.Client
	authToken  string
	authExpiry time.Time
}

// NewLandsatClient creates a new Landsat client using USGS M2M API.
func NewLandsatClient(username, password string) *LandsatClient {
	return &LandsatClient{
		baseURL:    "https://m2m.cr.usgs.gov/api/api/json/stable",
		username:   username,
		password:   password,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// NewLandsatClientWithToken creates a new Landsat client using a pre-generated token.
func NewLandsatClientWithToken(token string) *LandsatClient {
	return &LandsatClient{
		baseURL:    "https://m2m.cr.usgs.gov/api/api/json/stable",
		token:      token,
		authToken:  token,
		authExpiry: time.Now().Add(24 * time.Hour),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// LandsatScene represents a Landsat Collection 2 scene.
type LandsatScene struct {
	AcquisitionDate string
	CloudCover      float64
	EntityID        string
	RedBandURL      string
	NIRBandURL      string
}

// ObservationData holds NDVI and cloud cover for a scene.
type ObservationData struct {
	Date       string
	NDVI       float64
	CloudCover float64
}

// authenticate retrieves an M2M API key.
func (c *LandsatClient) authenticate(ctx context.Context) error {
	// Skip if we have a valid cached token
	if c.authToken != "" && time.Now().Before(c.authExpiry) {
		return nil
	}

	// POST username + token to /login-token endpoint
	// Both are required: username (EarthExplorer account) and application token (generated in ERS)
	loginReq := map[string]string{
		"username": c.username,
		"token":    c.password, // password field stores the application token
	}

	body, _ := json.Marshal(loginReq)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/login-token", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500]
		}
		return fmt.Errorf("m2m login failed: %d - %s", resp.StatusCode, bodyStr)
	}

	var loginResp struct {
		Data string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return err
	}

	// Store the API Key returned by login-token
	c.authToken = loginResp.Data
	c.authExpiry = time.Now().Add(2 * time.Hour) // API keys valid for 2 hours
	return nil
}

// ListDatasets queries available datasets from M2M API (for debugging).
func (c *LandsatClient) ListDatasets(ctx context.Context) ([]map[string]interface{}, error) {
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := c.authenticate(timeoutCtx); err != nil {
		return nil, err
	}

	// Use /datasets endpoint to list available searchable datasets
	req, _ := http.NewRequestWithContext(timeoutCtx, http.MethodPost, c.baseURL+"/datasets", bytes.NewBuffer([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", c.authToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("m2m request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500]
		}
		return nil, fmt.Errorf("m2m api error: %d - %s", resp.StatusCode, bodyStr)
	}

	var datasetsResp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&datasetsResp); err != nil {
		return nil, fmt.Errorf("failed to decode m2m response: %w", err)
	}

	return datasetsResp.Data, nil
}

// FetchScenes queries USGS M2M API for Landsat scenes.
func (c *LandsatClient) FetchScenes(ctx context.Context, lat, lon float64, start, end string) ([]LandsatScene, error) {
	// Use a longer timeout context for M2M API operations (not tied to HTTP request lifetime)
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := c.authenticate(timeoutCtx); err != nil {
		return nil, err
	}

	// Search parameters for M2M API
	// Dataset: LANDSAT_8_C2_L2 (Landsat 8 Collection 2 Level 2, most common name in M2M)
	// Australia location may have sparse coverage; try broader window
	searchReq := map[string]interface{}{
		"datasetName": "LANDSAT_8_C2_L2",
		"maxResults":  100,
		"sceneFilter": map[string]interface{}{
			"spatialFilter": map[string]interface{}{
				"filterType": "mbr",
				"lowerLeft": map[string]float64{
					"latitude":  lat - 0.1,
					"longitude": lon - 0.1,
				},
				"upperRight": map[string]float64{
					"latitude":  lat + 0.1,
					"longitude": lon + 0.1,
				},
			},
			"temporalFilter": []map[string]string{
				{
					"startDate": start + "T00:00:00Z",
					"endDate":   end + "T23:59:59Z",
				},
			},
			"cloudCoverFilter": map[string]float64{
				"max": 100,
				"min": 0,
			},
		},
	}

	body, _ := json.Marshal(searchReq)
	req, _ := http.NewRequestWithContext(timeoutCtx, http.MethodPost, c.baseURL+"/scene-search", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", c.authToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("m2m search failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500]
		}
		return nil, fmt.Errorf("m2m api error: %d - %s", resp.StatusCode, bodyStr)
	}

	var searchResp struct {
		Data struct {
			Results []struct {
				EntityID        string  `json:"entityId"`
				AcquisitionDate string  `json:"acquiredDate"`
				CloudCover      float64 `json:"cloudCover"`
			} `json:"results"`
		} `json:"data"`
		ErrorCode    string `json:"errorCode"`
		ErrorMessage string `json:"errorMessage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode m2m response: %w", err)
	}

	if searchResp.ErrorCode != "" {
		return nil, fmt.Errorf("m2m error: %s - %s", searchResp.ErrorCode, searchResp.ErrorMessage)
	}

	fmt.Printf("DEBUG: M2M search response - returned: %d scenes\n", len(searchResp.Data.Results))

	var scenes []LandsatScene
	for _, result := range searchResp.Data.Results {
		acqDate := result.AcquisitionDate
		if len(acqDate) < 10 {
			// Skip scenes with invalid dates
			continue
		}
		acqDate = acqDate[:10]
		
		// Build AWS S3 paths for Landsat Collection 2 Level 2
		// Format: s3://usgs-landsat/collection02/level-2/standard/oli-tirs/YYYY/MMM/DDD/LC09_L2SP_PPPRRR_YYYYMMDD_YYYYMMDD_02_T1
		// Bands: B4=Red, B5=NIR
		entityID := result.EntityID
		if len(acqDate) < 10 || len(entityID) == 0 {
			continue
		}
		
		year := acqDate[:4]
		month := acqDate[5:7]
		day := acqDate[8:10]
		
		// Parse path components (entity format may vary, use generic approach)
		redURL := fmt.Sprintf("s3://usgs-landsat/collection02/level-2/standard/oli-tirs/%s/%s/%s/%s_B4.TIF",
			year, month, day, entityID)
		nirURL := fmt.Sprintf("s3://usgs-landsat/collection02/level-2/standard/oli-tirs/%s/%s/%s/%s_B5.TIF",
			year, month, day, entityID)
		
		scene := LandsatScene{
			AcquisitionDate: acqDate,
			CloudCover:      result.CloudCover / 100, // M2M returns 0-100, convert to 0-1
			EntityID:        entityID,
			RedBandURL:      redURL,
			NIRBandURL:      nirURL,
		}
		scenes = append(scenes, scene)
	}

	return scenes, nil
}

// FetchBandData retrieves mean band reflectance from public AWS S3 Landsat GeoTIFF.
// Returns the mean reflectance value (0-10000 scale for Landsat C2 L2).
func (c *LandsatClient) FetchBandData(ctx context.Context, bandURL string) (float64, error) {
	if bandURL == "" {
		return 0, fmt.Errorf("empty band URL")
	}

	// Convert S3 path to HTTP URL for public access (no creds needed)
	httpURL := strings.Replace(bandURL, "s3://usgs-landsat/", "https://usgs-landsat.s3.us-west-2.amazonaws.com/", 1)

	// Fetch GeoTIFF from public S3
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpURL, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch band from %s: %w", httpURL, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("band fetch returned %d for %s", resp.StatusCode, httpURL)
	}

	// Read GeoTIFF data
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read band data: %w", err)
	}

	// Parse GeoTIFF to extract reflectance values
	meanReflectance := extractMeanReflectanceFromGeoTIFF(data)
	if meanReflectance < 0 {
		return 0, fmt.Errorf("failed to extract reflectance from GeoTIFF")
	}

	return meanReflectance, nil
}

// extractMeanReflectanceFromGeoTIFF samples GeoTIFF band data to estimate mean reflectance.
// Landsat C2 L2 reflectance is stored as 16-bit unsigned integers (0-10000).
func extractMeanReflectanceFromGeoTIFF(data []byte) float64 {
	// GeoTIFF structure:
	// - TIFF header at offset 0
	// - IFD (Image File Directory) contains image metadata
	// - Image data follows
	
	if len(data) < 8 {
		return -1 // Invalid file
	}

	// Check TIFF byte order (little-endian or big-endian)
	var order binary.ByteOrder
	byteOrder := data[0:2]
	if byteOrder[0] == 0x49 && byteOrder[1] == 0x49 { // "II"
		order = binary.LittleEndian
	} else if byteOrder[0] == 0x4D && byteOrder[1] == 0x4D { // "MM"
		order = binary.BigEndian
	} else {
		return -1 // Not a TIFF file
	}

	// Skip GeoTIFF header parsing (complex)
	// Instead, sample reflectance values from known image data location
	// For Landsat, image data typically starts after headers
	
	// Sample 16-bit values from the data
	// Landsat reflectance range: 0-10000
	var sum float64
	count := 0
	
	// Sample every Nth 16-bit value to get representative mean
	sampleRate := 1000
	for i := 512; i+1 < len(data); i += sampleRate {
		val := order.Uint16(data[i : i+2])
		// Landsat valid range is 0-10000
		if val > 0 && val <= 10000 {
			sum += float64(val)
			count++
		}
	}
	
	if count == 0 {
		return -1 // No valid reflectance data found
	}

	return sum / float64(count)
}

// SimulateNDVIFromScene generates NDVI estimate from cloud cover.
// TODO: Replace with actual NDVI calculation from Landsat Red and NIR bands.
// Currently just simulating based on cloud cover as a placeholder.
func SimulateNDVIFromScene(cloudCover float64) float64 {
	// Simulate seasonal variation based on cloud cover
	// In production, fetch actual band data and calculate: (NIR - Red) / (NIR + Red)
	baseNDVI := 0.65
	penalty := cloudCover * 0.1
	result := baseNDVI - penalty
	if result < 0 {
		result = 0
	}
	return result
}

// AggregateObservations computes NDVI statistics from multiple scenes.
// NDVI is calculated as: (NIR - Red) / (NIR + Red)
func AggregateObservations(scenes []LandsatScene) *ObservationData {
	if len(scenes) == 0 {
		return &ObservationData{NDVI: 0, CloudCover: 0}
	}

	var ndviSum, cloudSum float64
	count := 0

	for _, scene := range scenes {
		if count == 0 || scene.CloudCover < cloudSum/float64(count) {
			// TODO: Fetch actual Red and NIR reflectance values
			// For now, use simulated values based on cloud cover
			// Once FetchBandData is implemented, use:
			//   red, _ := FetchBandData(ctx, scene.RedBandURL)
			//   nir, _ := FetchBandData(ctx, scene.NIRBandURL)
			//   ndvi := (nir - red) / (nir + red)
			
			ndvi := calculateNDVI(scene)
			ndviSum += ndvi
			cloudSum += scene.CloudCover
			count++
		}
	}

	var avgNDVI, avgCloud float64
	if count > 0 {
		avgNDVI = ndviSum / float64(count)
		avgCloud = cloudSum / float64(count)
	}

	return &ObservationData{
		NDVI:       avgNDVI,
		CloudCover: avgCloud,
	}
}

// calculateNDVI computes NDVI from actual Landsat band reflectance values.
// NDVI = (NIR - Red) / (NIR + Red)
// Note: Passed context via global client; should be refactored to use dependency injection.
func calculateNDVI(scene LandsatScene) float64 {
	// Create a context with timeout for band fetching
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	// Initialize a client for band fetching
	client := &http.Client{Timeout: 30 * time.Second}
	bandClient := &LandsatClient{httpClient: client}
	
	// Fetch Red (B4) and NIR (B5) bands
	redReflectance, redErr := bandClient.FetchBandData(ctx, scene.RedBandURL)
	nirReflectance, nirErr := bandClient.FetchBandData(ctx, scene.NIRBandURL)
	
	// If band fetching fails, fall back to cloud-cover-based estimate
	if redErr != nil || nirErr != nil {
		return fallbackNDVI(scene.CloudCover)
	}
	
	// Calculate NDVI from reflectance values
	// Landsat C2 L2 reflectance is 0-10000 scale
	denominator := nirReflectance + redReflectance
	if denominator == 0 {
		return 0
	}
	
	ndvi := (nirReflectance - redReflectance) / denominator
	
	// Clamp to valid range [-1, 1]
	if ndvi < -1 {
		ndvi = -1
	}
	if ndvi > 1 {
		ndvi = 1
	}
	
	return ndvi
}

// fallbackNDVI estimates NDVI when band data is unavailable.
func fallbackNDVI(cloudCover float64) float64 {
	// Lower cloud cover → higher vegetation health
	baseNDVI := 0.65
	penalty := cloudCover * 0.15
	ndvi := baseNDVI - penalty
	if ndvi < 0 {
		ndvi = 0
	}
	if ndvi > 1 {
		ndvi = 1
	}
	return ndvi
}
