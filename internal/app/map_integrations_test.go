package app_test

import (
	"github.com/kproios/dz-manager/internal/mapevents"
	"testing"
	"time"
)

func TestLiveGroundAndLocalMarkerMetadata(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC()
	data := map[string]any{"key": "ground-1", "category": "tracked_item", "class": "PunchedCard", "name": "PunchedCard", "kind": "item"}
	e.send(ev("ground-start", "event.start", now, "", []float64{200, 0, 300}, data))
	e.send(ev("flag-start", "event.start", now, "", []float64{500, 0, 700}, map[string]any{"key": "flag-1", "category": "base_flag", "class": "TerritoryFlag", "name": "Survivors", "group": "SUR", "radius": 60}))
	var markers []mapevents.Marker
	if code := e.JSON("GET", "/api/map/events", nil, &markers); code != 200 || len(markers) != 2 {
		t.Fatalf("markers: %d %+v", code, markers)
	}
	if markers[0].Group != "SUR" || markers[0].Radius != 60 || markers[1].Name != "PunchedCard" || markers[1].Kind != "item" {
		t.Fatalf("metadata: %+v", markers)
	}
	data["name"] = "Updated card"
	e.send(ev("ground-refresh", "event.seen", now.Add(time.Second), "", []float64{210, 0, 310}, data))
	e.JSON("GET", "/api/map/events", nil, &markers)
	if markers[1].Name != "Updated card" {
		t.Fatal("refresh lost metadata")
	}
	e.send(ev("pickup", "event.end", now.Add(2*time.Second), "", nil, map[string]any{"key": "ground-1"}))
	e.JSON("GET", "/api/map/events", nil, &markers)
	if markers[1].Status != "ended" {
		t.Fatal("picked-up item still active")
	}
}

func TestTrackingSaveRequestsNewCensus(t *testing.T) {
	e := setup(t)
	cfg := mapevents.Tracking{RareItems: []string{"PunchedCard"}}
	var first, second mapevents.Tracking
	if code := e.JSON("PUT", "/api/settings/tracking", cfg, &first); code != 200 {
		t.Fatal(code)
	}
	e.JSON("PUT", "/api/settings/tracking", cfg, &second)
	if first.Revision < 1 || second.Revision <= first.Revision {
		t.Fatalf("unchanged save did not request census: %d %d", first.Revision, second.Revision)
	}
}
