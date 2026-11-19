package app_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kproios/dz-manager/internal/mapevents"
)

const steamKiller = "76561198000000077"

func publicGet(t *testing.T, e *env, path, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", e.TS.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestDeathBecomesMapMarkerAndPublicKillFeed(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC()
	e.send(
		ev("c1", "player.connect", now.Add(-time.Hour), steamA, nil, map[string]any{"name": "Alice"}),
		ev("c2", "player.connect", now.Add(-time.Hour), steamKiller, nil, map[string]any{"name": "Bandit"}),
	)
	e.send(ev("d1", "player.death", now, steamA, []float64{5000, 0, 9000}, map[string]any{"killer": steamKiller, "weapon": "M4A1", "distance_m": 120, "killer_pos": map[string]float64{"x": 5100, "y": 0, "z": 9050}}))
	var markers []mapevents.Marker
	e.JSON("GET", "/api/map/events", nil, &markers)
	var death *mapevents.Marker
	for i := range markers {
		if markers[i].Category == "death" {
			death = &markers[i]
		}
	}
	if death == nil || death.Name != "Alice killed by Bandit" || !strings.Contains(string(death.Data), `"killer_pos"`) {
		t.Fatalf("death marker: %+v", markers)
	}

	// The bot API needs a token and never shows Steam IDs.
	if code, _ := publicGet(t, e, "/public/v1/status", ""); code != 401 {
		t.Fatalf("no token: %d", code)
	}
	var tok struct {
		Token string `json:"token"`
	}
	if code := e.JSON("POST", "/api/settings/api-keys", map[string]any{"name": "Discord bot", "scopes": []string{"public"}}, &tok); code != 201 || !strings.HasPrefix(tok.Token, "dzm_") {
		t.Fatalf("token: %d %+v", code, tok)
	}
	code, body := publicGet(t, e, "/public/v1/killfeed", tok.Token)
	if code != 200 || !strings.Contains(body, `"killer":"Bandit"`) || !strings.Contains(body, `"victim":"Alice"`) || strings.Contains(body, steamA) {
		t.Fatalf("killfeed: %d %s", code, body)
	}
	code, body = publicGet(t, e, "/public/v1/feed", tok.Token)
	var feed []map[string]any
	if code != 200 || json.Unmarshal([]byte(body), &feed) != nil || !strings.Contains(body, "Bandit killed Alice") {
		t.Fatalf("feed: %d %s", code, body)
	}
	if code, _ := publicGet(t, e, "/public/v1/status", tok.Token); code != 200 {
		t.Fatalf("status: %d", code)
	}
}

func TestWorldEventEndShowsWhichEvent(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC()
	e.send(ev("s1", "event.start", now.Add(-time.Minute), "", []float64{8000, 0, 8000}, map[string]any{"key": "boot:heli", "category": "heli_crash", "class": "Wreck_UH1Y"}))
	e.send(ev("e1", "event.end", now, "", nil, map[string]any{"key": "boot:heli"}))
	var page struct {
		Items []map[string]any `json:"items"`
	}
	e.JSON("GET", "/api/events?limit=10&types=event.end", nil, &page)
	if len(page.Items) != 1 {
		t.Fatalf("items: %+v", page.Items)
	}
	sub, _ := page.Items[0]["subject"].(map[string]any)
	if sub == nil || sub["category"] != "heli_crash" || sub["class"] != "Wreck_UH1Y" {
		t.Fatalf("subject: %+v", page.Items[0])
	}
}
