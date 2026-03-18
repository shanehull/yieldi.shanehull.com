let map,
  polygon,
  filledPoly,
  pointMarkers = [],
  drawingMode = true,
  coordinates = [],
  isDraggingPoint = false,
  draggingPointIndex = -1,
  fieldSizeHa = 0,
  currentAbortController = null;

// Update URL with current state for sharing
function updateURL() {
  const params = new URLSearchParams();

  if (coordinates.length > 0) {
    // Encode coordinates as lat,lng;lat,lng...
    const coordStr = coordinates
      .map((c) => `${c[0].toFixed(6)},${c[1].toFixed(6)}`)
      .join(";");
    params.set("c", coordStr);
  }

  const assessmentDate = document.getElementById("assessmentDate").value;
  const harvestDate = document.getElementById("harvestDate").value;
  const baselineYield = document.getElementById("baselineYield").value;
  const targetHedge = document.getElementById("targetHedge").value;
  const preset = document.getElementById("presetSelect").value;
  const alpha = document.getElementById("alpha").value;
  const beta1 = document.getElementById("beta1").value;
  const beta2 = document.getElementById("beta2").value;

  if (assessmentDate) params.set("ad", assessmentDate);
  if (harvestDate) params.set("hd", harvestDate);
  if (baselineYield) params.set("by", baselineYield);
  if (targetHedge) params.set("th", targetHedge);
  if (preset) params.set("p", preset);
  if (alpha) params.set("a", alpha);
  if (beta1) params.set("b1", beta1);
  if (beta2) params.set("b2", beta2);

  const newURL = `${window.location.pathname}?${params.toString()}`;
  window.history.replaceState({}, "", newURL);
}

// Parse URL params and initialize state
function parseURLParams() {
  const params = new URLSearchParams(window.location.search);
  let hasParams = false;

  // Set inputs first so updateURL (triggered by updatePreview) has the correct data
  if (params.has("ad")) {
    document.getElementById("assessmentDate").value = params.get("ad");
    hasParams = true;
  }
  if (params.has("hd")) {
    document.getElementById("harvestDate").value = params.get("hd");
    hasParams = true;
  }
  if (params.has("by")) {
    document.getElementById("baselineYield").value = params.get("by");
    hasParams = true;
  }
  if (params.has("th")) {
    document.getElementById("targetHedge").value = params.get("th");
    hasParams = true;
  }
  if (params.has("p")) {
    document.getElementById("presetSelect").value = params.get("p");
    hasParams = true;
  }
  if (params.has("a")) {
    document.getElementById("alpha").value = params.get("a");
    hasParams = true;
  }
  if (params.has("b1")) {
    document.getElementById("beta1").value = params.get("b1");
    hasParams = true;
  }
  if (params.has("b2")) {
    document.getElementById("beta2").value = params.get("b2");
    hasParams = true;
  }

  // Handle coordinates last as they trigger updatePreview/updateURL
  const coordStr = params.get("c");
  if (coordStr) {
    hasParams = true;
    const pairs = coordStr.split(";");
    coordinates = pairs.map((p) => {
      const [lat, lng] = p.split(",").map(Number);
      return [lat, lng];
    });

    // If we have coordinates, initialize markers
    if (coordinates.length > 0) {
      coordinates.forEach((latlng, i) => {
        addPointMarker({ lat: latlng[0], lng: latlng[1] }, i + 1);
      });
      updatePreview();

      // Center map on polygon
      if (coordinates.length >= 3) {
        const bounds = L.latLngBounds(coordinates);
        map.fitBounds(bounds, { padding: [50, 50] });
      }
    }
  }

  return hasParams;
}

// Calculate polygon area in hectares using Haversine formula
function calculatePolygonAreaHa(coords) {
  if (coords.length < 3) return 0;

  const R = 6371000; // Earth radius in meters
  let area = 0;

  for (let i = 0; i < coords.length; i++) {
    const p1 = coords[i];
    const p2 = coords[(i + 1) % coords.length];

    const lat1 = (p1[0] * Math.PI) / 180;
    const lat2 = (p2[0] * Math.PI) / 180;
    const dlon = ((p2[1] - p1[1]) * Math.PI) / 180;

    area += Math.sin(lat1) * Math.cos(lat2) * Math.sin(dlon);
  }

  area = Math.abs((area * R * R) / 2);
  return area / 10000; // Convert m² to hectares
}

