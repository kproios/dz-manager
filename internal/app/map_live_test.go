package app_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kproios/dz-manager/internal/mapevents"
	"github.com/kproios/dz-manager/internal/metricz"
)

func activeKeys(e *env) map[string]bool {
	var ms []mapevents.Marker
	e.JSON("GET", "/api/map/events", nil, &ms)
	out := map[string]bool{}
	for _, m := range ms {
		if m.Status == "active" {
			out[m.Key] = true
		}
	}
	return out
}

// The reported bug: the game replaced a crash but its marker stayed for hours.
func TestMarkerWithoutPingsLeavesTheMap(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC()
	heli := map[string]any{"key": "ghost", "category": "heli_crash", "class": "Wreck_UH1Y"}
	e.send(ev("g1", "event.start", now, "", []float64{100, 0, 100}, heli))
	if !activeKeys(e)["ghost"] {
		t.Fatal("new marker must be active")
	}

	clock := now.Add(time.Duration(mapevents.DefaultStaleSeconds-10) * time.Second)
	e.App.MapEvents.Now = func() time.Time { return clock }
	if !activeKeys(e)["ghost"] {
		t.Fatal("marker removed before the limit")
	}

	clock = now.Add(time.Duration(mapevents.DefaultStaleSeconds+10) * time.Second)
	if activeKeys(e)["ghost"] {
		t.Fatal("marker still active after the limit with no ping")
	}

	// Still in the world after all (a late ping): it comes straight back.
	e.send(ev("g2", "event.seen", clock, "", []float64{100, 0, 100}, heli))
	if !activeKeys(e)["ghost"] {
		t.Fatal("a later ping must revive the marker")
	}
}

func TestPingsKeepMarkerAliveOnPanelClockWhateverTheServerClockSays(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC()
	heli := map[string]any{"key": "wrong-clock", "category": "heli_crash", "class": "Wreck_UH1Y"}
	// The game server's clock is half an hour behind; the panel receives it now.
	skewed := now.Add(-30 * time.Minute)
	e.send(ev("c1", "event.start", skewed, "", []float64{1, 0, 1}, heli))
	if !activeKeys(e)["wrong-clock"] {
		t.Fatal("a wrong game-server clock must not remove a marker that is being pinged")
	}
}

func TestDeathsAndRaidsKeepTheirOwnLifetime(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC()
	e.send(ev("d1", "player.death", now, steamA, []float64{10, 0, 20}, map[string]any{"cause": "environment"}))
	e.send(ev("r1", "raid.hit", now, steamA, []float64{4010, 0, 9010}, map[string]any{"class": "Fence"}))
	clock := now.Add(5 * time.Minute)
	e.App.MapEvents.Now = func() time.Time { return clock }
	var ms []mapevents.Marker
	e.JSON("GET", "/api/map/events", nil, &ms)
	status := map[string]string{}
	for _, m := range ms {
		status[m.Category] = m.Status
	}
	if status["death"] != "expired" || status["raid"] != "active" {
		t.Fatalf("own lifetimes not honoured: %+v", status)
	}
}

// Teleporting to a body needs its position long after the marker ended.
func TestEndedEventFoundByKeyWithPosition(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC().Add(-48 * time.Hour)
	e.send(ev("old1", "player.death", now, steamA, []float64{1234, 0, 5678}, map[string]any{"cause": "environment"}))
	var m mapevents.Marker
	if code := e.JSON("GET", "/api/map/event?key=death:old1", nil, &m); code != 200 {
		t.Fatalf("lookup: %d", code)
	}
	if m.X != 1234 || m.Z != 5678 || m.Status != "expired" {
		t.Fatalf("marker: %+v", m)
	}
	if code := e.JSON("GET", "/api/map/event?key=nope", nil, &m); code != 404 {
		t.Fatalf("unknown key: %d", code)
	}
	// The activity log carries the position too.
	var page struct {
		Items []struct {
			Type string `json:"type"`
			Pos  *struct {
				X float64 `json:"x"`
				Z float64 `json:"z"`
			} `json:"pos"`
		} `json:"items"`
	}
	e.JSON("GET", "/api/events?types=player.death", nil, &page)
	if len(page.Items) != 1 || page.Items[0].Pos == nil || page.Items[0].Pos.X != 1234 {
		t.Fatalf("activity position: %+v", page)
	}
}

