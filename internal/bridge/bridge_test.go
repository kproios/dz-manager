package bridge_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/woozymasta/dzid"

	"github.com/kproios/dz-manager/internal/apptest"
	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/bridge"
	"github.com/kproios/dz-manager/internal/players"
	"github.com/kproios/dz-manager/internal/profiles"
)

const (
	alice = "76561198000000001"
	bob   = "76561198000000002"
)

type env struct {
	*apptest.Env
	token, server string
}

func setup(t *testing.T) *env {
	e := apptest.New(t, "")
	e.Login()
	var p profiles.Profile
	e.JSON("POST", "/api/profiles", profiles.Input{Name: "TestServer"}, &p)
	var wz profiles.Wizard
	e.JSON("GET", "/api/profiles/"+p.ID+"/wizard", nil, &wz)
	return &env{Env: e, token: wz.BridgeConfig["server_token"].(string), server: p.ID}
}

func (e *env) send(evs ...map[string]any) (int, bridge.Result) {
	body, _ := json.Marshal(map[string]any{"v": 1, "server_id": e.server, "emitter": "dzmbridge@0.1.0", "seq": 1, "events": evs})
	code, out := e.Post("/ingest/v1/"+e.token+"/events", "application/json", body, nil)
	var r bridge.Result
	_ = json.Unmarshal([]byte(out), &r)
	return code, r
}

func ev(id, typ string, at time.Time, actor string, data map[string]any) map[string]any {
	m := map[string]any{"id": id, "type": typ, "ts": at.UTC().Format(time.RFC3339), "actor": actor, "data": data}
	return m
}

func TestIngestValidationAndDedupe(t *testing.T) {
	e := setup(t)
	now := time.Now()
	body, _ := json.Marshal(map[string]any{"v": 1, "events": []any{}})
	if code, _ := e.Post("/ingest/v1/wrong-token/events", "application/json", body, nil); code != 401 {
		t.Fatalf("bad token = %d", code)
	}
	wrongServer, _ := json.Marshal(map[string]any{"v": 1, "server_id": "other", "events": []any{}})
	if code, _ := e.Post("/ingest/v1/"+e.token+"/events", "application/json", wrongServer, nil); code != 403 {
		t.Fatalf("server mismatch = %d", code)
	}
	v2, _ := json.Marshal(map[string]any{"v": 2, "events": []any{}})
	if code, _ := e.Post("/ingest/v1/"+e.token+"/events", "application/json", v2, nil); code != 400 {
		t.Fatalf("version mismatch = %d", code)
	}
	code, r := e.send(
		ev("e1", "heartbeat", now, "", map[string]any{"mod_version": "0.1.0", "commands": []string{"ping", "list_players"}}),
		ev("e2", "BAD TYPE", now, "", nil),
		ev("e3", "custom.thing", now, "", map[string]any{"k": 1}),
		map[string]any{"id": "e4", "type": "x.y", "ts": "yesterday"},
		ev("e5", "x.y", now.Add(time.Hour), "", nil),
	)
	if code != 200 || r.Accepted != 2 || len(r.Rejected) != 3 {
		t.Fatalf("first batch: %d %+v", code, r)
	}
	_, r = e.send(ev("e1", "heartbeat", now, "", nil), ev("e3", "custom.thing", now, "", nil))
	if r.Duplicates != 2 || r.Accepted != 0 {
		t.Fatalf("dedupe: %+v", r)
	}
	if !e.App.Caps.OK(e.server, "bridge") || !e.App.Bridge.Supports(e.server, "list_players") || e.App.Bridge.Supports(e.server, "kill") {
		t.Fatal("heartbeat capabilities")
	}
	var raw []map[string]any
	e.JSON("GET", "/api/events/raw?type=custom.thing", nil, &raw)
	if len(raw) != 1 || raw[0]["known"] != false {
		t.Fatalf("unknown type must be stored raw and flagged: %+v", raw)
	}
	big := make([]map[string]any, bridge.MaxEvents+1)
	for i := range big {
		big[i] = ev("b"+strings.Repeat("x", i%5), "x.y", now, "", nil)
	}
	if code, _ := e.send(big...); code != 413 {
		t.Fatalf("oversized batch = %d", code)
	}
}

