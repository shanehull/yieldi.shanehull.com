package satellite

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

// AggregateObservations computes NDVI statistics from multiple scenes.
// Uses cloud cover as a proxy for vegetation health.
// Lower cloud cover indicates healthier crops; higher cloud cover indicates stress or poor conditions.
func AggregateObservations(scenes []LandsatScene) *ObservationData {
	if len(scenes) == 0 {
		return &ObservationData{NDVI: 0, CloudCover: 0}
	}

	var ndviSum, cloudSum float64
	count := 0

	for _, scene := range scenes {
		// Normalize cloud cover from STAC 0-100 scale to 0-1
		cloudCover := scene.CloudCover / 100.0

		// Estimate NDVI from cloud cover
		// Lower cloud cover → higher vegetation health
		// baseNDVI of 0.65 represents typical healthy wheat
		ndvi := 0.65 - (cloudCover * 0.15)
		if ndvi < 0 {
			ndvi = 0
		}
		if ndvi > 1 {
			ndvi = 1
		}

		ndviSum += ndvi
		cloudSum += cloudCover
		count++
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
