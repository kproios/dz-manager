package app_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/woozymasta/dzid"

	"github.com/kproios/dz-manager/internal/actions"
	"github.com/kproios/dz-manager/internal/apptest"
	"github.com/kproios/dz-manager/internal/bans"
	"github.com/kproios/dz-manager/internal/bridge"
	"github.com/kproios/dz-manager/internal/mapevents"
	"github.com/kproios/dz-manager/internal/profiles"
)

const steamA = "76561198000000001"

type env struct {
	*apptest.Env
	token, server string
	be            *apptest.FakeBE
}

func setup(t *testing.T) *env {
	e := apptest.New(t, "")
	e.Login()
	be := apptest.StartBE(t, "pw")
	var p profiles.Profile
	e.JSON("POST", "/api/profiles", profiles.Input{Name: "TestServer", GameHost: "127.0.0.1", RconPort: be.Port, RconPassword: "pw"}, &p)
	var wz profiles.Wizard
	e.JSON("GET", "/api/profiles/"+p.ID+"/wizard", nil, &wz)
	if _, ok := wz.BridgeConfig["rare_items"]; !ok {
		t.Fatal("wizard bridge config lacks tracking extras")
	}
	return &env{Env: e, token: wz.BridgeConfig["server_token"].(string), server: p.ID, be: be}
}

func (e *env) send(evs ...map[string]any) bridge.Result {
	body, _ := json.Marshal(map[string]any{"v": 1, "emitter": "dzmbridge@0.1.0", "events": evs})
	_, out := e.Post("/ingest/v1/"+e.token+"/events", "application/json", body, nil)
	var r bridge.Result
	_ = json.Unmarshal([]byte(out), &r)
	return r
}

func ev(id, typ string, at time.Time, actor string, pos []float64, data map[string]any) map[string]any {
	m := map[string]any{"id": id, "type": typ, "ts": at.UTC().Format(time.RFC3339), "actor": actor, "data": data}
	if pos != nil {
		m["pos"] = map[string]float64{"x": pos[0], "y": pos[1], "z": pos[2]}
	}
	return m
}

// fakeMod answers commands like @DZMBridge would.
func (e *env) fakeMod(t *testing.T, stop chan struct{}, respond func(c bridge.Command) (bool, string, any)) {
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
			}
			_, out := e.Post("/ingest/v1/"+e.token+"/commands/poll", "application/json", []byte(`{}`), nil)
			var r struct{ Commands []bridge.Command }
			json.Unmarshal([]byte(out), &r)
			for _, c := range r.Commands {
				ok, msg, data := respond(c)
				b, _ := json.Marshal(map[string]any{"ok": ok, "error": msg, "data": data})
				e.Post("/ingest/v1/"+e.token+"/commands/"+c.ID+"/result", "application/json", b, nil)
			}
		}
	}()
}

