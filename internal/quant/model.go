package quant

import (
	"time"

	"github.com/shanehull/yieldi/internal/config"
)

// Model coefficients - calibrated by quantitative team, not user-configurable
const (
	coefficientAlpha = 0.2 // Intercept: base yield multiplier
	coefficientBeta1 = 0.7 // NDVI weight: vegetation health sensitivity
	coefficientBeta2 = 0.1 // Rainfall weight: moisture sensitivity
)

// YieldModel holds the coefficients for the Theta-Yield estimation formula.
type YieldModel struct {
	Alpha           float64
	Beta1           float64
	Beta2           float64
	BaseHedgeTarget float64
	HarvestDate     time.Time
	TotalSeasonDays int
	BaselineYield   float64
	ReferenceDate   time.Time // For testing/debugging; nil uses time.Now()
}

// NewYieldModel returns the production model with default coefficients and Theta-Yield parameters.
func NewYieldModel() *YieldModel {
	return NewYieldModelWithConfig(config.DefaultConfig())
}

// NewYieldModelWithConfig creates a model using regional configuration.
// Model coefficients are fixed constants, only regional parameters vary.
func NewYieldModelWithConfig(cfg *config.SeasonConfig) *YieldModel {
	return &YieldModel{
		Alpha:           coefficientAlpha,
		Beta1:           coefficientBeta1,
		Beta2:           coefficientBeta2,
		BaseHedgeTarget: cfg.TargetHedgeRatio,
		HarvestDate:     cfg.GetHarvestDate(),
		TotalSeasonDays: cfg.SeasonDays,
		BaselineYield:   cfg.BaselineYield,
	}
}

// calculateDaysToHarvest returns the number of days remaining until harvest.
// Uses ReferenceDate if set, otherwise defaults to today.
// If current date is past harvest date, returns 0.
func (m *YieldModel) calculateDaysToHarvest() int {
	now := m.ReferenceDate
	if now.IsZero() {
		now = time.Now().UTC()
	}
	d := int(m.HarvestDate.Sub(now).Hours() / 24)
	if d < 0 {
		d = 0
	}
	return d
}

// calculateTimeWeight computes W: the time-decay weight based on days to harvest.
// W = (D / TotalSeasonDays)^2, which gives 0 at season start and 1.0 at harvest.
// This represents confidence increasing as we approach harvest.
func (m *YieldModel) calculateTimeWeight(daysToHarvest int) float64 {
	d := float64(daysToHarvest)
	// Clamp D to [0, TotalSeasonDays] range
	if d > float64(m.TotalSeasonDays) {
		d = float64(m.TotalSeasonDays)
	}
	normalized := d / float64(m.TotalSeasonDays)
	// W increases from 0 (season start) to 1.0 (harvest)
	return normalized * normalized
}

// EstimateYield calculates yield based on NDVI and rainfall anomalies with time-decay weighting.
// Formula: Yield_est = (α + (β1 * NDVI_Anomaly * W) + (β2 * Rainfall_Delta * W)) * Baseline_Yield
// Returns the estimated yield.
func (m *YieldModel) EstimateYield(yieldBaseline, ndviAnomaly, rainfallDelta float64) float64 {
	daysToHarvest := m.calculateDaysToHarvest()
	w := m.calculateTimeWeight(daysToHarvest)
	
	multiplier := m.Alpha + (m.Beta1 * ndviAnomaly * w) + (m.Beta2 * rainfallDelta * w)
	return multiplier * yieldBaseline
}

// CalculateHedgeRatio computes the target hedge ratio based on yield estimate with time weighting.
// Target_Hedge_Ratio = Base_Hedge_Target * (Yield_est / Yield_baseline) * W
func (m *YieldModel) CalculateHedgeRatio(yieldEstimate, yieldBaseline float64) float64 {
	if yieldBaseline == 0 {
		return 0
	}
	daysToHarvest := m.calculateDaysToHarvest()
	w := m.calculateTimeWeight(daysToHarvest)
	return m.BaseHedgeTarget * (yieldEstimate / yieldBaseline) * w
}

// ProductionRisk calculates the risk metrics for a hectare-based contract.
type ProductionRisk struct {
	YieldEstimate     float64
	YieldBaseline     float64
	YieldDeltaPercent float64
	HedgeRatio        float64
	TargetHedgeRatio  float64 // Hedge ratio derived from Theta-Yield model
	NDVIAnomaly       float64
	RainfallDelta     float64
	CloudCover        float64
	LowConfidence     bool
	Confidence        float64 // Model confidence (W * 100)
	DaysToHarvest     int     // Days remaining until harvest
}

// AssessRisk evaluates production risk for a given hectare using the Theta-Yield model.
func (m *YieldModel) AssessRisk(yieldBaseline, historicalNDVIMean, currentNDVI, meanRainfall, actualRainfall, cloudCover float64) *ProductionRisk {
	// Calculate anomalies
	ndviAnomaly := 0.0
	if historicalNDVIMean > 0 {
		ndviAnomaly = currentNDVI / historicalNDVIMean
	}

	rainfallDelta := 0.0
	if meanRainfall > 0 {
		rainfallDelta = (actualRainfall - meanRainfall) / meanRainfall
	}

	// Estimate yield with time-decay weighting
	yieldEstimate := m.EstimateYield(yieldBaseline, ndviAnomaly, rainfallDelta)

	// Calculate hedge ratio with time weighting
	targetHedgeRatio := m.CalculateHedgeRatio(yieldEstimate, yieldBaseline)

	// Clamp hedge ratio to [0, 1]
	if targetHedgeRatio < 0 {
		targetHedgeRatio = 0
	}
	if targetHedgeRatio > 1 {
		targetHedgeRatio = 1
	}

	// Calculate confidence and days to harvest
	daysToHarvest := m.calculateDaysToHarvest()
	w := m.calculateTimeWeight(daysToHarvest)
	confidence := w * 100

	yieldDeltaPercent := 0.0
	if yieldBaseline > 0 {
		yieldDeltaPercent = ((yieldEstimate - yieldBaseline) / yieldBaseline) * 100
	}

	return &ProductionRisk{
		YieldEstimate:     yieldEstimate,
		YieldBaseline:     yieldBaseline,
		YieldDeltaPercent: yieldDeltaPercent,
		HedgeRatio:        targetHedgeRatio,
		TargetHedgeRatio:  targetHedgeRatio,
		NDVIAnomaly:       ndviAnomaly,
		RainfallDelta:     rainfallDelta,
		CloudCover:        cloudCover,
		LowConfidence:     cloudCover > 0.20,
		Confidence:        confidence,
		DaysToHarvest:     daysToHarvest,
	}
}
