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

func TestSightingAfterEndRevivesMarkerAndActiveListedFirst(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC()
	heli := map[string]any{"key": "boot1:heli", "category": "heli_crash", "class": "Wreck_UH1Y"}
	e.send(ev("h-start", "event.start", now.Add(-5*time.Minute), "", []float64{7000, 0, 8000}, heli))
	e.send(ev("h-end", "event.end", now.Add(-4*time.Minute), "", nil, map[string]any{"key": "boot1:heli"}))
	// A stale replay from before the end must not bring it back.
	e.send(ev("h-old", "event.seen", now.Add(-4*time.Minute-30*time.Second), "", []float64{7000, 0, 8000}, heli))
	var markers []mapevents.Marker
	e.JSON("GET", "/api/map/events", nil, &markers)
	if len(markers) != 1 || markers[0].Status != "ended" {
		t.Fatalf("after stale replay: %+v", markers)
	}
	// Newer history must not push the live marker down the list.
	e.send(ev("i-start", "event.start", now.Add(-3*time.Minute), "", []float64{100, 0, 100}, map[string]any{"key": "boot1:item", "category": "tracked_item", "class": "PunchedCard"}))
	e.send(ev("i-end", "event.end", now.Add(-2*time.Minute), "", nil, map[string]any{"key": "boot1:item"}))
	e.send(ev("h-seen", "event.seen", now.Add(-time.Minute), "", []float64{7001, 0, 8001}, heli))
	markers = nil // json.Unmarshal would keep fields omitted in the new response
	e.JSON("GET", "/api/map/events", nil, &markers)
	if len(markers) != 2 || markers[0].Key != "boot1:heli" || markers[0].Status != "active" || markers[0].EndedAt != nil {
		t.Fatalf("revived: %+v", markers)
	}
}

func TestPrefsArePerUserAndDurable(t *testing.T) {
	e := setup(t)
	var out map[string]any
	if code := e.JSON("GET", "/api/prefs", nil, &out); code != 200 || len(out) != 0 {
		t.Fatalf("empty prefs: %d %v", code, out)
	}
	if code := e.JSON("PUT", "/api/prefs", map[string]any{"map": map[string]any{"hidden": []string{"heli"}}}, nil); code != 200 {
		t.Fatalf("save: %d", code)
	}
	e.JSON("GET", "/api/prefs", nil, &out)
	if m, _ := out["map"].(map[string]any); m == nil || len(m["hidden"].([]any)) != 1 {
		t.Fatalf("round trip: %v", out)
	}
	if code := e.JSON("PUT", "/api/prefs", []int{1}, nil); code != 400 {
		t.Fatalf("non-object accepted: %d", code)
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
