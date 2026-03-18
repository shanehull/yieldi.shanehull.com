# RFC: Batch Polygon Assessment

**Status:** Proposed  
**Author:** Shane Hull  
**Date:** 2026-03-17

## Summary

Enable the assessment of multiple field polygons in a single request via file upload (GeoJSON/KML/KMZ/Shapefile) or JSON API, with efficient data fetching and per-polygon results. Currently, the API processes one polygon at a time, forcing clients to make sequential requests or draw fields manually.

## Problem Statement

Farmers manage multiple paddocks. Current workflow:

1. Manually draw polygon for field A on web UI
2. Wait for satellite/weather data fetch
3. Get results for field A
4. Clear map, repeat for field B, C, D...
5. Or worse: export GIS file but no way to batch assess

This is inefficient. Real workflows involve exporting field boundaries from GIS/farm management software (Agworld, AgWorld, QGIS, etc.).

## Proposed Solution

### API Changes

**Two request methods:**

#### Method 1: File Upload (Primary)

**Endpoint:** `POST /api/assess-batch`

**Content-Type:** `multipart/form-data`

**Form fields:**

- `file` - GeoJSON, KML, KMZ, or Shapefile (.zip containing .shp, .shx, .dbf)
- `assessment_date` - Optional; RFC3339 format (default: now)
- `harvest_date` - Required; YYYY-MM-DD format
- `season_days` - Required; integer days
- `baseline_yield` - Required; t/ha (applied to all features unless overridden by feature property)
- `target_hedge` - Required; 0-1 scale
- `alpha` - Optional; default 0.2
- `beta1` - Optional; default 0.7
- `beta2` - Optional; default 0.1

**Feature properties in uploaded file** (optional, override defaults):

GeoJSON/Shapefile features can include properties to override global parameters:

```json
{
  "type": "Feature",
  "properties": {
    "id": "field_a",
    "name": "North Paddock",
    "baseline_yield": 2.5,
    "target_hedge": 0.60
  },
  "geometry": {"type": "Polygon", "coordinates": [...]}
}
```

If feature lacks `id`, use `name` or generate UUID.

#### Method 2: JSON API (Alternative/Programmatic)

**Endpoint:** `POST /api/assess-batch`

**Content-Type:** `application/json`

**Request format:**

```json
{
  "assessment_date": "2025-09-15",
  "harvest_date": "2025-11-15",
  "season_days": 62,
  "polygons": [
    {
      "id": "field_a",
      "geometry": {"type": "Polygon", "coordinates": [...]},
      "baseline_yield": 2.5,
      "target_hedge": 0.60
    },
    {
      "id": "field_b",
      "geometry": {"type": "Polygon", "coordinates": [...]},
      "baseline_yield": 2.8,
      "target_hedge": 0.55
    }
  ],
  "alpha": 0.2,
  "beta1": 0.7,
  "beta2": 0.1
}
```

### Supported File Formats

**Priority order (implementation):**

1. **GeoJSON** (.geojson, .json)
   - Native support, no external libs needed
   - Features can include property overrides
   - Widely exported from QGIS, ArcGIS, Agworld

2. **KML** (.kml)
   - Compress/extract if .kmz (standard ZIP)
   - Parse Placemark/Polygon elements
   - Property map from `<name>` and `<description>` tags

3. **Shapefile** (.zip containing .shp, .shx, .dbf)
   - Extract ZIP, parse using library (e.g., `github.com/jonas-p/go-shp`)
   - Read attributes from .dbf file
   - Support multi-part polygons

**Validation:**

- All features must be Polygon or MultiPolygon type
- Minimum 3 points per polygon ring
- Coordinates in [lon, lat] (WGS84)
- Reject KML with > 100 features or Shapefiles > 10MB uncompressed

**Error handling:**

Return 400 with detailed message:

```json
{
  "error": "File parsing failed",
  "details": "Feature 'field_3' at index 2: Invalid geometry type. Expected Polygon, got Point"
}
```

### Response Format (Both Methods)

```json
{
  "assessment_date": "2025-09-15",
  "fetched_at": "2025-03-17T20:15:00Z",
  "file_name": "fields.geojson",
  "total_features": 2,
  "successful": 2,
  "failed": 0,
  "data_sources": {
    "location_1": {
      "centroid": {"lat": -34.87, "lon": 143.49},
      "ndvi_historical": 0.596,
      "ndvi_current": 0.598,
      "rainfall_historical": 29773.2,
      "rainfall_current": 29706
    }
  },
  "results": [
    {
      "polygon_id": "field_a",
      "yield_estimate": 2.2713,
      "yield_baseline": 2.5,
      "yield_delta_percent": -9.15,
      "hedge_ratio": 0.5451,
      "target_hedge_ratio": 0.60,
      "total_yield_estimate": 161.5,
      "total_hedge_volume": 88.6,
      "ndvi_anomaly": 1.0125,
      "rainfall_delta": -0.0023,
      "cloud_cover": 0.3421,
      "low_confidence": true,
      "confidence": 100,
      "days_to_harvest": 76
    },
    {
      "polygon_id": "field_b",
      ...
    }
  ]
}
```

