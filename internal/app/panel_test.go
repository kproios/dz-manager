package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/mapevents"
	"testing"
	"time"
)

func TestActivityPaginationSearchAndHeartbeatFilter(t *testing.T) {
	e := setup(t)
	at := time.Now()
	for i := 0; i < 6; i++ {
		e.send(ev(fmt.Sprint("beat-", i), "heartbeat", at, "", nil, map[string]any{}))
	}
	for i := 0; i < 4; i++ {
		e.send(ev(fmt.Sprint("join-", i), "player.connect", at, steamA, nil, map[string]any{"name": "Survivor"}))
	}
	var page struct {
		Items []map[string]any `json:"items"`
		Next  int64            `json:"next_before"`
	}
	if code := e.JSON("GET", "/api/events?limit=2&q=Survivor", nil, &page); code != 200 || len(page.Items) != 2 || page.Next == 0 {
		t.Fatalf("page: %d %+v", code, page)
	}
	first := page.Items[0]["id"]
	if code := e.JSON("GET", fmt.Sprintf("/api/events?limit=2&before=%d&q=Survivor", page.Next), nil, &page); code != 200 || len(page.Items) != 2 || page.Next != 0 || page.Items[0]["id"] == first {
		t.Fatalf("older: %d %+v", code, page)
	}
	e.JSON("GET", "/api/events?limit=100", nil, &page)
	if len(page.Items) != 4 {
		t.Fatalf("heartbeats were not hidden: %d", len(page.Items))
	}
	e.JSON("GET", "/api/events?heartbeat=true&limit=100", nil, &page)
	if len(page.Items) != 10 {
		t.Fatalf("heartbeat opt-in: %d", len(page.Items))
	}
	e.JSON("GET", "/api/events?q=%25", nil, &page)
	if len(page.Items) != 0 {
		t.Fatal("search wildcard must be literal")
	}
}

func TestAuditSearchPaginationAndReadableTarget(t *testing.T) {
	e := setup(t)
	for i := 0; i < 4; i++ {
		if err := e.App.Server.Audit.Write(e.Ctx, audit.System, "restart.schedule_update", e.server, map[string]any{"times": []string{"12:00"}}, "ok", fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	var page audit.Page
	if code := e.JSON("GET", "/api/audit/search?q=restart%20schedule&limit=2&result=ok", nil, &page); code != 200 || len(page.Items) != 2 || page.NextBefore == 0 || page.Items[0].Target != "TestServer" {
		t.Fatalf("search: %d %+v", code, page)
	}
	first := page.Items[0].ID
	e.JSON("GET", fmt.Sprintf("/api/audit/search?q=restart%%20schedule&limit=2&before=%d", page.NextBefore), nil, &page)
	if len(page.Items) != 2 || page.NextBefore != 0 || page.Items[0].ID == first {
		t.Fatalf("older: %+v", page)
	}
	e.JSON("GET", "/api/audit/search?q=%25", nil, &page)
	if len(page.Items) != 0 {
		t.Fatal("wildcard not literal")
	}
}

func TestTrackingDeliveredOnPollAndObservedItemIsKnown(t *testing.T) {
	e := setup(t)
	tracking := mapevents.Tracking{EventClasses: map[string]string{"Wreck_UH1Y": "heli_crash"}, RareItems: []string{"M4A1"}}
	if code := e.JSON("PUT", "/api/settings/tracking", tracking, nil); code != 200 {
		t.Fatal(code)
	}
	_, body := e.Post("/ingest/v1/"+e.token+"/commands/poll", "application/json", []byte(`{}`), nil)
	var poll struct{ Tracking mapevents.Tracking }
	if err := json.Unmarshal([]byte(body), &poll); err != nil || len(poll.Tracking.RareItems) != 1 || poll.Tracking.RareItems[0] != "M4A1" {
		t.Fatalf("poll tracking: %v", err)
	}
	e.send(ev("observed-item", "item.observed", time.Now(), "", []float64{500, 10, 700}, map[string]any{"class": "M4A1", "key": "ground-1", "observation": "item.observed"}))
	var markers []mapevents.Marker
	if code := e.JSON("GET", "/api/map/events", nil, &markers); code != 200 || len(markers) != 1 || markers[0].Observation != "item.observed" {
		t.Fatalf("observed marker: %d %+v", code, markers)
	}
	var page struct {
		Items      []mapevents.Marker
		NextBefore int64 `json:"next_before"`
	}
	if code := e.JSON("GET", "/api/map/events?paged=true&q=M4A1&limit=1", nil, &page); code != 200 || len(page.Items) != 1 || page.NextBefore != 0 {
		t.Fatalf("marker page: %d %+v", code, page)
	}
}

func TestManualRestartRequiresVerificationAndPreventsDuplicates(t *testing.T) {
	e := setup(t)
	svc := e.App.Restarts
	sent := []string{}
	svc.Exec = func(_ context.Context, cmd string) error { sent = append(sent, cmd); return nil }
	svc.Observe(e.Ctx, e.server, true)
	if code := e.JSON("POST", "/api/restarts/now", map[string]string{"confirm": "RESTART"}, nil); code != 409 {
		t.Fatal("unverified restart accepted", code)
	}
	e.JSON("POST", "/api/restarts/drive-verification", map[string]any{"verified": true, "evidence": "isolated test provider restarts native shutdown"}, nil)
	if code := e.JSON("POST", "/api/restarts/now", map[string]string{"confirm": "wrong"}, nil); code != 400 {
		t.Fatal(code)
	}
	if code := e.JSON("POST", "/api/restarts/now", map[string]string{"confirm": "RESTART"}, nil); code != 202 || len(sent) != 1 || sent[0] != "#shutdown" {
		t.Fatalf("restart: %d %v", code, sent)
	}
	if code := e.JSON("POST", "/api/restarts/now", map[string]string{"confirm": "RESTART"}, nil); code != 409 || len(sent) != 1 {
		t.Fatal("duplicate restart", code, sent)
	}
	svc.Observe(e.Ctx, e.server, false)
	svc.Observe(e.Ctx, e.server, false)
	svc.Observe(e.Ctx, e.server, true)
	history, err := svc.Events(e.Ctx, e.server, 1)
	if err != nil || len(history) != 1 || history[0].Kind != "manual" || history[0].Outcome != "ok" {
		t.Fatalf("manual restart history: %+v %v", history, err)
	}
}

func TestMarkerRefreshDoesNotGrowActivityHistory(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC()
	data := map[string]any{"key": "world-1", "category": "custom", "class": "M4A1"}
	e.send(ev("start-1", "event.start", now, "", []float64{500, 10, 600}, data))
	for i := 0; i < 60; i++ {
		e.send(ev(fmt.Sprint("seen-", i), "event.seen", now.Add(time.Duration(i+1)*time.Second), "", []float64{501, 10, 601}, data))
	}
	var raw int
	e.App.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE server_id=?`, e.server).Scan(&raw)
	if raw != 1 {
		t.Fatalf("routine refreshes grew history: %d", raw)
	}
	var markers []mapevents.Marker
	e.JSON("GET", "/api/map/events", nil, &markers)
	if len(markers) != 1 || markers[0].X != 501 || markers[0].Status != "active" {
		t.Fatalf("live marker not refreshed: %+v", markers)
	}
	// Replayed older telemetry cannot move the marker backwards.
	e.send(ev("old-seen", "event.seen", now, "", []float64{100, 10, 100}, data))
	e.JSON("GET", "/api/map/events", nil, &markers)
	if markers[0].X != 501 {
		t.Fatal("old refresh moved marker backwards")
	}
}