// Set assessment and harvest dates on page load if they aren't already set
function setDefaultDates() {
  const assessElem = document.getElementById("assessmentDate");
  const harvestElem = document.getElementById("harvestDate");

  if (!assessElem.value) {
    const today = new Date();
    assessElem.valueAsDate = new Date(
      today.getFullYear(),
      today.getMonth(),
      today.getDate(),
    );
  }

  if (!harvestElem.value) {
    const year = new Date().getFullYear();
    harvestElem.value = year + "-11-15";
  }
}

// Model coefficient presets (real-world scenarios)
const presets = {
  default: {
    name: "Mallee Wheat (Low Rain)",
    alpha: 0.12, // Very low floor (total crop failure possible in dry years)
    beta1: 0.6, // Vigor moderate (water-limited, less responsive)
    beta2: 0.4, // Rainfall DOMINANT (make-or-break factor)
  },
  wa: {
    name: "WA Wheat (Semi-Arid)",
    alpha: 0.15, // Low floor (rainfed, variable conditions)
    beta1: 0.62, // Vigor matters but constrained by water
    beta2: 0.38, // High rain sensitivity (variable rainfall)
  },
  irrigated: {
    name: "Murray Basin (Irrigated)",
    alpha: 0.28, // Stable floor (irrigation provides buffer)
    beta1: 0.7, // Vigor more predictive in controlled systems
    beta2: 0.08, // Rain barely matters (irrigation managed)
  },
  high_rainfall: {
    name: "High Rainfall Zone (South)",
    alpha: 0.25, // Reasonable floor (more reliable rainfall)
    beta1: 0.72, // Vigor important but water less limiting
    beta2: 0.15, // Rainfall matters less (adequate moisture)
  },
};

function addPointMarker(latlng, index) {
  const marker = L.circleMarker(latlng, {
    radius: 10,
    fillColor: "#22c55e",
    color: "#16a34a",
    weight: 3,
    opacity: 1,
    fillOpacity: 0.8,
    interactive: true,
  }).addTo(map);

  marker.on("mousedown", function (e) {
    isDraggingPoint = true;
    draggingPointIndex = index - 1;
    map.dragging.disable();
    L.DomEvent.stop(e);
  });

  marker.on("click", function (e) {
    L.DomEvent.stop(e);
  });

  marker.on("dblclick", function (e) {
    L.DomEvent.stop(e);
  });

  marker.on("mouseover", function () {
    this.setStyle({ fillColor: "#16a34a", radius: 12 });
  });

  marker.on("mouseout", function () {
    this.setStyle({ fillColor: "#22c55e", radius: 10 });
  });

  const tooltip = L.tooltip({
    permanent: true,
    direction: "center",
    className: "point-label",
  })
    .setContent(String(index))
    .setLatLng(latlng);
  marker.bindTooltip(tooltip);

  marker.bindPopup(
    `Point ${index}<br/>${latlng.lat.toFixed(4)}, ${latlng.lng.toFixed(4)}`,
    { closeButton: false },
  );
  pointMarkers.push(marker);
}

function updatePointCounter() {
  // Show/hide undo button based on whether points exist
  document.getElementById("undoBtn").style.display =
    coordinates.length > 0 ? "block" : "none";
}

function updatePreview() {
  if (polygon) {
    map.removeLayer(polygon);
    polygon = null;
  }
  if (filledPoly) {
    map.removeLayer(filledPoly);
    filledPoly = null;
  }

  if (coordinates.length >= 3) {
    const closed = [...coordinates, coordinates[0]];

    filledPoly = L.polygon(closed, {
      color: "#22c55e",
      fillColor: "#86efac",
      fillOpacity: 0.15,
      weight: 2,
    }).addTo(map);

    polygon = L.polyline(closed, {
      color: "#22c55e",
      weight: 2,
      opacity: 0.7,
    }).addTo(map);

    // Auto-calculate field size from polygon
    fieldSizeHa = calculatePolygonAreaHa(coordinates);

    // Show area indicator
    document.getElementById("areaValue").textContent = fieldSizeHa.toFixed(1);
    document.getElementById("areaIndicator").style.display = "block";

    document.getElementById("assessBtn").disabled = false;
  } else {
    document.getElementById("assessBtn").disabled = true;
  }
  updatePointCounter();
  updateURL();
}

