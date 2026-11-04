package metricz

import (
	"sort"
	"strings"
	"time"
)

// Player is a live player as reported by MetricZ. Fields are pointers so that
// "not reported" is distinguishable from zero.
type Player struct {
	Steam64     string   `json:"steam64"`
	BEGUID      string   `json:"reported_id,omitempty"` // MetricZ "guid" label = PlayerIdentity.GetId()
	Name        string   `json:"name"`
	X           float64  `json:"x"`
	Y           float64  `json:"y"` // altitude
	Z           float64  `json:"z"`
	Orientation *float64 `json:"orientation,omitempty"`
	Health      *float64 `json:"health,omitempty"` // 0..1
	Blood       *float64 `json:"blood,omitempty"`
	Shock       *float64 `json:"shock,omitempty"`
	Water       *float64 `json:"water,omitempty"`
	Energy      *float64 `json:"energy,omitempty"`
	Unconscious *bool    `json:"unconscious,omitempty"`
	Bleeding    *bool    `json:"bleeding,omitempty"`
	Restrained  *bool    `json:"restrained,omitempty"`
	Godmode     *bool    `json:"godmode,omitempty"`
	PingMax     *float64 `json:"ping_max,omitempty"`
	Lifetime    *float64 `json:"lifetime_seconds,omitempty"`
	Kills       *float64 `json:"players_killed,omitempty"`
	hasPos      int
}

// Vehicle is a live transport.
type Vehicle struct {
	ID          string   `json:"id"` // MetricZ persistent hash
	Class       string   `json:"class"`
	Type        string   `json:"type"`
	X           float64  `json:"x"`
	Y           float64  `json:"y"`
	Z           float64  `json:"z"`
	Orientation *float64 `json:"orientation,omitempty"`
	Health      *float64 `json:"health,omitempty"`
	Fuel        *float64 `json:"fuel,omitempty"`
	Crew        *float64 `json:"crew,omitempty"`
	Speed       *float64 `json:"speed,omitempty"`
	EngineOn    *bool    `json:"engine_on,omitempty"`
	hasPos      int
}

// Perf is server-level performance from one push.
type Perf struct {
	FPS        *float64 `json:"fps,omitempty"`
	FPSMin     *float64 `json:"fps_min,omitempty"`
	FPSAvg     *float64 `json:"fps_avg,omitempty"`
	Players    *float64 `json:"players,omitempty"`
	Infected   *float64 `json:"infected,omitempty"`
	Animals    *float64 `json:"animals,omitempty"`
	Items      *float64 `json:"items,omitempty"`
	Vehicles   int      `json:"vehicles"`
	UptimeSecs *float64 `json:"uptime_seconds,omitempty"`
}

// Snapshot is the state from the latest complete push.
type Snapshot struct {
	ServerID   string    `json:"server_id"`
	World      string    `json:"world"`
	ReceivedAt time.Time `json:"received_at"`
	Players    []Player  `json:"players"`
	Vehicles   []Vehicle `json:"vehicles"`
	Perf       Perf      `json:"perf"`
	Samples    int       `json:"samples"`
	BadLines   int       `json:"bad_lines"`
}

const prefix = "dayz_metricz_"

func f(v float64) *float64 { return &v }
func b(v float64) *bool    { x := v != 0; return &x }

// Build turns samples into a snapshot. Entities without a complete position are dropped
// (never guessed).
func Build(samples []Sample) Snapshot {
	players := map[string]*Player{}
	vehicles := map[string]*Vehicle{}
	var snap Snapshot
	for _, s := range samples {
		name := strings.TrimPrefix(s.Name, prefix)
		if name == s.Name {
			continue
		}
		if snap.World == "" {
			snap.World = s.Labels["world"]
		}
		switch {
		case strings.HasPrefix(name, "player_"):
			id := s.Labels["steam_id"]
			if id == "" {
				continue
			}
			p := players[id]
			if p == nil {
				p = &Player{Steam64: id}
				players[id] = p
			}
			if g := s.Labels["guid"]; g != "" {
				p.BEGUID = g
			}
			if n := s.Labels["name"]; n != "" {
				p.Name = n
			}
			applyPlayer(p, strings.TrimPrefix(name, "player_"), s.Value)
		case strings.HasPrefix(name, "transport_"):
			id := s.Labels["hash"]
			if id == "" {
				continue
			}
			v := vehicles[id]
			if v == nil {
				v = &Vehicle{ID: id}
				vehicles[id] = v
			}
			if c := s.Labels["class"]; c != "" {
				v.Class = c
			}
			if t := s.Labels["type"]; t != "" {
				v.Type = t
			}
			applyVehicle(v, strings.TrimPrefix(name, "transport_"), s.Value)
		default:
			applyPerf(&snap.Perf, name, s.Value)
		}
	}
	snap.Players = []Player{}
	for _, p := range players {
		if p.hasPos == 3 {
			snap.Players = append(snap.Players, *p)
		}
	}
	snap.Vehicles = []Vehicle{}
	for _, v := range vehicles {
		if v.hasPos == 3 {
			snap.Vehicles = append(snap.Vehicles, *v)
		}
	}
	sort.Slice(snap.Players, func(i, j int) bool { return snap.Players[i].Name < snap.Players[j].Name })
	sort.Slice(snap.Vehicles, func(i, j int) bool { return snap.Vehicles[i].ID < snap.Vehicles[j].ID })
	snap.Perf.Vehicles = len(snap.Vehicles)
	snap.Samples = len(samples)
	return snap
}

func applyPlayer(p *Player, m string, v float64) {
	switch m {
	case "position_x":
		p.X, p.hasPos = v, p.hasPos+1
	case "position_y":
		p.Y, p.hasPos = v, p.hasPos+1
	case "position_z":
		p.Z, p.hasPos = v, p.hasPos+1
	case "orientation":
		p.Orientation = f(v)
	case "health":
		p.Health = f(v)
	case "blood":
		p.Blood = f(v)
	case "shock":
		p.Shock = f(v)
	case "water":
		p.Water = f(v)
	case "energy":
		p.Energy = f(v)
	case "unconscious":
		p.Unconscious = b(v)
	case "bleeding_active":
		p.Bleeding = b(v)
	case "restrained":
		p.Restrained = b(v)
	case "godmode":
		p.Godmode = b(v)
	case "network_ping_max":
		p.PingMax = f(v)
	case "lifetime_seconds":
		p.Lifetime = f(v)
	case "stat_players_killed_total":
		p.Kills = f(v)
	}
}

func applyVehicle(t *Vehicle, m string, v float64) {
	switch m {
	case "position_x":
		t.X, t.hasPos = v, t.hasPos+1
	case "position_y":
		t.Y, t.hasPos = v, t.hasPos+1
	case "position_z":
		t.Z, t.hasPos = v, t.hasPos+1
	case "orientation":
		t.Orientation = f(v)
	case "health":
		t.Health = f(v)
	case "fuel_fraction":
		t.Fuel = f(v)
	case "crew_occupied":
		t.Crew = f(v)
	case "speed":
		t.Speed = f(v)
	case "engine_on":
		t.EngineOn = b(v)
	}
}

func applyPerf(p *Perf, m string, v float64) {
	switch m {
	case "fps":
		p.FPS = f(v)
	case "fps_window_min":
		p.FPSMin = f(v)
	case "fps_window_avg":
		p.FPSAvg = f(v)
	case "players_online":
		p.Players = f(v)
	case "infected":
		p.Infected = f(v)
	case "animals":
		p.Animals = f(v)
	case "items":
		p.Items = f(v)
	case "uptime_seconds":
		p.UptimeSecs = f(v)
	}
}
