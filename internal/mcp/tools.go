package mcp

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
)

var (
	reSteam   = regexp.MustCompile(`^[0-9]{17}$`)
	reVehicle = regexp.MustCompile(`^-?[0-9]{1,10}:-?[0-9]{1,10}$`)
)

func str(args map[string]any, k string) string {
	if v, ok := args[k].(string); ok {
		return v
	}
	return ""
}

func num(args map[string]any, k string) (float64, bool) {
	switch v := args[k].(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	}
	return 0, false
}

func get(path string) func(map[string]any) (string, string, url.Values, any, error) {
	return func(map[string]any) (string, string, url.Values, any, error) { return "GET", path, nil, nil, nil }
}

func getQ(path string, keys ...string) func(map[string]any) (string, string, url.Values, any, error) {
	return func(a map[string]any) (string, string, url.Values, any, error) {
		q := url.Values{}
		for _, k := range keys {
			if s := str(a, k); s != "" {
				q.Set(k, s)
			} else if n, ok := num(a, k); ok {
				q.Set(k, strconv.FormatFloat(n, 'f', -1, 64))
			}
		}
		return "GET", path, q, nil, nil
	}
}

var limitProp = map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "description": "How many to return"}
var steamProp = map[string]any{"type": "string", "pattern": "^[0-9]{17}$", "description": "The player's Steam64 ID"}