func TestCommandsLifecycle(t *testing.T) {
	e := setup(t)
	now := time.Now()
	b := e.App.Bridge
	if _, err := b.Enqueue(e.Ctx, e.server, "ping", "", map[string]any{}, systemActor, 0); err == nil {
		t.Fatal("enqueue without live bridge must fail")
	}
	e.send(ev("hb", "heartbeat", now, "", map[string]any{"mod_version": "0.1.0", "commands": []string{"ping"}}))
	id, err := b.Enqueue(e.Ctx, e.server, "ping", "", map[string]any{"n": 1}, systemActor, 0)
	if err != nil {
		t.Fatal(err)
	}
	poll := func() []bridge.Command {
		_, out := e.Post("/ingest/v1/"+e.token+"/commands/poll", "application/json", []byte(`{}`), nil)
		var r struct{ Commands []bridge.Command }
		json.Unmarshal([]byte(out), &r)
		return r.Commands
	}
	if cs := poll(); len(cs) != 1 || cs[0].ID != id || cs[0].Action != "ping" {
		t.Fatalf("poll: %+v", cs)
	}
	if cs := poll(); len(cs) != 0 {
		t.Fatal("command delivered twice")
	}
	if code, _ := e.Post("/ingest/v1/"+e.token+"/commands/nope/result", "application/json", []byte(`{"ok":true}`), nil); code != 404 {
		t.Fatalf("unknown result = %d", code)
	}
	if code, _ := e.Post("/ingest/v1/"+e.token+"/commands/"+id+"/result", "application/json", []byte(`{"ok":true,"data":{"pong":1}}`), nil); code != 200 {
		t.Fatalf("result = %d", code)
	}
	c, _ := b.Await(e.Ctx, id)
	if c.Status != "ok" || string(c.Result) != `{"pong":1}` {
		t.Fatalf("completed: %+v", c)
	}
	// Expired command is never delivered.
	id2, _ := b.Enqueue(e.Ctx, e.server, "ping", "", nil, systemActor, time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if cs := poll(); len(cs) != 0 {
		t.Fatalf("expired command delivered: %+v", cs)
	}
	c, _ = b.Get(e.Ctx, id2)
	if c.Status != "expired" {
		t.Fatalf("status = %s", c.Status)
	}
	// Delivered then timed out: late result recorded but not success.
	id3, _ := b.Enqueue(e.Ctx, e.server, "ping", "", nil, systemActor, 300*time.Millisecond)
	poll()
	time.Sleep(350 * time.Millisecond)
	e.Post("/ingest/v1/"+e.token+"/commands/"+id3+"/result", "application/json", []byte(`{"ok":true}`), nil)
	c, _ = b.Get(e.Ctx, id3)
	if c.Status != "expired" || !strings.Contains(c.Error, "late result") {
		t.Fatalf("late result: %+v", c)
	}
}

func TestPlayerHistory(t *testing.T) {
	e := setup(t)
	t0 := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	guidA := dzid.BattlEyeString(76561198000000001)
	e.send(
		ev("c1", "player.connect", t0, alice, map[string]any{"name": "Alice", "be_guid": guidA}),
		ev("c2", "player.connect", t0.Add(time.Minute), bob, map[string]any{"name": "Bob", "be_guid": "ffffffffffffffffffffffffffffffff"}),
	)
	// Out-of-order: death arrives before an earlier rename/reconnect.
	e.send(ev("d1", "player.death", t0.Add(30*time.Minute), bob, map[string]any{"killer": alice, "weapon": "M4A1", "distance_m": 120.5}))
	e.send(ev("d1", "player.death", t0.Add(30*time.Minute), bob, map[string]any{"killer": alice})) // duplicate
	e.send(ev("d2", "player.death", t0.Add(40*time.Minute), alice, map[string]any{"cause": "fall", "distance_m": 999}))
	e.send(ev("x1", "player.disconnect", t0.Add(60*time.Minute), alice, nil))
	// Alice reconnects under a new name; Bob's session is still open when the server crashes.
	e.send(ev("c3", "player.connect", t0.Add(70*time.Minute), alice, map[string]any{"name": "Alice2"}))
	e.send(ev("s1", "server.start", t0.Add(120*time.Minute), "", nil))

	var pa players.Profile
	if code := e.JSON("GET", "/api/players/"+alice, nil, &pa); code != 200 {
		t.Fatalf("profile = %d", code)
	}
	if pa.Player.Name != "Alice2" || len(pa.Names) != 2 || pa.Player.BEGUID != guidA || pa.Player.IDMismatch {
		t.Fatalf("alice identity: %+v names=%v", pa.Player, pa.Names)
	}
	if pa.Player.Kills != 1 || pa.Player.Deaths != 1 {
		t.Fatalf("alice k/d: %+v", pa.Player)
	}
	if len(pa.Sessions) != 2 || pa.Sessions[1].EndKind != "disconnect" || pa.Sessions[1].DurationS != 3600 || pa.Sessions[0].EndKind != "inferred_end" {
		t.Fatalf("alice sessions: %+v", pa.Sessions)
	}
	var env players.Combat
	for _, c := range pa.Combat {
		if c.Role == "death" {
			env = c
		}
	}
	if env.Killer != "" || env.DistanceM != nil || env.Cause != "fall" {
		t.Fatalf("environmental death must have no killer/distance: %+v", env)
	}
	var pb players.Profile
	e.JSON("GET", "/api/players/"+bob, nil, &pb)
	if pb.Player.Deaths != 1 || !pb.Player.IDMismatch || pb.Sessions[0].EndKind != "inferred_end" {
		t.Fatalf("bob: %+v %+v", pb.Player, pb.Sessions)
	}
	// Lookup by BE GUID and search by old name.
	if code := e.JSON("GET", "/api/players/"+guidA, nil, &pa); code != 200 {
		t.Fatal("lookup by guid")
	}
	var list []players.Player
	e.JSON("GET", "/api/players?q=Alice", nil, &list)
	if len(list) != 1 || list[0].Steam64 != alice {
		t.Fatalf("search: %+v", list)
	}
}

var systemActor = audit.System
