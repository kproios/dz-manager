package app_test

import (
	"strings"
	"testing"
	"time"
)

// @DZMBridge's minute report feeds the dashboard and public status without
// MetricZ, is stored in the perf history, and never in the activity events.
func TestBridgePerfReplacesMetricZ(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC()
	e.send(ev("p1", "server.perf", now, "", nil, map[string]any{"fps": 58.5, "fps_avg": 59.2, "fps_min": 41, "uptime_seconds": 754,
		"players": 3, "infected": 812, "animals": 64, "items": 120345, "vehicles": 47}))

	var st struct {
		PerfSource string `json:"perf_source"`
		Perf       struct {
			FPSAvg   float64 `json:"fps_avg"`
			Uptime   float64 `json:"uptime_seconds"`
			Infected float64 `json:"infected"`
			Vehicles int     `json:"vehicles"`
		} `json:"perf"`
	}
	if code := e.JSON("GET", "/api/map/state", nil, &st); code != 200 || st.PerfSource != "bridge" || st.Perf.FPSAvg != 59.2 || st.Perf.Uptime != 754 || st.Perf.Infected != 812 || st.Perf.Vehicles != 47 {
		t.Fatalf("map state perf: %d %+v", code, st)
	}
	var hist []struct {
		FPS    *float64 `json:"fps"`
		FPSMin *float64 `json:"fps_min"`
		Items  *float64 `json:"items"`
	}
	if code := e.JSON("GET", "/api/perf?hours=1", nil, &hist); code != 200 || len(hist) != 1 || *hist[0].FPS != 59.2 || *hist[0].FPSMin != 41 || *hist[0].Items != 120345 {
		t.Fatalf("perf history: %+v", hist)
	}
	tok := e.key(t, "public")
	if code, body := call(t, e, "GET", "/public/v1/status", tok, nil, nil); code != 200 || !strings.Contains(body, `"uptime_seconds":754`) || !strings.Contains(body, `"fps":59.2`) {
		t.Fatalf("public status: %d %s", code, body)
	}
	var n int
	e.App.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE type='server.perf'`).Scan(&n)
	if n != 0 {
		t.Fatalf("perf reports stored as activity events: %d", n)
	}
	// A report from long ago (backlog) is not taken as current.
	e.send(ev("p0", "server.perf", now.Add(-time.Hour), "", nil, map[string]any{"fps": 5, "uptime_seconds": 1}))
	e.JSON("GET", "/api/map/state", nil, &st)
	if st.Perf.Uptime != 754 {
		t.Fatalf("old report replaced the current one: %+v", st)
	}
}