// Tools is every tool. tools/list shows only those the key's scopes allow.
var Tools = []Tool{
	{Name: "server_status", Title: "Server status", Scope: "public", ReadOnly: true,
		Description: "Server name, map, players online and the maximum, uptime, server FPS and the next scheduled restart.",
		Build:       get("/status")},
	{Name: "players_online", Title: "Players online", Scope: "public", ReadOnly: true,
		Description: "Names of the players online right now.", Build: get("/online")},
	{Name: "kill_feed", Title: "Kill feed", Scope: "public", ReadOnly: true,
		Description: "The latest player kills: killer, victim, weapon, distance and time.",
		Props:       map[string]any{"limit": limitProp}, Build: getQ("/killfeed", "limit")},
	{Name: "leaderboard", Title: "Leaderboard", Scope: "public", ReadOnly: true,
		Description: "Top players by kills, K/D (at least 5 kills) or playtime.",
		Props:       map[string]any{"by": map[string]any{"type": "string", "enum": []string{"kills", "kd", "playtime"}}, "limit": limitProp},
		Build:       getQ("/leaderboard", "by", "limit")},
	{Name: "player_stats", Title: "Player stats by name", Scope: "public", ReadOnly: true,
		Description: "Kills, deaths, playtime, first and last seen for a player name (exact, case-insensitive).",
		Props:       map[string]any{"name": map[string]any{"type": "string"}}, Required: []string{"name"},
		Build: getQ("/player", "name")},
	{Name: "recent_events", Title: "Recent events", Scope: "public", ReadOnly: true,
		Description: "Recent public events: kills, deaths, joins and leaves, raids (never where), world events, bans and restarts. Pass after = the last id you saw to get only newer ones.",
		Props:       map[string]any{"after": map[string]any{"type": "integer"}, "limit": limitProp}, Build: getQ("/feed", "after", "limit")},
	{Name: "world_events", Title: "Active world events", Scope: "public", ReadOnly: true,
		Description: "How many helicrashes, aircraft crashes, airdrops, convoys and trains are on the map now (counts only).", Build: get("/events")},

	{Name: "search_players", Title: "Search players", Scope: "players:read", ReadOnly: true,
		Description: "Players by name (contains) or exact Steam64, with kills, deaths, playtime, first/last seen, and whether they are banned or have queue priority.",
		Props:       map[string]any{"q": map[string]any{"type": "string"}, "sort": map[string]any{"type": "string", "enum": []string{"last_seen", "kills", "playtime"}}, "limit": limitProp},
		Build:       getQ("/players", "q", "sort", "limit")},
	{Name: "get_player", Title: "Player by Steam64", Scope: "players:read", ReadOnly: true,
		Description: "One player's stats and ban/priority status.", Props: map[string]any{"steam64": steamProp}, Required: []string{"steam64"},
		Build: func(a map[string]any) (string, string, url.Values, any, error) {
			id := str(a, "steam64")
			if !reSteam.MatchString(id) {
				return "", "", nil, nil, errors.New("steam64 must be a 17-digit Steam64 ID")
			}
			return "GET", "/players/" + id, nil, nil, nil
		}},
	{Name: "ban_list", Title: "Ban list", Scope: "bans:read", ReadOnly: true,
		Description: "Active bans with Steam64, BattlEye GUID, name, reason and end.", Build: get("/banlist")},
	{Name: "priority_list", Title: "Priority queue", Scope: "priority:read", ReadOnly: true,
		Description: "Players with queue priority and when it ends.", Build: get("/priority")},
	{Name: "live_map", Title: "Live map", Scope: "map:read", ReadOnly: true,
		Description: "The world (name and size in metres), every player online and every vehicle with positions (x east, z north), health, fuel, crew and engine state, and the active map events. Vehicle ids here are what vehicle_action takes.",
		Build:       get("/map")},

	{Name: "player_action", Title: "Act on a player", Scope: "actions:write", Destructive: true,
		Description: "Runs an action on an online player through the game server: teleport (x and z, or to_player), give_item (class, quantity 1-20), heal, kill (needs confirm \"KILL\") or strip (deletes everything they carry, needs confirm \"STRIP\"). Ask the human first; returns what the game server reported.",
		Props: map[string]any{
			"steam64":   steamProp,
			"action":    map[string]any{"type": "string", "enum": []string{"teleport", "give_item", "heal", "kill", "strip"}},
			"x":         map[string]any{"type": "number"},
			"z":         map[string]any{"type": "number"},
			"to_player": steamProp,
			"class":     map[string]any{"type": "string", "description": "give_item: DayZ classname"},
			"quantity":  map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
			"reason":    map[string]any{"type": "string", "maxLength": 120},
			"confirm":   map[string]any{"type": "string", "description": "KILL or STRIP, only after the human approved"},
		},
		Required: []string{"steam64", "action"},
		Build: func(a map[string]any) (string, string, url.Values, any, error) {
			id, act := str(a, "steam64"), str(a, "action")
			if !reSteam.MatchString(id) {
				return "", "", nil, nil, errors.New("steam64 must be a 17-digit Steam64 ID")
			}
			body := map[string]any{}
			for _, k := range []string{"to_player", "class", "reason", "confirm"} {
				if v := str(a, k); v != "" {
					body[k] = v
				}
			}
			for _, k := range []string{"x", "z", "quantity"} {
				if v, ok := num(a, k); ok {
					body[k] = v
				}
			}
			return "POST", "/players/" + id + "/actions/" + url.PathEscape(act), nil, body, nil
		}},
	{Name: "vehicle_action", Title: "Act on a vehicle", Scope: "actions:write", Destructive: true,
		Description: "Runs an action on a vehicle (id from live_map): vehicle_refuel, vehicle_repair (pristine vehicle and parts, fluids topped up), vehicle_flip (back on its wheels, refused while moving), vehicle_stop_engine, or vehicle_delete (removes it and its cargo, refused while someone is inside, needs confirm \"DELETE\"). Ask the human first.",
		Props: map[string]any{
			"vehicle_id": map[string]any{"type": "string", "pattern": "^-?[0-9]+:-?[0-9]+$"},
			"action":     map[string]any{"type": "string", "enum": []string{"vehicle_refuel", "vehicle_repair", "vehicle_flip", "vehicle_stop_engine", "vehicle_delete"}},
			"confirm":    map[string]any{"type": "string", "description": "DELETE for vehicle_delete, only after the human approved"},
		},
		Required: []string{"vehicle_id", "action"},
		Build: func(a map[string]any) (string, string, url.Values, any, error) {
			id := str(a, "vehicle_id")
			if !reVehicle.MatchString(id) {
				return "", "", nil, nil, errors.New("vehicle_id must be an id from live_map, like 12345:678")
			}
			body := map[string]any{}
			if c := str(a, "confirm"); c != "" {
				body["confirm"] = c
			}
			return "POST", "/vehicles/" + url.PathEscape(id) + "/actions/" + url.PathEscape(str(a, "action")), nil, body, nil
		}},
	{Name: "send_message", Title: "Message players", Scope: "actions:write",
		Description: "Shows an on-screen message to everyone, or to one player when steam64 is given. Ask the human first.",
		Props:       map[string]any{"text": map[string]any{"type": "string", "maxLength": 200}, "steam64": steamProp},
		Required:    []string{"text"},
		Build: func(a map[string]any) (string, string, url.Values, any, error) {
			text := str(a, "text")
			if text == "" || len(text) > 200 {
				return "", "", nil, nil, fmt.Errorf("text is required (up to 200 characters)")
			}
			return "POST", "/message", nil, map[string]any{"text": text, "steam64": str(a, "steam64")}, nil
		}},

	{Name: "restart_server", Title: "Restart the game server", Scope: "server:restart", Destructive: true,
		Description: "Restarts the game server: delay_minutes 0 shuts it down now, 1-30 starts a countdown that warns players first. Needs confirm \"RESTART\". The hosting provider brings the server back; watch server_status for it. Refused while the server is offline or a restart is already under way. Ask the human first.",
		Props: map[string]any{
			"delay_minutes": map[string]any{"type": "integer", "enum": []int{0, 1, 2, 5, 10, 15, 30}, "description": "0 = now"},
			"confirm":       map[string]any{"type": "string", "description": "RESTART, only after the human approved"},
		},
		Required: []string{"confirm"},
		Build: func(a map[string]any) (string, string, url.Values, any, error) {
			body := map[string]any{"confirm": str(a, "confirm"), "delay_minutes": 0}
			if n, ok := num(a, "delay_minutes"); ok {
				body["delay_minutes"] = int(n)
			}
			return "POST", "/restart", nil, body, nil
		}},
}