func TestStaleSettingIsValidatedAndApplied(t *testing.T) {
	e := setup(t)
	if code := e.JSON("PUT", "/api/settings/map", map[string]int{"stale_seconds": 5}, nil); code != 400 {
		t.Fatalf("too low accepted: %d", code)
	}
	now := time.Now().UTC()
	e.send(ev("s1", "event.start", now, "", []float64{1, 0, 1}, map[string]any{"key": "k", "category": "police", "class": "X"}))
	if code := e.JSON("PUT", "/api/settings/map", map[string]int{"stale_seconds": 600}, nil); code != 200 {
		t.Fatalf("save: %d", code)
	}
	clock := now.Add(5 * time.Minute)
	e.App.MapEvents.Now = func() time.Time { return clock }
	if !activeKeys(e)["k"] {
		t.Fatal("removed before the configured limit")
	}
	clock = now.Add(11 * time.Minute)
	if activeKeys(e)["k"] {
		t.Fatal("kept past the configured limit")
	}
}

func (e *env) pushLive(t *testing.T, body map[string]any) int {
	t.Helper()
	raw, _ := json.Marshal(body)
	code, _ := e.Post("/ingest/v1/"+e.token+"/live", "application/json", raw, nil)
	return code
}

// Motorbikes never reach the map through MetricZ. The bridge's list must decide.
func TestLivePushDrivesPlayersAndVehicles(t *testing.T) {
	e := setup(t)
	bike := map[string]any{"id": "10:20", "class": "Motorbike_01_Blue", "type": "motorbike", "x": 100.0, "y": 0.0, "z": 200.0, "health": 1.0, "speed": 0.0, "crew": 0, "engine_on": false}
	boat := map[string]any{"id": "30:40", "class": "Boat_01_Black", "type": "boat", "x": 300.0, "y": 0.0, "z": 400.0}
	if code := e.pushLive(t, map[string]any{"v": 1, "full": true, "interval_s": 2,
		"players":  []map[string]any{{"steam64": steamA, "name": "Alice", "x": 11.0, "y": 0.0, "z": 22.0, "health": 0.5}},
		"vehicles": []map[string]any{bike, boat}, "gone": []string{}}); code != 200 {
		t.Fatalf("push: %d", code)
	}
	var st metricz.MapState
	e.JSON("GET", "/api/map/state", nil, &st)
	if !st.Available || !st.Fresh || st.Source != "bridge" || st.IntervalSec != 2 {
		t.Fatalf("state: %+v", st)
	}
	if len(st.Players) != 1 || st.Players[0].X != 11 || len(st.Vehicles) != 2 {
		t.Fatalf("entities: %+v", st)
	}

	// A partial push moves one vehicle, removes the other and keeps the rest of the list.
	bike["x"] = 150.0
	e.pushLive(t, map[string]any{"v": 1, "full": false, "players": []map[string]any{}, "vehicles": []map[string]any{bike}, "gone": []string{"30:40"}})
	st = metricz.MapState{}
	e.JSON("GET", "/api/map/state", nil, &st)
	if len(st.Vehicles) != 1 || st.Vehicles[0].Class != "Motorbike_01_Blue" || st.Vehicles[0].X != 150 {
		t.Fatalf("after partial push: %+v", st.Vehicles)
	}
	if len(st.Players) != 0 {
		t.Fatalf("a player who left must go at once: %+v", st.Players)
	}

	// A complete list removes anything it no longer contains.
	e.pushLive(t, map[string]any{"v": 1, "full": true, "players": []map[string]any{}, "vehicles": []map[string]any{}, "gone": []string{}})
	st = metricz.MapState{}
	e.JSON("GET", "/api/map/state", nil, &st)
	if len(st.Vehicles) != 0 {
		t.Fatalf("after full push: %+v", st.Vehicles)
	}
}

func TestLivePushRejectsBadTokenAndBadRows(t *testing.T) {
	e := setup(t)
	if code, _ := e.Post("/ingest/v1/not-the-token/live", "application/json", []byte(`{"v":1}`), nil); code != 401 {
		t.Fatalf("bad token: %d", code)
	}
	e.pushLive(t, map[string]any{"v": 1, "full": true,
		"players":  []map[string]any{{"steam64": "not-a-steam-id", "x": 1.0, "z": 1.0}, {"steam64": steamA, "x": -500.0, "z": 1.0}},
		"vehicles": []map[string]any{{"id": "1:2", "class": "<img src=x>", "x": 1.0, "z": 1.0}, {"id": "1:3", "class": "Sedan_02", "x": 1e9, "z": 1.0}}, "gone": []string{}})
	var st metricz.MapState
	e.JSON("GET", "/api/map/state", nil, &st)
	if len(st.Players) != 0 || len(st.Vehicles) != 0 {
		t.Fatalf("bad rows stored: %+v", st)
	}
}