function cancelDrawing() {
  drawingMode = true;
  coordinates = [];
  fieldSizeHa = 0;
  pointMarkers.forEach((m) => map.removeLayer(m));
  pointMarkers = [];
  if (polygon) map.removeLayer(polygon);
  if (filledPoly) map.removeLayer(filledPoly);
  document.getElementById("assessBtn").disabled = true;
  document.getElementById("map").classList.add("drawing");
  document.getElementById("areaIndicator").style.display = "none";
  const status = document.getElementById("drawStatus");
  if (status) status.textContent = "";
  updateURL();
}

function finishDrawing() {
  if (coordinates.length < 3) return;
  drawingMode = false;
  document.getElementById("drawBtn").textContent = "Draw Polygon";
  document.getElementById("drawBtn").className = "btn-green";
  document.getElementById("map").classList.remove("drawing");
  const status = document.getElementById("drawStatus");
  if (status) status.textContent = "";
  pointMarkers.forEach((m) => map.removeLayer(m));
  pointMarkers = [];
  document.getElementById("undoBtn").style.display = "none";
  updateURL();
}

function setupEventListeners() {
  // Preset selector
  document
    .getElementById("presetSelect")
    .addEventListener("change", function () {
      const preset = presets[this.value];
      if (preset) {
        document.getElementById("alpha").value = preset.alpha.toFixed(2);
        document.getElementById("beta1").value = preset.beta1.toFixed(2);
        document.getElementById("beta2").value = preset.beta2.toFixed(2);
      }
      updateURL();
    });

  document.getElementById("resetBtn").addEventListener("click", function () {
    setDefaultDates();
    document.getElementById("baselineYield").value = "2.5";
    document.getElementById("targetHedge").value = "0.60";
    document.getElementById("presetSelect").value = "default";
    document.getElementById("alpha").value = "0.12";
    document.getElementById("beta1").value = "0.60";
    document.getElementById("beta2").value = "0.40";
    updateURL();
  });

  document.getElementById("clearBtn").addEventListener("click", cancelDrawing);

  document.getElementById("undoBtn").addEventListener("click", function () {
    if (coordinates.length === 0) return;

    coordinates.pop();

    if (pointMarkers.length > 0) {
      const lastMarker = pointMarkers.pop();
      map.removeLayer(lastMarker);
    }

    updatePreview();
    updatePointCounter();
    updateURL();

    if (coordinates.length === 0) {
      document.getElementById("undoBtn").style.display = "none";
    }
  });

  // Input changes
  [
    "assessmentDate",
    "harvestDate",
    "baselineYield",
    "targetHedge",
    "alpha",
    "beta1",
    "beta2",
  ].forEach((id) => {
    document.getElementById(id).addEventListener("change", updateURL);
  });

  document.getElementById("assessBtn").addEventListener("click", function () {
    const btn = this;

    if (currentAbortController) {
      currentAbortController.abort();
      return;
    }

    if (coordinates.length < 3) {
      alert("Need at least 3 points");
      return;
    }

    btn.textContent = "Fetching... [X]";
    btn.classList.remove("btn-green-sm");
    btn.classList.add("btn-slate-sm");

    const geojson = {
      type: "Polygon",
      coordinates: [coordinates.map((c) => [c[1], c[0]])],
    };

    // Calculate season days from assessment date to harvest date
    let seasonDays = 198; // default
    const assessmentDateVal = document.getElementById("assessmentDate").value;
    const harvestDateVal = document.getElementById("harvestDate").value;

    if (assessmentDateVal && harvestDateVal) {
      const assessDate = new Date(assessmentDateVal);
      const harvestDate = new Date(harvestDateVal);
      seasonDays = Math.max(
        1,
        Math.floor((harvestDate - assessDate) / (1000 * 60 * 60 * 24)),
      );
    }

    // Build request payload with model parameters from UI
    const payload = {
      geometry: JSON.stringify(geojson),
      field_size_ha: fieldSizeHa,
      baseline_yield: parseFloat(
        document.getElementById("baselineYield").value,
      ),
      target_hedge_ratio: parseFloat(
        document.getElementById("targetHedge").value,
      ),
      harvest_date: harvestDateVal,
      season_days: seasonDays,
      alpha: parseFloat(document.getElementById("alpha").value),
      beta1: parseFloat(document.getElementById("beta1").value),
      beta2: parseFloat(document.getElementById("beta2").value),
    };

    // Add assessment date if specified
    if (assessmentDateVal) {
      payload.assessment_date = new Date(assessmentDateVal).toISOString();
    }

    currentAbortController = new AbortController();

    fetch("/api/assess", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
      signal: currentAbortController.signal,
    })
      .then((r) => r.json())
      .then((data) => {
        if (data.error) {
          document.getElementById("riskCard").innerHTML =
            `<h3>Error</h3><div class="error">${data.error}</div>`;
          return;
        }
        showResults(data);
      })
      .catch((err) => {
        if (err.name === "AbortError") return;
        alert("Error: " + err);
      })
      .finally(() => {
        currentAbortController = null;
        btn.textContent = "Assess";
        btn.classList.remove("btn-slate-sm");
        btn.classList.add("btn-green-sm");
        btn.disabled = coordinates.length < 3;
      });
  });
}

