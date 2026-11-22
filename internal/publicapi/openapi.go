package publicapi

// OpenAPI returns the OpenAPI 3.1 description of /public/v1 (served without
// a key at /public/v1/openapi.json so tools can read it).
func OpenAPI(version string) map[string]any {
	type m = map[string]any
	str := m{"type": "string"}
	steam := m{"name": "steam64", "in": "path", "required": true, "schema": m{"type": "string", "pattern": "^[0-9]{17}$"}, "description": "Steam64 ID"}
	vehicle := m{"name": "id", "in": "path", "required": true, "schema": m{"type": "string", "pattern": "^-?[0-9]+:-?[0-9]+$"}, "description": "Vehicle id from GET /map (the bridge's network id)"}
	limit := func(def, max int) m {
		return m{"name": "limit", "in": "query", "schema": m{"type": "integer", "minimum": 1, "maximum": max, "default": def}}
	}
	ok := func(desc string) m {
		return m{"200": m{"description": desc, "content": m{"application/json": m{}}}, "401": m{"description": "missing or invalid API key"}, "403": m{"description": "the key lacks this scope"}}
	}
	get := func(scope, summary, desc string, params ...m) m {
		op := m{"summary": summary, "description": desc + " Scope: `" + scope + "`.", "security": []m{{"apiKey": []string{scope}}}, "responses": ok(summary), "tags": []string{scope}}
		if len(params) > 0 {
			op["parameters"] = params
		}
		return m{"get": op}
	}
	actionBody := m{"content": m{"application/json": m{"schema": m{"type": "object", "properties": m{
		"confirm": m{"type": "string", "description": "The action's confirmation word for destructive actions (KILL, STRIP, DELETE)"},
		"reason":  m{"type": "string", "maxLength": 120},
		"x":       m{"type": "number"}, "z": m{"type": "number"}, "y": m{"type": "number"},
		"to_player": m{"type": "string", "description": "teleport: Steam64 of the player to go to"},
		"class":     m{"type": "string", "description": "give_item: classname"},
		"quantity":  m{"type": "integer", "minimum": 1, "maximum": 20},
	}}}}}
	post := func(scope, summary, desc string, body m, params ...m) m {
		return m{"post": m{"summary": summary, "description": desc + " Scope: `" + scope + "`.", "security": []m{{"apiKey": []string{scope}}}, "parameters": params, "requestBody": body,
			"responses": m{"200": m{"description": "done in game (the result is what the game server reported)"}, "400": m{"description": "invalid input"}, "401": m{"description": "missing or invalid API key"}, "403": m{"description": "the key lacks this scope"}, "502": m{"description": "the game server refused or failed"}, "504": m{"description": "no answer from the game server"}},
			"tags":      []string{scope}}}
	}
	format := func(values ...string) m {
		return m{"name": "format", "in": "query", "schema": m{"type": "string", "enum": values, "default": values[0]}}
	}
	return m{
		"openapi": "3.1.0",
		"info": m{"title": "DZ Manager public API", "version": version,
			"description": "API keys are created in DZ Manager under Settings → Integrations, each with its own scopes. Send `Authorization: Bearer dzm_…`. Only put a key with just the `public` scope in browser code."},
		"servers":    []m{{"url": "/public/v1"}},
		"components": m{"securitySchemes": m{"apiKey": m{"type": "http", "scheme": "bearer", "bearerFormat": "dzm_…"}}},
		"paths": m{
			"/status":      get("public", "Server status", "Name, map, players online, max players, uptime, FPS, next restart."),
			"/online":      get("public", "Players online", "Names of the players online now."),
			"/info":        get("public", "Server info text", "The text written in Settings → Integrations (rules, links)."),
			"/feed":        get("public", "Event feed", "Kills, deaths, joins, raids (never where), world events, bans and restarts. Poll with after= the last id you saw.", m{"name": "after", "in": "query", "schema": m{"type": "integer"}}, limit(100, 500)),
			"/killfeed":    get("public", "Kill feed", "Latest player kills.", limit(20, 100)),
			"/leaderboard": get("public", "Leaderboard", "Top players.", m{"name": "by", "in": "query", "schema": m{"type": "string", "enum": []string{"kills", "kd", "playtime"}}}, limit(10, 50)),
			"/player":      get("public", "Player stats by name", "Kills, deaths and playtime of one player.", m{"name": "name", "in": "query", "required": true, "schema": str}),
			"/bans":        get("public", "Ban list (names)", "Active bans with name, reason and end. No IDs.", limit(50, 200)),
			"/events":      get("public", "World event counts", "How many helicrashes, airdrops, convoys… are active. No positions."),
			"/players": get("players:read", "Players", "Players with Steam64, stats, and whether they are banned or have priority.",
				m{"name": "q", "in": "query", "schema": str, "description": "name contains, or exact Steam64"},
				m{"name": "sort", "in": "query", "schema": m{"type": "string", "enum": []string{"last_seen", "kills", "playtime"}}}, limit(50, 500),
				m{"name": "offset", "in": "query", "schema": m{"type": "integer", "minimum": 0}}),
			"/players/{steam64}": get("players:read", "One player", "Stats of one player.", steam),
			"/banlist":           get("bans:read", "Ban list export", "Active bans to share with other servers. json, txt (one Steam64 per line) or battleye (bans.txt: GUID minutes reason; -1 = permanent).", format("json", "txt", "battleye")),
			"/priority":          get("priority:read", "Priority queue export", "Players with queue priority and when it ends.", format("json", "txt")),
			"/map":               get("map:read", "Live map", "The world (name, size), live players and vehicles with positions, and active events."),
			"/players/{steam64}/actions/{action}": post("actions:write", "Player action", "teleport, give_item, heal, kill (confirm KILL) or strip (confirm STRIP). Needs @DZMBridge.", actionBody, steam,
				m{"name": "action", "in": "path", "required": true, "schema": m{"type": "string", "enum": []string{"teleport", "give_item", "heal", "kill", "strip"}}}),
			"/vehicles/{id}/actions/{action}": post("actions:write", "Vehicle action", "vehicle_refuel, vehicle_repair, vehicle_flip, vehicle_stop_engine or vehicle_delete (confirm DELETE; refused while someone is inside). Needs @DZMBridge.", actionBody, vehicle,
				m{"name": "action", "in": "path", "required": true, "schema": m{"type": "string", "enum": []string{"vehicle_refuel", "vehicle_repair", "vehicle_flip", "vehicle_stop_engine", "vehicle_delete"}}}),
			"/message": post("actions:write", "Message players", "On-screen message to everyone, or to one player with steam64.",
				m{"content": m{"application/json": m{"schema": m{"type": "object", "required": []string{"text"}, "properties": m{"text": m{"type": "string", "maxLength": 200}, "steam64": str}}}}}),
			"/restart": post("server:restart", "Restart the server", "delay_minutes 0 shuts the server down now (the provider relaunches it); 1-30 starts a countdown that warns players. confirm must be RESTART. 409 while the server is offline, the provider relaunch is unverified or a restart is under way.",
				m{"content": m{"application/json": m{"schema": m{"type": "object", "required": []string{"confirm"}, "properties": m{"confirm": m{"type": "string", "enum": []string{"RESTART"}}, "delay_minutes": m{"type": "integer", "enum": []int{0, 1, 2, 5, 10, 15, 30}}}}}}}),
		},
	}
}
