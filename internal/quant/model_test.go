package quant

import (
	"math"
	"testing"
)

// newTestModel returns a model with a fixed reference date at season start
// so days-to-harvest equals TotalSeasonDays, making W = 1 deterministically.
func newTestModel() *YieldModel {
	m := NewYieldModel()
	m.ReferenceDate = m.HarvestDate.AddDate(0, 0, -m.TotalSeasonDays)
	return m
}

func TestEstimateYield(t *testing.T) {
	model := newTestModel()

	tests := []struct {
		name           string
		yieldBaseline  float64
		ndviAnomaly    float64
		rainfallDelta  float64
		expectedResult float64
	}{
		{
			name:          "Baseline conditions",
			yieldBaseline: 2.5,
			ndviAnomaly:   0.0,
			rainfallDelta: 0.0,
			// (1 + 0.7*0.0 + 0.1*0.0) * 2.5 = 1.0 * 2.5 = 2.5
			expectedResult: 2.5,
		},
		{
			name:          "Positive NDVI anomaly",
			yieldBaseline: 2.5,
			ndviAnomaly:   0.1,
			rainfallDelta: 0.0,
			// (1 + 0.7*0.1 + 0.1*0.0) * 2.5 = 1.07 * 2.5 = 2.675
			expectedResult: 2.675,
		},
		{
			name:          "Negative rainfall delta",
			yieldBaseline: 2.5,
			ndviAnomaly:   0.0,
			rainfallDelta: -0.2,
			// (1 + 0.7*0.0 + 0.1*-0.2) * 2.5 = 0.98 * 2.5 = 2.45
			expectedResult: 2.45,
		},
		{
			name:          "Combined positive anomalies",
			yieldBaseline: 2.6,
			ndviAnomaly:   0.05,
			rainfallDelta: 0.15,
			// (1 + 0.7*0.05 + 0.1*0.15) * 2.6 = 1.05 * 2.6 = 2.73
			expectedResult: 2.73,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := model.EstimateYield(tt.yieldBaseline, tt.ndviAnomaly, tt.rainfallDelta)
			if !floatEqual(result, tt.expectedResult) {
				t.Errorf("expected %.2f, got %.2f", tt.expectedResult, result)
			}
		})
	}
}

func TestEstimateYieldAlphaFloor(t *testing.T) {
	model := newTestModel()

	// Severe negative anomalies should be floored at the Alpha yield floor
	result := model.EstimateYield(2.5, -1.0, -1.0)
	expected := 0.2 * 2.5 // alpha floor * baseline
	if !floatEqual(result, expected) {
		t.Errorf("expected yield floored at %.2f, got %.2f", expected, result)
	}
}

func TestCalculateHedgeRatio(t *testing.T) {
	model := newTestModel()

	tests := []struct {
		name           string
		yieldEstimate  float64
		yieldBaseline  float64
		expectedResult float64
	}{
		{
			name:          "Baseline yield",
			yieldEstimate: 2.5,
			yieldBaseline: 2.5,
			// 0.6 * (2.5 / 2.5) = 0.6
			expectedResult: 0.6,
		},
		{
			name:          "50% baseline yield",
			yieldEstimate: 1.25,
			yieldBaseline: 2.5,
			// 0.6 * (1.25 / 2.5) = 0.3
			expectedResult: 0.3,
		},
		{
			name:          "110% baseline yield",
			yieldEstimate: 2.75,
			yieldBaseline: 2.5,
			// 0.6 * (2.75 / 2.5) = 0.66
			expectedResult: 0.66,
		},
		{
			name:           "Zero baseline yield",
			yieldEstimate:  2.5,
			yieldBaseline:  0.0,
			expectedResult: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := model.CalculateHedgeRatio(tt.yieldEstimate, tt.yieldBaseline)
			if !floatEqual(result, tt.expectedResult) {
				t.Errorf("expected %.2f, got %.2f", tt.expectedResult, result)
			}
		})
	}
}

func TestAssessRisk(t *testing.T) {
	model := newTestModel()

	risk := model.AssessRisk(
		2.5,   // yieldBaseline
		0.6,   // historicalNDVIMean
		0.63,  // currentNDVI (5% higher)
		600.0, // meanRainfall
		600.0, // actualRainfall
		0.05,  // cloudCover (5%)
	)

	if risk == nil {
		t.Fatal("expected risk assessment, got nil")
	}

	if risk.LowConfidence {
		t.Errorf("expected low confidence false, got true")
	}

	// 5% above-average NDVI and neutral rainfall should give a small positive estimate
	expectedNDVIAnomaly := (0.63 / 0.6) - 1
	if math.Abs(risk.NDVIAnomaly-expectedNDVIAnomaly) > 1e-9 {
		t.Errorf("unexpected NDVI anomaly: %.4f", risk.NDVIAnomaly)
	}

	// Hedge ratio should be clamped to [0, 1]
	if risk.HedgeRatio < 0 || risk.HedgeRatio > 1 {
		t.Errorf("hedge ratio out of bounds: %.2f", risk.HedgeRatio)
	}
}

func TestAssessRiskHighCloudCover(t *testing.T) {
	model := newTestModel()

	risk := model.AssessRisk(
		2.5,   // yieldBaseline
		0.6,   // historicalNDVIMean
		0.63,  // currentNDVI
		600.0, // meanRainfall
		600.0, // actualRainfall
		0.25,  // cloudCover (25% > threshold)
	)

	if !risk.LowConfidence {
		t.Errorf("expected low confidence true, got false")
	}
}

func TestAssessRiskHedgeRatioClamping(t *testing.T) {
	model := newTestModel()

	// Very poor yield should clamp hedge ratio to 0
	risk := model.AssessRisk(
		2.5,   // yieldBaseline
		0.6,   // historicalNDVIMean
		0.1,   // currentNDVI (very low)
		600.0, // meanRainfall
		100.0, // actualRainfall (very dry)
		0.05,  // cloudCover
	)

	if risk.HedgeRatio < 0 {
		t.Errorf("hedge ratio should be clamped >= 0, got %.2f", risk.HedgeRatio)
	}

	// Excellent yield should clamp hedge ratio to 1
	risk = model.AssessRisk(
		2.5,   // yieldBaseline
		0.6,   // historicalNDVIMean
		0.8,   // currentNDVI (very high)
		600.0, // meanRainfall
		800.0, // actualRainfall (very wet)
		0.05,  // cloudCover
	)

	if risk.HedgeRatio > 1 {
		t.Errorf("hedge ratio should be clamped <= 1, got %.2f", risk.HedgeRatio)
	}
}

// Helper function to compare floats with tolerance
func floatEqual(a, b float64) bool {
	return math.Abs(a-b) < 0.01
}
