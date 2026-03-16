package weather

import (
	"testing"
)

// TestCalculateCentroid verifies centroid calculation for a Victoria polygon
func TestCalculateCentroid(t *testing.T) {
	// Victoria polygon: approx -37, 145
	// Coordinates in GeoJSON [lon, lat] format
	coords := [][][]float64{
		{
			{145.0, -37.0}, // lon, lat
			{145.5, -37.0},
			{145.5, -37.5},
			{145.0, -37.5},
			{145.0, -37.0}, // close the ring
		},
	}

	lat, lon, err := CalculateCentroid(coords)
	if err != nil {
		t.Fatalf("CalculateCentroid failed: %v", err)
	}

	t.Logf("Centroid: lat=%.4f, lon=%.4f", lat, lon)

	// Using simple average of all vertices (including closure point)
	// Points: [145.0, -37.0], [145.5, -37.0], [145.5, -37.5], [145.0, -37.5], [145.0, -37.0]
	// Avg lon: (145.0 + 145.5 + 145.5 + 145.0 + 145.0) / 5 = 145.2
	// Avg lat: (-37.0 + -37.0 + -37.5 + -37.5 + -37.0) / 5 = -37.2

	expectedLat := -37.2
	expectedLon := 145.2

	tolerance := 0.01

	if lat < expectedLat-tolerance || lat > expectedLat+tolerance {
		t.Errorf("latitude out of range: got %.4f, expected ~%.4f", lat, expectedLat)
	}
	if lon < expectedLon-tolerance || lon > expectedLon+tolerance {
		t.Errorf("longitude out of range: got %.4f, expected ~%.4f", lon, expectedLon)
	}
}

// TestCalculateCentroidDegenerate verifies degenerate polygon handling
func TestCalculateCentroidDegenerate(t *testing.T) {
	// Nearly collinear points (degenerate polygon)
	coords := [][][]float64{
		{
			{145.0, -37.0},
			{145.1, -37.0},
			{145.2, -37.0},
			{145.0, -37.0},
		},
	}

	lat, lon, err := CalculateCentroid(coords)
	if err != nil {
		t.Fatalf("CalculateCentroid failed: %v", err)
	}

	t.Logf("Degenerate centroid: lat=%.4f, lon=%.4f", lat, lon)

	// Average should be approximately (145.1, -37.0)
	expectedLat := -37.0
	expectedLon := 145.075

	tolerance := 0.05

	if lat < expectedLat-tolerance || lat > expectedLat+tolerance {
		t.Errorf("latitude out of range: got %.4f, expected ~%.4f", lat, expectedLat)
	}
	if lon < expectedLon-tolerance || lon > expectedLon+tolerance {
		t.Errorf("longitude out of range: got %.4f, expected ~%.4f", lon, expectedLon)
	}
}
