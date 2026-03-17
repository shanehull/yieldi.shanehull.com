# Yieldi

Quantitative grain risk engine that estimates yield in real-time and calculates dynamic hedge ratios pre-harvest.

## What It Does

Yieldi estimates yield in real-time and calculates dynamic hedge ratios pre-harvest. Using satellite imagery (NDVI) and rainfall data, it:

- **Estimates yield** against a baseline by measuring current crop health and rainfall conditions
- **Calculates hedge ratios** that scale based on expected production
- **Increases confidence** as harvest approaches, becoming more precise for hedging decisions
- **Draw & Assess** any paddock on an interactive map to get instant yield and risk analysis

The he"ge decision drives the amount of production to lock in at current prices, whether through futures, forwards, or other instruments.

## Quick Start

```bash
air
```

Server runs on `http://localhost:8080`.

## Testing

```bash
go test -v ./...
```

## Architecture

```
cmd/server/
├── main.go                    HTTP server, graceful shutdown

internal/
├── quant/
│   ├── model.go              Yield estimation & hedge ratio calculation
│   └── model_test.go         Unit tests
├── satellite/
│   ├── service.go            Landsat NDVI fetch (STAC API)
│   ├── stac.go               STAC API client (no auth)
│   └── landsat.go            Landsat NDVI computation
├── weather/
│   ├── service.go            Rainfall from SILO API
│   ├── silo.go               Australian SILO client
│   └── service_test.go       Unit tests
├── cache/
│   └── cache.go              In-memory cache for API responses
├── config/
│   └── season.go             Regional parameters (baseline yield, harvest date)
├── handlers/
│   └── handlers.go           HTTP endpoints (/health, /api/assess)
└── ui/
    ├── dashboard.templ       Main template (Templ)
    └── components.templ      UI components

static/
└── index.html                Frontend (Leaflet map, Tailwind CSS)
```

## API

### POST /api/assess

Assess production risk for a polygon.

**Request:**

```json
{
  "geometry": {
    "type": "Polygon",
    "coordinates": [[[lon, lat], [lon, lat], ...]]
  }
}
```

**Response:**

```json
{
  "yield_estimate": 2.4,
  "yield_baseline": 2.5,
  "yield_delta_percent": -5.6,
  "hedge_ratio": 0.567,
  "ndvi_anomaly": 0.98,
  "rainfall_delta": 0.033,
  "cloud_cover": 0.08,
  "low_confidence": false,
  "confidence": 85.5,
  "days_to_harvest": 42
}
```

Units: t/ha (tonnes per hectare)

### GET /health

Health check.

## Model

**Yield Estimation (Theta-Yield):**

Time-weighted estimation that increases confidence as harvest approaches.

```
Yield_est = (α + (β1 * NDVI_Anomaly * W) + (β2 * Rainfall_Delta * W)) * Yield_baseline
```

Where:

- α = 0.2, β1 = 0.7, β2 = 0.1 (fixed coefficients)
- W = time-decay weight = (Days_to_Harvest / Season_Days)²
- NDVI_Anomaly = Current_NDVI / Historical_Mean_NDVI
- Rainfall_Delta = (Current_Rainfall - Historical_Mean) / Historical_Mean

**Hedge Ratio:**

```
Target_Hedge_Ratio = Base_Target * (Yield_est / Yield_baseline) * W
```

Clamped to [0, 1].

## Data Sources

- **Crop Health (NDVI):** Landsat via STAC API (free, no auth required)
  - Current: 60 days of recent imagery
  - Historical: 5 years of April-December growing season
- **Rainfall:** Australian SILO API (free, no auth required)
  - Current: April 1 to today
  - Historical: 5 years of same seasonal period

## Development

```bash
# Hot reload with air
air

# Lint
golangci-lint run ./...

# Test
go test -v ./...
```

## Container

```bash
docker run -p 8080:8080 ghcr.io/shanehull/yieldi.shanehull.com:latest
```

## License

Proprietary