**New fields:**
- `file_name` - Name of uploaded file (if file upload)
- `total_features` - Total features parsed from file
- `successful` - Number of successfully assessed polygons
- `failed` - Number of failed polygons (included in results with `error` field)

### Data Fetching Strategy

**Centroid clustering:** Group polygons by proximity (within 5km radius). Use a single satellite/weather fetch per cluster.

**Implementation:**

1. Calculate centroids for all polygons
2. Cluster by K-D tree or simple distance matrix
3. For each cluster, fetch NDVI and rainfall once
4. Cache results with cluster hash
5. Assign cached data to all polygons in cluster

**Benefit:** A farm with 10 fields within 2km gets only 1 set of satellite/weather calls instead of 10.

### Backend Structure

**New types in `internal/handlers/handlers.go`:**

```go
type BatchAssessRequest struct {
  AssessmentDate string           `json:"assessment_date"`
  HarvestDate    string           `json:"harvest_date"`
  SeasonDays     int              `json:"season_days"`
  Polygons       []PolygonInput   `json:"polygons"`
  Alpha          float64          `json:"alpha"`
  Beta1          float64          `json:"beta1"`
  Beta2          float64          `json:"beta2"`
}

type PolygonInput struct {
  ID              string      `json:"id"`
  Geometry        interface{} `json:"geometry"` // GeoJSON Polygon or MultiPolygon
  BaselineYield   float64     `json:"baseline_yield"`
  TargetHedge     float64     `json:"target_hedge"`
}

type DataSource struct {
  Centroid           [2]float64 `json:"centroid"` // [lat, lon]
  NDVIHistorical     float64    `json:"ndvi_historical"`
  NDVICurrent        float64    `json:"ndvi_current"`
  RainfallHistorical float64    `json:"rainfall_historical"`
  RainfallCurrent    float64    `json:"rainfall_current"`
}

type BatchAssessResponse struct {
  AssessmentDate string                 `json:"assessment_date"`
  FetchedAt      time.Time              `json:"fetched_at"`
  FileName       string                 `json:"file_name,omitempty"`
  TotalFeatures  int                    `json:"total_features,omitempty"`
  Successful     int                    `json:"successful"`
  Failed         int                    `json:"failed"`
  DataSources    map[string]DataSource  `json:"data_sources"`
  Results        []AssessResponse       `json:"results"`
}
```

**New package `internal/geo/` for file parsing:**

```go
// internal/geo/parser.go
type FileParser interface {
  Parse(data []byte) ([]PolygonInput, error)
}

type GeoJSONParser struct{}
func (p *GeoJSONParser) Parse(data []byte) ([]PolygonInput, error) { ... }

type KMLParser struct{}
func (p *KMLParser) Parse(data []byte) ([]PolygonInput, error) { ... }

type ShapefileParser struct{}
func (p *ShapefileParser) Parse(data []byte) ([]PolygonInput, error) { ... } // Expects ZIP

func DetectAndParse(filename string, data []byte) ([]PolygonInput, error) {
  // Detect format by extension and content
  // Delegate to appropriate parser
}
```

**New handlers:**

- `HandleAssessBatch(w http.ResponseWriter, r *http.Request)` - Entry point for both file upload and JSON API
  - Detects Content-Type (multipart/form-data vs application/json)
  - Routes to appropriate parsing logic
  
**New service method in handlers:**

- `assessBatchPolygons(ctx context.Context, polygons []PolygonInput, params BatchParams) ([]AssessResponse, error)`
  - Implements centroid clustering
  - Fetches data per cluster
  - Applies model to each polygon
  - Handles partial failures gracefully

### UI/UX Changes

**File upload interface:**

- Add "Upload File" button in control panel
- Support drag-and-drop for GeoJSON/KML/KMZ/Shapefile
- Show upload progress and parsing status
- Display error if file is invalid (format, size, geometry)

**Two workflows:**

1. **File Upload (Primary)**
   - User uploads GeoJSON/KML/Shapefile
   - System parses and displays all features on map
   - Form fields (baseline_yield, target_hedge, dates) apply to all features unless overridden in file properties
   - Click "Assess All" to batch process

2. **Manual Drawing (Existing)**
   - Allow drawing multiple polygons without clearing
   - Each polygon gets a unique color/ID
   - Display list of drawn polygons with their field sizes
   - Option to "Assess All" at the end

**Results display (Batch):**

- Keep current sidebar for quick view of first polygon
- Add **polygon selector dropdown** or scrollable list
- Click polygon in list to view its detailed results
- **Summary table** showing all polygons:

  | Field | Yield (t/ha) | vs Baseline | Hedge (t/ha) | Confidence | Days |
  | ----- | ------------ | ----------- | ------------ | ---------- | ---- |
  | A | 2.27 | -9% | 1.36 | 100% | 76 |
  | B | 2.45 | -2% | 1.47 | 100% | 76 |

- Click row to highlight polygon on map and show full details

