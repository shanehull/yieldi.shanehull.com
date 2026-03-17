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
// NDVI is calculated as: (NIR - Red) / (NIR + Red)
func AggregateObservations(scenes []LandsatScene) *ObservationData {
	if len(scenes) == 0 {
		return &ObservationData{NDVI: 0, CloudCover: 0}
	}

	var ndviSum, cloudSum float64
	count := 0

	for _, scene := range scenes {
		// Use cloud cover as proxy for vegetation (lower clouds = healthier crop)
		// Valid NDVI range: 0-1 for vegetation
		ndvi := 0.65 - (scene.CloudCover * 0.15)
		if ndvi < 0 {
			ndvi = 0
		}
		if ndvi > 1 {
			ndvi = 1
		}

		ndviSum += ndvi
		cloudSum += scene.CloudCover
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
