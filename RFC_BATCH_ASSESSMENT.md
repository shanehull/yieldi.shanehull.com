# RFC: Batch Polygon Assessment

**Status:** Proposed  
**Author:** Shane Hull  
**Date:** 2026-03-17

## Summary

Enable the assessment of multiple field polygons in a single request, with efficient data fetching and per-polygon results. Currently, the API processes one polygon at a time, forcing clients to make sequential requests.

## Problem Statement

Farmers manage multiple paddocks. Current workflow:

1. Draw polygon for field A
2. Wait for satellite/weather data fetch
3. Get results for field A
4. Clear map, repeat for field B, C, D...

This is inefficient and doesn't leverage opportunities to reuse data for nearby locations.

## Proposed Solution

### API Changes

**New endpoint:** `POST /api/assess-batch`

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

**Response format:**

```json
{
  "assessment_date": "2025-09-15",
  "fetched_at": "2025-03-17T20:15:00Z",
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
  AssessmentDate string `json:"assessment_date"`
  HarvestDate    string `json:"harvest_date"`
  SeasonDays     int    `json:"season_days"`
  Polygons       []PolygonInput `json:"polygons"`
  Alpha          float64 `json:"alpha"`
  Beta1          float64 `json:"beta1"`
  Beta2          float64 `json:"beta2"`
}

type PolygonInput struct {
  ID              string      `json:"id"`
  Geometry        GeoJSON     `json:"geometry"`
  BaselineYield   float64     `json:"baseline_yield"`
  TargetHedge     float64     `json:"target_hedge"`
}

type DataSource struct {
  Centroid           geo.Point
  NDVIHistorical     float64
  NDVICurrent        float64
  RainfallHistorical float64
  RainfallCurrent    float64
}

type BatchAssessResponse struct {
  AssessmentDate string                    `json:"assessment_date"`
  FetchedAt      time.Time                 `json:"fetched_at"`
  DataSources    map[string]DataSource     `json:"data_sources"`
  Results        []AssessResponse          `json:"results"`
}
```

**New handler:** `HandleAssessBatch(w http.ResponseWriter, r *http.Request)`

**New service method:** `Service.FetchBatchData(ctx context.Context, polygons []PolygonInput, asOf time.Time) map[string]DataSource`

### UI/UX Changes

**Multi-polygon drawing:**

- Allow drawing multiple polygons without clearing
- Each polygon gets a unique color/ID
- Display list of drawn polygons with their field sizes

**Results display:**

- Keep current sidebar for single polygon
- Add polygon selector dropdown or list
- Click polygon in list to view its results
- Summary table showing all polygons:
  | Field | Yield (t/ha) | vs Baseline | Hedge Ratio | Confidence |
  | ----- | ------------ | ----------- | ----------- | ---------- |
  | A | 2.27 | -9% | 54.5% | 100% |
  | B | 2.45 | -2% | 58% | 100% |

**Export functionality:**

- Download results as CSV,JSON
- Include polygon ID, field size, all metrics

**Map interaction:**

- Highlight polygon on hover
- Click polygon to view its results
- Optional: color-code by yield performance (red=low, green=high)

### Shareable URLs (Batch Assessment)

Current URL encoding supports single polygons. For batch assessment, extend to support multiple polygons in URL state:

**URL Parameters (batch):**

- `c` - **Multiple polygons** (pipe-separated): `c=poly1_coords;poly1_id|poly2_coords;poly2_id`
  - Example: `c=-34.8704,143.4925;-34.8705,143.4930;fieldA|-34.9000,143.5000;-34.9100,143.5100;fieldB`
- Other parameters remain unchanged (single assessment_date, harvest_date, coefficients for all polygons)

**Alternative approach:** Generate single-use batch assessment ID stored server-side:

- `bid=abc123def456` references stored batch with all polygon/config data
- Cleaner URLs, but requires server-side state (consider for v2)

**Recommendation:** Use pipe-separated approach for MVP. Simple, stateless, shareable URLs. Migrate to batch IDs if URL length becomes issue (>2000 chars).

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

### Testing

- Unit tests for centroid clustering logic
- Integration test with mock STAC/SILO (verify data reuse)
- Test error cases (malformed geometry, missing fields, API failures)
- Performance test with 50+ polygons

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

## References

- Current API: `/api/assess` in `internal/handlers/handlers.go`
- Satellite service: `internal/satellite/service.go`
- Weather service: `internal/weather/service.go`
- Model: `internal/quant/model.go`