**Export functionality:**

- Download results as **CSV** (tabular summary)
- Download as **JSON** (full response including data_sources)
- Include polygon ID, field size, all metrics
- Bulk export with timestamp

**Map interaction:**

- Highlight polygon on hover
- Click polygon to view its results
- Color-code by yield performance:
  - Green: > 10% above baseline
  - Yellow: ±10% of baseline
  - Red: > 10% below baseline
- Show tooltip with field ID/name on hover

### Shareable URLs (Batch Assessment)

**Single Polygon (≤5 polygons):**

URL encoding supports direct polygon coordinates:

- `c` - Polygon coordinates (pipe-separated): `c=lat,lng;lat,lng;...`
- `ad` - Assessment date, `hd` - Harvest date, `by` - Baseline yield, etc.
- Example: `?c=-34.8704,143.4925;-34.8705,143.4930&ad=2025-09-15&hd=2025-11-15`

**Multiple Polygons (>5 polygons):**

URL length constraints (browser limit ~2000 chars) prevent encoding >5 polygons directly. For larger batches:

- **Use file upload workflow** — User uploads GeoJSON/KMZ, system assesses, returns results
- **Do NOT include polygon coordinates in URL** — Prevents 100KB+ URLs

**Future Enhancement (Phase 2):**

Server-side batch result IDs:
- `bid=abc123def456` references stored batch assessment with all polygons/config
- Cleaner URLs, shorter, shareable indefinitely
- Requires persistent database storage (results retained long-term)

### Backward Compatibility

Keep existing `/api/assess` endpoint unchanged. Single-polygon clients continue to work without modification.

### Error Handling

**Partial failures:** If one polygon fails (bad geometry, API timeout), return error in that result but continue processing others.

**Structure:**

```json
{
  "polygon_id": "field_c",
  "error": "Invalid geometry: Polygon must have at least 3 points"
}
```

**All polygons** in same request share same `assessment_date`, `harvest_date`, coefficients. No per-polygon overrides.

### Testing Strategy

**Test Data:**

GeoJSON test files in `/static/test-data/`:

**Hand-drawn (Real Paddocks):**
- `mallee-single.geojson` — 1 paddock from Mallee, Victoria, baseline 2.5 t/ha, 60% hedge (quick smoke test)
- `mallee-two-paddocks.geojson` — 2 adjacent paddocks from Mallee, Victoria (clustering test)

**AI-derived (ePaddocks™ CSIRO):**
- `moree-sample-small.geojson` — 11 paddocks from Moree, NSW (unit/integration tests)
- `moree-sample.geojson` — 333 paddocks from Moree, NSW, range 1–595.5 ha (performance/scale tests)

All features include properties: `id`, `name`, `baseline_yield`, `target_hedge`.

**Test Cases:**

- **Unit tests:** Centroid clustering logic, file parsing (GeoJSON validation)
- **Integration tests:** File upload → parsing → clustering → batch assessment with mock STAC/SILO
- **Error handling:** Malformed geometry, missing fields, API failures, partial failures (5 polygons, 1 fails)
- **Performance:** 333 Moree paddocks → verify clustering reduces API calls, measure batch response time
- **Format support:** GeoJSON parsing and validation (KML/Shapefile parsing tested separately)

### Performance Considerations

**Worst case:** 100 polygons scattered across region = 100 unique centroids = 100 API calls. Same as current sequential approach.

**Best case:** 100 polygons in single 5km area = 1 API call + 100 parallel yield calculations.

**Caching:** Results cached by centroid hash, so repeated assessments of same polygons are instant.

### Future Enhancements

- Multi-season comparison (compare 2025 vs 2024 for same fields)
- Field-by-field variance analysis (which paddocks are struggling)
- Portfolio-level hedge strategy (optimize total hedge across farms)

## Open Questions

1. Should UI allow per-polygon coefficient overrides (different alpha/beta1/beta2 per field)?
   - **Recommendation:** No. Coefficients are model-wide. Different field types would require different model instances.

2. Should results be sorted or ordered?
   - **Recommendation:** Return in same order as input. Client controls ordering.

3. Should there be a maximum polygon limit?
   - **Recommendation:** Yes. Set to 100 per request to prevent abuse and API saturation.

4. Which file format to prioritize first?
   - **Recommendation:** GeoJSON (no external dependencies, widely supported, easiest parsing). Then KML (many GIS platforms export this). Shapefile last (requires ZIP handling + go-shp library).

5. Should per-feature property overrides (baseline_yield, target_hedge) be supported?
   - **Recommendation:** Yes, but optional. Allows power users to specify different yields/hedge ratios per field. Falls back to global defaults if feature properties missing.

6. How to handle Shapefiles with missing DBF data?
   - **Recommendation:** Generate feature IDs as "feature_0", "feature_1", etc. Use polygon index as fallback.

## References

- Current API: `/api/assess` in `internal/handlers/handlers.go`
- Satellite service: `internal/satellite/service.go`
- Weather service: `internal/weather/service.go`
- Model: `internal/quant/model.go`