func TestActions(t *testing.T) {
	e := setup(t)
	now := time.Now()
	e.send(ev("c1", "player.connect", now, steamA, nil, map[string]any{"name": "Alice"}))
	var cat []actions.Availability
	e.JSON("GET", "/api/actions", nil, &cat)
	if cat[0].Available {
		t.Fatal("actions must be unavailable without a bridge heartbeat")
	}
	if code := e.JSON("POST", "/api/players/"+steamA+"/actions/heal", map[string]any{}, nil); code != 409 {
		t.Fatalf("heal without bridge = %d", code)
	}
	e.send(ev("hb", "heartbeat", now, "", nil, map[string]any{"mod_version": "0.1.0", "commands": []string{"teleport", "give_item", "heal", "kill"}}))
	stop := make(chan struct{})
	defer close(stop)
	var got []bridge.Command
	e.fakeMod(t, stop, func(c bridge.Command) (bool, string, any) {
		got = append(got, c)
		if c.Action == "give_item" {
			return false, "unknown classname", nil
		}
		return true, "", map[string]any{"health": 100}
	})
	var res map[string]any
	if code := e.JSON("POST", "/api/players/"+steamA+"/actions/heal", map[string]any{}, &res); code != 200 || res["ok"] != true {
		t.Fatalf("heal = %d %v", code, res)
	}
	if code := e.JSON("POST", "/api/players/"+steamA+"/actions/teleport", map[string]any{"x": 7000.0, "z": 2500.0}, &res); code != 200 {
		t.Fatalf("teleport = %d %v", code, res)
	}
	if code := e.JSON("POST", "/api/players/"+steamA+"/actions/give_item", map[string]any{"class": "M4A1", "quantity": 1}, &res); code != 502 || res["error"] != "unknown classname" {
		t.Fatalf("give_item mod error = %d %v", code, res)
	}
	if code := e.JSON("POST", "/api/players/"+steamA+"/actions/kill", map[string]any{"confirm": "nope"}, nil); code != 400 {
		t.Fatalf("kill without confirmation = %d", code)
	}
	if code := e.JSON("POST", "/api/players/"+steamA+"/actions/teleport", map[string]any{"x": -5.0, "z": 1.0}, nil); code != 400 {
		t.Fatalf("out-of-map teleport = %d", code)
	}
	if code := e.JSON("POST", "/api/players/"+steamA+"/actions/give_item", map[string]any{"class": "M4A1; rm -rf", "quantity": 1}, nil); code != 400 {
		t.Fatalf("bad classname = %d", code)
	}
	if code := e.JSON("POST", "/api/players/76561198999999999/actions/heal", map[string]any{}, nil); code != 404 {
		t.Fatalf("unknown player = %d", code)
	}
	if len(got) != 3 || got[1].Target != steamA || !strings.Contains(string(got[1].Params), `"x":7000`) {
		t.Fatalf("commands seen by mod: %+v", got)
	}
	var n int
	e.App.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action LIKE 'player.%'`).Scan(&n)
	if n != 4 { // 3 executed + 1 refused (no bridge): failed attempts are audited too
		t.Fatalf("action audit rows = %d", n)
	}
}

func TestMapEvents(t *testing.T) {
	e := setup(t)
	now := time.Now()
	// Owner configures rare items.
	if code := e.JSON("PUT", "/api/settings/tracking", mapevents.Tracking{EventClasses: map[string]string{"Wreck_UH1Y": "heli_crash", "AirdropContainer": "airdrop"}, RareItems: []string{"M4A1"}}, nil); code != 200 {
		t.Fatalf("tracking = %d", code)
	}
	e.send(
		ev("s1", "event.start", now.Add(-10*time.Minute), "", []float64{7000, 300, 8000}, map[string]any{"key": "heli:1", "category": "heli_crash", "class": "Wreck_UH1Y"}),
		ev("s2", "event.start", now.Add(-5*time.Minute), "", []float64{9000, 200, 9000}, map[string]any{"key": "drop:1", "category": "airdrop", "class": "AirdropContainer"}),
		ev("s3", "event.start", now, "", nil, map[string]any{"key": "nopos", "category": "heli_crash"}),
		ev("i1", "item.spawn", now, "", []float64{100, 1, 100}, map[string]any{"class": "M4A1"}),
		ev("i2", "item.spawn", now, "", []float64{100, 1, 100}, map[string]any{"class": "AKM"}), // not allow-listed
		// End arrives before start (out of order): stays ended.
		ev("e3", "event.end", now.Add(-time.Minute), "", nil, map[string]any{"key": "drop:2"}),
		ev("s4", "event.start", now.Add(-2*time.Minute), "", []float64{1, 1, 1}, map[string]any{"key": "drop:2", "category": "airdrop"}),
	)
	e.send(ev("e1", "event.end", now, "", nil, map[string]any{"key": "heli:1"}))
	var ms []mapevents.Marker
	e.JSON("GET", "/api/map/events", nil, &ms)
	status := map[string]string{}
	for _, m := range ms {
		status[m.Key] = m.Status
	}
	if status["heli:1"] != "ended" || status["drop:1"] != "active" || status["drop:2"] != "ended" || len(ms) != 4 {
		t.Fatalf("markers: %+v", ms)
	}
	if _, has := status["nopos"]; has {
		t.Fatal("event without position must not be drawn")
	}
	var item mapevents.Marker
	for _, m := range ms {
		if m.Category == "rare_item" {
			item = m
		}
	}
	if item.Class != "M4A1" || item.Status != "historical" {
		t.Fatalf("rare item: %+v", item)
	}
	// Restart ends everything active.
	e.send(ev("st", "server.start", now.Add(time.Minute), "", nil, nil))
	e.JSON("GET", "/api/map/events", nil, &ms)
	for _, m := range ms {
		if m.Status == "active" {
			t.Fatalf("active after restart: %+v", m)
		}
	}
	// TTL fallback.
	e.send(ev("s9", "event.start", now.Add(2*time.Minute), "", []float64{5, 5, 5}, map[string]any{"key": "ttl", "category": "custom", "ttl_s": 1}))
	e.App.MapEvents.Now = func() time.Time { return now.Add(time.Hour) }
	e.JSON("GET", "/api/map/events", nil, &ms)
	for _, m := range ms {
		if m.Key == "ttl" && m.Status != "expired" {
			t.Fatalf("ttl: %+v", m)
		}
	}
	if code := e.JSON("PUT", "/api/settings/tracking", mapevents.Tracking{RareItems: []string{"bad item"}}, nil); code != 400 {
		t.Fatal("invalid classname accepted")
	}
}

func TestBans(t *testing.T) {
	e := setup(t)
	now := time.Now()
	guid := dzid.BattlEyeString(76561198000000001)
	e.send(ev("c1", "player.connect", now, steamA, nil, map[string]any{"name": "Alice"}))
	e.be.SetPlayers("Players on server:\n[#] [IP Address]:[Port] [Ping] [GUID] [Name]\n--------------------------------------------------\n4  127.0.0.1:2302 42 " + guid + "(OK) Alice\n(1 players in total)")
	e.be.AddForeignBan(strings.Repeat("ab", 16), "cftools ban")

	var b bans.Ban
	if code := e.JSON("POST", "/api/bans", map[string]any{"steam64": steamA, "reason": "cheating", "minutes": 0}, &b); code != 201 || b.BEGUID != guid || b.Applied != "ok" {
		t.Fatalf("ban = %d %+v", code, b)
	}
	sent := strings.Join(e.be.Sent(), "|")
	if !strings.Contains(sent, "addBan "+guid+" 0 [DZM#1] cheating") || !strings.Contains(sent, "kick 4 Banned: cheating") {
		t.Fatalf("rcon: %s", sent)
	}
	if code := e.JSON("POST", "/api/bans", map[string]any{"steam64": steamA, "reason": "again"}, nil); code != 409 {
		t.Fatalf("duplicate ban = %d", code)
	}
	if code := e.JSON("POST", "/api/bans", map[string]any{"steam64": steamA, "be_guid": strings.Repeat("0", 32), "reason": "x"}, nil); code != 400 {
		t.Fatal("mismatched guid accepted")
	}
	var d bans.Drift
	e.JSON("GET", "/api/bans/reconcile", nil, &d)
	if d.InSync != 1 || len(d.Foreign) != 1 || len(d.Missing) != 0 {
		t.Fatalf("drift: %+v", d)
	}
	if code := e.JSON("POST", "/api/bans/1/revoke", nil, &b); code != 200 || b.Status != "revoked" {
		t.Fatalf("revoke = %d %+v", code, b)
	}
	if e.be.BanCount() != 1 {
		t.Fatal("foreign ban must survive; ours must be removed")
	}

	// External list: import, exact-type match, never enforced.
	csv := "identifier,identifier_type,reason\n" + steamA + ",steam64,listed elsewhere\n" + guid + ",steam64,wrong type\n"
	var imp map[string]any
	if code := e.JSON("POST", "/api/banlists/import", map[string]any{"name": "Community", "format": "csv", "content": csv}, &imp); code != 400 {
		t.Fatalf("import with wrong-type row must be rejected: %d %v", code, imp)
	}
	csv = "identifier,identifier_type,reason\n" + steamA + ",steam64,listed elsewhere\n" + steamA + ",steam64,dupe\n" + strings.Repeat("c", 32) + ",be_guid,other\n"
	if code := e.JSON("POST", "/api/banlists/import", map[string]any{"name": "Community", "format": "csv", "content": csv}, &imp); code != 200 || imp["entries"].(float64) != 2 {
		t.Fatalf("import = %d %v", code, imp)
	}
	var f bans.Flags
	e.JSON("GET", "/api/players/"+steamA+"/flags", nil, &f)
	if len(f.External) != 1 || f.External[0].List != "Community" || f.LocalBan != nil {
		t.Fatalf("flags: %+v", f)
	}
	var q []map[string]any
	e.JSON("GET", "/api/review-queue", nil, &q)
	if len(q) != 1 {
		t.Fatalf("queue: %v", q)
	}
	e.JSON("POST", "/api/players/"+steamA+"/review", map[string]any{"decision": "watch", "note": "check"}, nil)
	e.JSON("GET", "/api/review-queue", nil, &q)
	if len(q) != 0 {
		t.Fatal("reviewed player still queued")
	}
	if n := len(e.be.Sent()); strings.Count(strings.Join(e.be.Sent(), "|"), "addBan") != 1 || n == 0 {
		t.Fatal("external match must never issue a ban")
	}
}
