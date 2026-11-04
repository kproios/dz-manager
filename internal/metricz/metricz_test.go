package metricz_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kproios/dz-manager/internal/apptest"
	"github.com/kproios/dz-manager/internal/metricz"
	"github.com/kproios/dz-manager/internal/profiles"
)

// Fixture is synthetic, built from MetricZ's documented metric names/labels;
// replace with a recorded (redacted) TestServer payload once MetricZ is installed.
func fixture(t *testing.T) string {
	b, err := os.ReadFile("testdata/push.prom")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseAndBuild(t *testing.T) {
	samples, bad := metricz.Parse(fixture(t))
	if bad != 1 {
		t.Fatalf("bad lines = %d, want 1", bad)
	}
	s := metricz.Build(samples)
	if len(s.Players) != 1 {
		t.Fatalf("players = %+v (entity without position must be dropped)", s.Players)
	}
	p := s.Players[0]
	if p.Steam64 != "76561198000000001" || p.Name != `Survivor "One"` || p.BEGUID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("identity: %+v", p)
	}
	if p.X != 6560.12 || p.Z != 2480.5 || p.Y != 9.8 || *p.Health != 0.95 || *p.Unconscious {
		t.Fatalf("values: %+v", p)
	}
	if p.Shock != nil {
		t.Fatal("unreported field must stay nil")
	}
	if len(s.Vehicles) != 1 || s.Vehicles[0].Class != "OffroadHatchback" || !*s.Vehicles[0].EngineOn || s.Vehicles[0].X != 10450 {
		t.Fatalf("vehicles: %+v", s.Vehicles)
	}
	if *s.Perf.FPSAvg != 44.2 || *s.Perf.Players != 2 || s.World != "chernarusplus" {
		t.Fatalf("perf: %+v", s.Perf)
	}
}

func TestDecodeSerialized(t *testing.T) {
	lines := strings.Split(strings.TrimSpace(fixture(t)), "\n")
	b, _ := json.Marshal(lines)
	text, err := metricz.Decode(b)
	if err != nil || !strings.Contains(text, "player_position_x") {
		t.Fatalf("decode: %v", err)
	}
	if _, err := metricz.Decode([]byte(`[1,2`)); err == nil {
		t.Fatal("broken JSON accepted")
	}
}

func basic(u, p string) map[string]string {
	return map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(u+":"+p))}
}

func TestIngestEndToEnd(t *testing.T) {
	e := apptest.New(t, "")
	e.Login()
	var p profiles.Profile
	e.JSON("POST", "/api/profiles", profiles.Input{Name: "TestServer"}, &p)
	var wz profiles.Wizard
	e.JSON("GET", "/api/profiles/"+p.ID+"/wizard", nil, &wz)
	h := wz.MetricZConfig["http"].(map[string]any)
	user, pass := h["user"].(string), h["password"].(string)
	if !strings.HasSuffix(h["url"].(string), "/ingest/metricz") {
		t.Fatalf("wizard url %v", h["url"])
	}

	var st metricz.MapState
	e.JSON("GET", "/api/map/state", nil, &st)
	if st.Available || st.Reason == "" {
		t.Fatalf("before data: %+v", st)
	}
	if code, _ := e.Post("/ingest/metricz/api/v1/ingest/1", "text/plain", []byte(fixture(t)), basic(user, "wrong")); code != 401 {
		t.Fatalf("wrong password = %d", code)
	}
	lines := strings.Split(strings.TrimSpace(fixture(t)), "\n")
	body, _ := json.Marshal(lines)
	if code, msg := e.Post("/ingest/metricz/api/v1/ingest/1?format=json", "text/plain", body, basic(user, pass)); code != 200 {
		t.Fatalf("single = %d %s", code, msg)
	}
	e.JSON("GET", "/api/map/state", nil, &st)
	if !st.Available || !st.Fresh || len(st.Players) != 1 || len(st.Vehicles) != 1 {
		t.Fatalf("after push: %+v", st)
	}
	if !e.App.Caps.OK(p.ID, "metricz") {
		t.Fatal("metricz capability not ok")
	}

	// Chunked transaction: chunks out of order, then commit.
	half := len(lines) / 2
	c0, _ := json.Marshal(lines[:half])
	c1, _ := json.Marshal(lines[half:])
	if code, _ := e.Post("/ingest/metricz/api/v1/ingest/1/tx1/1", "text/plain", c1, basic(user, pass)); code != 200 {
		t.Fatal("chunk 1")
	}
	if code, _ := e.Post("/ingest/metricz/api/v1/commit/1/tx2", "text/plain", nil, basic(user, pass)); code != 404 {
		t.Fatal("unknown txn commit should 404")
	}
	if code, _ := e.Post("/ingest/metricz/api/v1/commit/1/tx1", "text/plain", nil, basic(user, pass)); code != 400 {
		t.Fatal("commit with missing chunk 0 should fail")
	}
	e.Post("/ingest/metricz/api/v1/ingest/1/tx3/0", "text/plain", c0, basic(user, pass))
	e.Post("/ingest/metricz/api/v1/ingest/1/tx3/1", "text/plain", c1, basic(user, pass))
	if code, msg := e.Post("/ingest/metricz/api/v1/commit/1/tx3", "text/plain", nil, basic(user, pass)); code != 200 {
		t.Fatalf("commit = %d %s", code, msg)
	}
	var perf []metricz.PerfPoint
	e.JSON("GET", "/api/perf?hours=1", nil, &perf)
	if len(perf) != 1 || *perf[0].FPS != 44.2 {
		t.Fatalf("perf: %+v", perf)
	}
	// Staleness.
	e.App.MetricZ.StaleAfter = time.Millisecond
	time.Sleep(5 * time.Millisecond)
	e.JSON("GET", "/api/map/state", nil, &st)
	if st.Fresh || !strings.HasPrefix(st.Reason, "stale") {
		t.Fatalf("stale: %+v", st)
	}
	// Empty payload rejected.
	if code, _ := e.Post("/ingest/metricz/api/v1/ingest/1", "text/plain", []byte("# nothing"), basic(user, pass)); code != 400 {
		t.Fatal("empty payload accepted")
	}
}

func TestLoad(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		id := "7656119800000" + strings.Repeat("0", 4-len(itoa(i))) + itoa(i)
		for _, m := range []string{"position_x", "position_y", "position_z", "health", "blood", "shock", "water", "energy"} {
			sb.WriteString(`dayz_metricz_player_` + m + `{world="chernarusplus",steam_id="` + id + `"} 1234.5` + "\n")
		}
	}
	text := sb.String()
	start := time.Now()
	for i := 0; i < 100; i++ {
		s, _ := metricz.Parse(text)
		if len(metricz.Build(s).Players) != 60 {
			t.Fatal("build")
		}
	}
	per := time.Since(start) / 100
	t.Logf("60-player payload (%d bytes) parsed+built in %v", len(text), per)
	if per > 50*time.Millisecond {
		t.Fatalf("too slow: %v", per)
	}
}

func itoa(i int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+i/10)) + string(rune('0'+i%10))) }
