package quant

import (
	"math"
	"testing"
)

func TestEstimateYield(t *testing.T) {
	model := NewYieldModel()

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
			ndviAnomaly:   1.0,
			rainfallDelta: 0.0,
			// (0.2 + 0.7*1.0 + 0.1*0.0) * 2.5 = 0.9 * 2.5 = 2.25
			expectedResult: 2.25,
		},
		{
			name:          "Positive NDVI anomaly",
			yieldBaseline: 2.5,
			ndviAnomaly:   1.1,
			rainfallDelta: 0.0,
			// (0.2 + 0.7*1.1 + 0.1*0.0) * 2.5 = 0.97 * 2.5 = 2.425
			expectedResult: 2.425,
		},
		{
			name:          "Negative rainfall delta",
			yieldBaseline: 2.5,
			ndviAnomaly:   1.0,
			rainfallDelta: -0.2,
			// (0.2 + 0.7*1.0 + 0.1*-0.2) * 2.5 = 0.88 * 2.5 = 2.2
			expectedResult: 2.2,
		},
		{
			name:          "Combined positive anomalies",
			yieldBaseline: 2.6,
			ndviAnomaly:   1.05,
			rainfallDelta: 0.15,
			// (0.2 + 0.7*1.05 + 0.1*0.15) * 2.6 = 0.95 * 2.6 = 2.47
			expectedResult: 2.47,
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

func TestCalculateHedgeRatio(t *testing.T) {
	model := NewYieldModel()

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
	model := NewYieldModel()

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

	// Hedge ratio should be clamped to [0, 1]
	if risk.HedgeRatio < 0 || risk.HedgeRatio > 1 {
		t.Errorf("hedge ratio out of bounds: %.2f", risk.HedgeRatio)
	}
}

func TestAssessRiskHighCloudCover(t *testing.T) {
	model := NewYieldModel()

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
	model := NewYieldModel()

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