function initMap() {
  map = L.map("map").setView([-37.7, 145.0], 8);

  L.esri.basemapLayer("Imagery", { maxZoom: 18 }).addTo(map);

  // Set drawing mode active by default
  document.getElementById("map").classList.add("drawing");
  updatePointCounter();

  // Parse URL parameters to initialize state
  parseURLParams();

  // Set default dates for anything not provided in URL
  setDefaultDates();

  L.Control.geocoder().addTo(map);

  setTimeout(() => {
    const resultsDiv = document.querySelector(
      ".leaflet-control-geocoder-results",
    );
    if (resultsDiv) resultsDiv.style.display = "none";
  }, 500);

  // Global drag handlers
  document.addEventListener("mousemove", function (e) {
    if (
      isDraggingPoint &&
      draggingPointIndex >= 0 &&
      pointMarkers[draggingPointIndex]
    ) {
      const marker = pointMarkers[draggingPointIndex];
      const latLng = map.mouseEventToLatLng(e);
      marker.setLatLng(latLng);
      coordinates[draggingPointIndex] = [latLng.lat, latLng.lng];

      // Faster preview update for dragging (don't update URL/size every frame)
      if (polygon) {
        const closed = [...coordinates, coordinates[0]];
        polygon.setLatLngs(closed);
        if (filledPoly) filledPoly.setLatLngs(closed);
      }
    }
  });

  document.addEventListener("mouseup", function () {
    if (isDraggingPoint) {
      isDraggingPoint = false;
      draggingPointIndex = -1;
      map.dragging.enable();

      // Final update after drag finishes
      updatePreview();
      updateURL();
    }
  });

  map.on("click", (e) => {
    if (drawingMode && !isDraggingPoint) {
      // Check if we are too close to any existing point (20px threshold)
      const clickPoint = map.latLngToLayerPoint(e.latlng);
      const isNearExisting = coordinates.some((coord) => {
        const p = map.latLngToLayerPoint(coord);
        return clickPoint.distanceTo(p) < 12;
      });

      if (!isNearExisting) {
        coordinates.push([e.latlng.lat, e.latlng.lng]);
        addPointMarker(e.latlng, coordinates.length);
        updatePreview();
      }
    }
  });

  // Aggressively hide geocoder results using MutationObserver
  const geocoderContainer = document.querySelector(".leaflet-control-geocoder");
  if (geocoderContainer) {
    const observer = new MutationObserver(() => {
      const resultsDiv = geocoderContainer.querySelector(
        ".leaflet-control-geocoder-results",
      );
      if (resultsDiv) resultsDiv.style.display = "none !important";
    });
    observer.observe(geocoderContainer, { childList: true, subtree: true });
  }

  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && drawingMode) cancelDrawing();
    if (e.key === "Enter" && drawingMode && coordinates.length >= 3)
      finishDrawing();
  });

  setupEventListeners();
}

