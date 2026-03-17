package config

import (
	"time"
)

// SeasonConfig holds regional parameters for the yield model.
// Model coefficients (alpha, beta1, beta2) are fixed by the quant team, not configurable by users.
type SeasonConfig struct {
	// Season timing (regional variation)
	HarvestMonth     int     // 1-12
	HarvestDay       int     // 1-31
	SeasonDays       int     // Total days in growing season
	BaselineYield    float64 // t/ha (tonnes per hectare)
	TargetHedgeRatio float64 // Default hedge ratio (0-1, e.g., 0.60 = 60%)
}

// DefaultConfig returns the default configuration for Australian wheat.
func DefaultConfig() *SeasonConfig {
	return &SeasonConfig{
		HarvestMonth:     11, // November
		HarvestDay:       15,
		SeasonDays:       198,
		BaselineYield:    2.5,
		TargetHedgeRatio: 0.60,
	}
}

// GetHarvestDate returns the harvest date for the current or next cycle relative to now.
func (c *SeasonConfig) GetHarvestDate() time.Time {
	return c.GetHarvestDateRelativeTo(time.Now().UTC())
}

// GetHarvestDateRelativeTo returns the harvest date for the current or next cycle relative to a given date.
func (c *SeasonConfig) GetHarvestDateRelativeTo(referenceDate time.Time) time.Time {
	harvestDate := time.Date(referenceDate.Year(), time.Month(c.HarvestMonth), c.HarvestDay, 0, 0, 0, 0, time.UTC)
	if referenceDate.After(harvestDate) {
		harvestDate = time.Date(referenceDate.Year()+1, time.Month(c.HarvestMonth), c.HarvestDay, 0, 0, 0, 0, time.UTC)
	}
	return harvestDate
}
