package app_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kproios/dz-manager/internal/bridge"
	"github.com/kproios/dz-manager/internal/worldmap"
)

func (e *env) key(t *testing.T, scopes ...string) string {
	t.Helper()
	var out struct {
		Token string `json:"token"`
	}
	if code := e.JSON("POST", "/api/settings/api-keys", map[string]any{"name": "test " + strings.Join(scopes, "+"), "scopes": scopes}, &out); code != 201 {
		t.Fatalf("create key %v: %d", scopes, code)
	}
	return out.Token
}

func call(t *testing.T, e *env, method, path, token string, body any, hdr map[string]string) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, e.TS.URL+path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestVehicleActionsAndWorld(t *testing.T) {
	e := setup(t)
	now := time.Now()
	// Before the bridge reports, the map is the Chernarus default.
	var w worldmap.Info
	var resp struct {
		World worldmap.Info `json:"world"`
	}
	e.JSON("GET", "/api/map/world", nil, &resp)
	if resp.World.World != "chernarusplus" || resp.World.Source != "default" || resp.World.Size != 15360 {
		t.Fatalf("default world: %+v", resp.World)
	}
	// The bridge reports Livonia with its place names in two chunks.
	e.send(ev("w0", "world.info", now, "", nil, map[string]any{"world": "enoch", "label": "Livonia", "size": 12800, "snapshot": "s1", "parts": 2}))
	e.send(ev("w1", "world.places", now, "", nil, map[string]any{"snapshot": "s1", "part": 1, "places": []map[string]any{{"n": "Topolin", "k": "NameCity", "x": 1800, "z": 7400}}}))
	e.send(ev("w2", "world.places", now, "", nil, map[string]any{"snapshot": "s1", "part": 0, "places": []map[string]any{{"n": "Brena", "k": "NameCityCapital", "x": 6500, "z": 11200}, {"n": "Off map", "k": "NameCity", "x": 99999, "z": 1}}}))
	e.JSON("GET", "/api/map/world", nil, &resp)
	w = resp.World
	if w.World != "enoch" || w.Size != 12800 || w.Source != "bridge" || len(w.Places) != 2 || w.Places[0].Name != "Brena" {
		t.Fatalf("bridge world: %+v", w)
	}
	// Teleports are checked against the reported size.
	e.send(ev("c1", "player.connect", now, steamA, nil, map[string]any{"name": "Alice"}))
	e.send(ev("hb", "heartbeat", now, "", nil, map[string]any{"mod_version": "0.2.0", "commands": []string{"teleport", "vehicle_refuel", "vehicle_delete", "vehicle_flip"}}))
	if code := e.JSON("POST", "/api/players/"+steamA+"/actions/teleport", map[string]any{"x": 14000.0, "z": 100.0}, nil); code != 400 {
		t.Fatalf("teleport outside Livonia = %d", code)
	}

	var cat []map[string]any
	e.JSON("GET", "/api/vehicles/actions", nil, &cat)
	avail := map[string]bool{}
	for _, c := range cat {
		avail[c["name"].(string)] = c["available"].(bool)
	}
	if !avail["vehicle_refuel"] || avail["vehicle_repair"] || len(cat) != 5 {
		t.Fatalf("vehicle catalog: %+v", cat)
	}
	stop := make(chan struct{})
	defer close(stop)
	var got []bridge.Command
	e.fakeMod(t, stop, func(c bridge.Command) (bool, string, any) {
		got = append(got, c)
		if c.Action == "vehicle_flip" {
			return false, "the vehicle is moving", nil
		}
		return true, "", map[string]any{"fuel": 1}
	})
	var res map[string]any
	if code := e.JSON("POST", "/api/vehicles/123:456/actions/vehicle_refuel", map[string]any{}, &res); code != 200 || res["ok"] != true {
		t.Fatalf("refuel = %d %v", code, res)
	}
	if code := e.JSON("POST", "/api/vehicles/123:456/actions/vehicle_delete", map[string]any{}, nil); code != 400 {
		t.Fatalf("delete without DELETE = %d", code)
	}
	if code := e.JSON("POST", "/api/vehicles/123:456/actions/vehicle_delete", map[string]any{"confirm": "DELETE"}, &res); code != 200 {
		t.Fatalf("delete = %d %v", code, res)
	}
	if code := e.JSON("POST", "/api/vehicles/123:456/actions/vehicle_flip", map[string]any{}, &res); code != 502 || res["error"] != "the vehicle is moving" {
		t.Fatalf("flip refused by mod = %d %v", code, res)
	}
	if code := e.JSON("POST", "/api/vehicles/not-an-id/actions/vehicle_refuel", map[string]any{}, nil); code != 400 {
		t.Fatalf("bad vehicle id = %d", code)
	}
	if code := e.JSON("POST", "/api/vehicles/1:2/actions/heal", map[string]any{}, nil); code != 400 {
		t.Fatalf("player action on a vehicle = %d", code)
	}
	if len(got) != 3 || got[0].Target != "123:456" || got[0].Action != "vehicle_refuel" {
		t.Fatalf("commands: %+v", got)
	}
	var n int
	e.App.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action IN ('vehicle.refuel','vehicle.delete','vehicle.flip') AND target='vehicle:123:456'`).Scan(&n)
	if n != 3 {
		t.Fatalf("vehicle audit rows = %d", n)
	}
}

func TestRaidFeedNeverSaysWhere(t *testing.T) {
	e := setup(t)
	tok := e.key(t, "public")
	now := time.Now().UTC()
	e.send(ev("r1", "raid.hit", now, steamA, []float64{4012, 0, 9034}, map[string]any{"class": "Fence", "part": "wall"}))
	code, body := call(t, e, "GET", "/public/v1/feed", tok, nil, nil)
	if code != 200 || !strings.Contains(body, "A base is under attack") {
		t.Fatalf("feed: %d %s", code, body)
	}
	for _, leak := range []string{"Grid", "040", "4012", "9034", steamA} {
		if strings.Contains(body, leak) {
			t.Fatalf("raid feed leaks %q: %s", leak, body)
		}
	}
}

func TestAPIKeyScopesAndExports(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC()
	e.send(ev("c1", "player.connect", now, steamA, nil, map[string]any{"name": "Alice"}))
	e.App.DB.Exec(`INSERT INTO bans(steam64,be_guid,reason,created_at,issuer_sub,issuer_name,applied) VALUES(?,?,?,?,?,?,?)`, steamA, strings.Repeat("ab", 16), "cheating", now.Format(time.RFC3339), "x", "x", "pending")
	e.App.DB.Exec(`INSERT INTO priority(steam64,reason,added_by,created_at,updated_at) VALUES(?,?,?,?,?)`, steamA, "supporter", "x", now.Format(time.RFC3339), now.Format(time.RFC3339))

	pub := e.key(t, "public")
	bans := e.key(t, "bans:read", "priority:read", "players:read")
	if code, _ := call(t, e, "GET", "/public/v1/banlist", pub, nil, nil); code != 403 {
		t.Fatalf("public key on banlist = %d", code)
	}
	if code, _ := call(t, e, "GET", "/public/v1/banlist", "dzm_wrong", nil, nil); code != 401 {
		t.Fatalf("bad key = %d", code)
	}
	code, body := call(t, e, "GET", "/public/v1/banlist?format=battleye", bans, nil, nil)
	if code != 200 || strings.TrimSpace(body) != strings.Repeat("ab", 16)+" -1 cheating" {
		t.Fatalf("battleye export: %d %q", code, body)
	}
	code, body = call(t, e, "GET", "/public/v1/priority?format=txt", bans, nil, nil)
	if code != 200 || strings.TrimSpace(body) != steamA {
		t.Fatalf("priority txt: %d %q", code, body)
	}
	code, body = call(t, e, "GET", "/public/v1/players/"+steamA, bans, nil, nil)
	if code != 200 || !strings.Contains(body, `"banned":true`) || !strings.Contains(body, `"priority":true`) {
		t.Fatalf("player: %d %s", code, body)
	}
	// The public ban list shows names, never IDs.
	code, body = call(t, e, "GET", "/public/v1/bans", pub, nil, nil)
	if code != 200 || !strings.Contains(body, "Alice") || strings.Contains(body, steamA) {
		t.Fatalf("public bans: %d %s", code, body)
	}
	// CORS for browsers, and the OpenAPI document without a key.
	code, _ = call(t, e, "OPTIONS", "/public/v1/status", "", nil, map[string]string{"Origin": "https://example.org"})
	if code != 204 {
		t.Fatalf("preflight = %d", code)
	}
	code, body = call(t, e, "GET", "/public/v1/openapi.json", "", nil, nil)
	if code != 200 || !strings.Contains(body, `"/vehicles/{id}/actions/{action}"`) {
		t.Fatalf("openapi: %d", code)
	}
	// A revoked key stops working.
	var list struct {
		Keys []struct {
			ID     string   `json:"id"`
			Scopes []string `json:"scopes"`
		} `json:"keys"`
	}
	e.JSON("GET", "/api/settings/api-keys", nil, &list)
	for _, k := range list.Keys {
		if len(k.Scopes) == 1 && k.Scopes[0] == "public" {
			if code := e.JSON("DELETE", "/api/settings/api-keys/"+k.ID, nil, nil); code != 200 {
				t.Fatalf("revoke = %d", code)
			}
		}
	}
	if code, _ := call(t, e, "GET", "/public/v1/status", pub, nil, nil); code != 401 {
		t.Fatalf("revoked key = %d", code)
	}
}

func mcpCall(t *testing.T, e *env, token string, method string, params any) (int, map[string]any) {
	t.Helper()
	code, body := call(t, e, "POST", "/mcp", token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}, map[string]string{"Accept": "application/json, text/event-stream"})
	var out map[string]any
	_ = json.Unmarshal([]byte(body), &out)
	return code, out
}

func TestMCP(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC()
	e.send(ev("c1", "player.connect", now, steamA, nil, map[string]any{"name": "Alice"}))
	read := e.key(t, "public", "players:read")
	write := e.key(t, "public", "actions:write")

	if code, _ := mcpCall(t, e, "", "initialize", map[string]any{}); code != 401 {
		t.Fatalf("no key = %d", code)
	}
	code, body := call(t, e, "POST", "/mcp", read, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, map[string]string{"Origin": "https://evil.example"})
	if code != 403 {
		t.Fatalf("browser origin = %d %s", code, body)
	}
	_, init := mcpCall(t, e, read, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test"}})
	r, _ := init["result"].(map[string]any)
	if r == nil || r["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize: %v", init)
	}
	if code, _ := call(t, e, "POST", "/mcp", read, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}, nil); code != 202 {
		t.Fatalf("notification = %d", code)
	}
	names := func(tok string) []string {
		_, res := mcpCall(t, e, tok, "tools/list", map[string]any{})
		var out []string
		for _, x := range res["result"].(map[string]any)["tools"].([]any) {
			out = append(out, x.(map[string]any)["name"].(string))
		}
		return out
	}
	readTools := strings.Join(names(read), ",")
	if !strings.Contains(readTools, "search_players") || strings.Contains(readTools, "player_action") || strings.Contains(readTools, "ban_list") {
		t.Fatalf("read key tools: %s", readTools)
	}
	if !strings.Contains(strings.Join(names(write), ","), "vehicle_action") {
		t.Fatal("write key lacks vehicle_action")
	}
	_, res := mcpCall(t, e, read, "tools/call", map[string]any{"name": "search_players", "arguments": map[string]any{"q": "Ali"}})
	result, _ := res["result"].(map[string]any)
	if result == nil || result["isError"] == true || !strings.Contains(fmt.Sprint(result["content"]), steamA) {
		t.Fatalf("search_players: %v", res)
	}
	// A tool outside the key's scopes does not exist for it.
	_, res = mcpCall(t, e, read, "tools/call", map[string]any{"name": "player_action", "arguments": map[string]any{"steam64": steamA, "action": "heal"}})
	if res["error"] == nil {
		t.Fatalf("read key ran a write tool: %v", res)
	}
	// Write tools go through the same validation and audit as the panel.
	e.send(ev("hb", "heartbeat", now, "", nil, map[string]any{"mod_version": "0.2.0", "commands": []string{"heal", "kill", "vehicle_repair"}}))
	stop := make(chan struct{})
	defer close(stop)
	e.fakeMod(t, stop, func(c bridge.Command) (bool, string, any) { return true, "", map[string]any{"done": true} })
	_, res = mcpCall(t, e, write, "tools/call", map[string]any{"name": "player_action", "arguments": map[string]any{"steam64": steamA, "action": "kill"}})
	if result, _ = res["result"].(map[string]any); result["isError"] != true || !strings.Contains(fmt.Sprint(result["content"]), "KILL") {
		t.Fatalf("kill without confirm: %v", res)
	}
	_, res = mcpCall(t, e, write, "tools/call", map[string]any{"name": "vehicle_action", "arguments": map[string]any{"vehicle_id": "5:6", "action": "vehicle_repair"}})
	if result, _ = res["result"].(map[string]any); result == nil || result["isError"] == true {
		t.Fatalf("vehicle_repair: %v", res)
	}
	var actor string
	e.App.DB.QueryRow(`SELECT actor_name FROM audit_log WHERE action='vehicle.repair'`).Scan(&actor)
	if !strings.Contains(actor, "API key") {
		t.Fatalf("audit actor = %q", actor)
	}
}

func TestOldBotTokenKeepsWorking(t *testing.T) {
	e := setup(t)
	token := "dzm_" + strings.Repeat("c", 48)
	b, _ := json.Marshal(map[string]any{"token_hash": e.App.Box.Hash(token), "created_at": "2026-11-01T00:00:00Z", "info": "rules"})
	e.App.DB.Exec(`INSERT INTO settings(key,value) VALUES('bot_api',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(b))
	e.App.Keys.MigrateBotToken(e.Ctx)
	if code, body := call(t, e, "GET", "/public/v1/info", token, nil, nil); code != 200 || !strings.Contains(body, "rules") {
		t.Fatalf("old token: %d %s", code, body)
	}
}
