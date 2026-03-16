# Yieldi

Grain risk & hedge decision support system. Correlates Sentinel-2 satellite NDVI against rainfall data to estimate production yield and calculate optimal hedge ratios.

## Quick Start

```bash
# Setup
cp .env.example .env
# Edit .env with Sentinel Hub credentials

# Build
go mod download
templ generate ./internal/ui
go build -o ./bin/yieldi ./cmd/api

# Run
SENTINEL_CLIENT_ID=xxx SENTINEL_CLIENT_SECRET=yyy ./bin/yieldi
```

Server runs on `http://localhost:8080`.

## Testing

```bash
go test -v ./...
```

## Architecture

```
cmd/api/
├── main.go                    HTTP server, graceful shutdown

internal/
├── quant/
│   ├── model.go              Yield estimation & hedge ratio calculation
│   └── model_test.go         Unit tests
├── sentinel/
│   ├── client.go             Sentinel Hub OAuth2 client
│   └── service.go            5-year parallel NDVI fetch
├── weather/
│   ├── silo.go               Australian SILO API client
│   └── service.go            Current & historical rainfall, polygon centroid
├── handlers/
│   └── handlers.go           HTTP endpoints (/health, /api/assess)
└── ui/
    ├── dashboard.templ       Main template
    └── components.templ      UI components

static/
└── index.html                Frontend (Leaflet, HTMX, Tailwind)
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
  "low_confidence": false
}
```
Units: t/ha (tonnes per hectare)

### GET /health

Health check.

## Model

**Yield Estimation:**
```
Yield_est = (α + (β1 * NDVI_Anomaly) + (β2 * Rainfall_Delta)) * Yield_baseline
```
- α = 0.2, β1 = 0.7, β2 = 0.1

**Hedge Ratio:**
```
Target_Hedge_Ratio = 0.60 * (Yield_est / Yield_baseline)
```
- Clamped to [0, 1]

## Data Sources

- **NDVI:** Sentinel Hub Statistical API (5-year historical + current 30 days)
- **Rainfall:** Australian SILO API (April 1 - current date, 5-year historical)

## Development

```bash
# Hot reload
brew install cosmtrek/tap/air
air

# Lint
golangci-lint run ./...
```

## Production

```bash
docker build -t yieldi:latest .
docker-compose up

# Or Kubernetes
kubectl apply -f deployment.yaml
```

## License

Proprietary