function showResults(data) {
  const confidence = data.confidence.toFixed(0);
  const daysToHarvest = data.days_to_harvest;

  // Calculate farmer-friendly metrics
  const yieldDifference = data.yield_estimate - data.yield_baseline;
  const shortfallPercent =
    yieldDifference < 0
      ? (Math.abs(yieldDifference) / data.yield_baseline) * 100
      : 0;
  const protectionVolume = (
    data.yield_estimate * data.target_hedge_ratio
  ).toFixed(2);

  // Determine season outlook
  let outlook = "Average";
  let outlookColor = "#cbd5e1";
  if (data.yield_delta_percent > 10) {
    outlook = "Above Expected";
    outlookColor = "#86efac";
  } else if (data.yield_delta_percent < -10) {
    outlook = "Below Expected";
    outlookColor = "#fca5a5";
  }

  document.getElementById("riskCard").innerHTML = `
		<h3>Production Risk</h3>
		${data.low_confidence ? '<div style="color: #fbbf24; font-size: 0.75rem; margin-bottom: 0.5rem;">⚠ Low Confidence</div>' : ""}
		<div class="stat">
			<span class="stat-label">Yield Estimate</span>
			<span class="stat-value" style="color: #86efac;">${data.yield_estimate.toFixed(2)} t/ha</span>
		</div>
		<div class="stat">
			<span class="stat-label">vs Baseline</span>
			<span class="stat-value" style="color: ${yieldDifference >= 0 ? "#86efac" : "#fca5a5"};">${yieldDifference >= 0 ? "+" : ""}${yieldDifference.toFixed(2)} t/ha</span>
		</div>
		<div class="stat" style="border: none;">
			<span class="stat-label">Season Outlook</span>
			<span class="stat-value" style="color: ${outlookColor};">${outlook}</span>
		</div>
	`;

  const tHaToProtect = (data.total_hedge_volume / fieldSizeHa).toFixed(2);

  document.getElementById("hedgeCard").innerHTML = `
		<h3>Protection Strategy</h3>
		<div style="text-align: center; margin: 1rem 0;">
			<div style="font-size: 2.5rem; font-weight: bold; color: #86efac;">${data.total_hedge_volume.toFixed(1)}</div>
			<p class="status">tonnes to hedge</p>
		</div>
		<div style="font-size: 0.875rem; color: #cbd5e1; line-height: 1.8; margin-bottom: 1rem;">
			<p style="display: flex; justify-content: space-between; margin-bottom: 0.5rem;">
				<span>Protect (t/ha)</span>
				<span style="font-family: monospace;">${tHaToProtect}</span>
			</p>
			<p style="display: flex; justify-content: space-between; margin-bottom: 0.5rem;">
				<span>Total Estimated Yield</span>
				<span style="font-family: monospace;">${data.total_yield_estimate.toFixed(1)} t</span>
			</p>
			<p style="display: flex; justify-content: space-between; margin-bottom: 0.5rem;">
				<span>Coverage Ratio</span>
				<span style="font-family: monospace;">${(data.target_hedge_ratio * 100).toFixed(0)}% of yield</span>
			</p>
			<p style="display: flex; justify-content: space-between;">
				<span>Days to Harvest</span>
				<span style="font-family: monospace;">T-${daysToHarvest}</span>
			</p>
		</div>
		<div style="font-size: 0.875rem; color: #94a3b8; line-height: 1.6; border-top: 1px solid #475569; padding-top: 0.75rem;">
			<p style="margin-bottom: 0.5rem;"><strong>Model Confidence:</strong> ${confidence}%</p>
			<p style="margin: 0;">${confidence < 30 ? "⚠ Early season - use with caution" : confidence < 60 ? "◐ Moderate - refine closer to harvest" : "✓ High - strong signal"}</p>
		</div>
	`;

  document.getElementById("qualityCard").innerHTML = `
		<h3>Data Quality</h3>
		<div class="stat">
			<span class="stat-label">Cloud Cover</span>
			<span class="stat-value">${(data.cloud_cover * 100).toFixed(1)}%</span>
		</div>
		<div style="width: 100%; height: 8px; background: #334155; border-radius: 4px; margin: 0.5rem 0; overflow: hidden;">
			<div style="width: ${data.cloud_cover * 100}%; height: 100%; background: ${data.cloud_cover <= 0.2 ? "#86efac" : data.cloud_cover <= 0.5 ? "#fbbf24" : "#fca5a5"};"></div>
		</div>
		<div class="stat">
			<span class="stat-label">NDVI Anomaly</span>
			<span class="stat-value">${data.ndvi_anomaly.toFixed(3)}</span>
		</div>
		<p style="font-size: 0.75rem; color: #94a3b8; margin-top: 0.5rem;">
			${data.ndvi_anomaly > 1.1 ? "✓ Healthy" : data.ndvi_anomaly < 0.9 ? "✗ Stressed" : "~ Average"}
		</p>
	`;
}

document.addEventListener("DOMContentLoaded", initMap);
