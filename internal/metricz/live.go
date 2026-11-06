package metricz

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/kproios/dz-manager/internal/httpx"
)

// The live layer: @DZMBridge pushes player and vehicle positions every couple of
// seconds, far faster than MetricZ's 15 s export, and it also sees vehicle types
// MetricZ never registers (MotorbikeScript is not a CarScript). There is one rule
// for what the map shows: while the bridge is fresh it is the authority for
// positions and for the vehicle list; MetricZ fills in the player details the
// bridge does not send, and takes over completely if the bridge goes quiet.

const (
	liveStaleAfter = 15 * time.Second
	maxLiveBody    = 1 << 20
	maxLivePlayers = 512
	maxLiveVehicle = 5000
	worldLimit     = 40000 // metres; larger than any map, rejects garbage only
)

var (
	reLiveSteam = regexp.MustCompile(`^[0-9]{17}$`)
	reLiveID    = regexp.MustCompile(`^-?[0-9]+:-?[0-9]+$`)
	reLiveClass = regexp.MustCompile(`^[A-Za-z0-9_]{1,80}$`)
)

// LiveUpdate is one push from the bridge. Full means Vehicles is the complete list;
// otherwise it holds only vehicles that changed, and Gone those that were deleted.
type LiveUpdate struct {
	V           int       `json:"v"`
	Full        bool      `json:"full"`
	IntervalSec float64   `json:"interval_s"`
	Players     []Player  `json:"players"`
	Vehicles    []Vehicle `json:"vehicles"`
	Gone        []string  `json:"gone"`
}

// LiveDelta is what browsers receive over the event stream, so a phone gets a few
// hundred bytes every two seconds instead of refetching the whole map.
type LiveDelta struct {
	ServerID    string    `json:"server_id"`
	ReceivedAt  time.Time `json:"received_at"`
	IntervalSec float64   `json:"interval_s"`
	Players     []Player  `json:"players"`
	// VehiclesReady is false until the first complete vehicle list has arrived;
	// until then the map keeps MetricZ's vehicles and ignores these fields.
	VehiclesReady bool      `json:"vehicles_ready"`
	Full          bool      `json:"full"`
	Vehicles      []Vehicle `json:"vehicles"`
	Gone          []string  `json:"gone"`
}

type liveState struct {
	at            time.Time
	interval      float64
	players       []Player
	vehicles      map[string]Vehicle
	vehiclesReady bool
}

// live handles POST /ingest/v1/{token}/live.
func (rc *Receiver) live(w http.ResponseWriter, r *http.Request) {
	id, ok := rc.Profiles.AuthBridge(r.Context(), r.PathValue("token"))
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "invalid server token")
		return
	}
	body, err := httpx.ReadLimited(r, maxLiveBody)
	if err != nil {
		httpx.Error(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	var up LiveUpdate
	if err := json.Unmarshal(body, &up); err != nil {
		httpx.Error(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if len(up.Players) > maxLivePlayers || len(up.Vehicles) > maxLiveVehicle {
		httpx.Error(w, 413, "too many entities")
		return
	}
	delta := rc.ApplyLive(id, up, time.Now().UTC())
	if rc.PublishLive != nil {
		rc.PublishLive(delta)
	}
	httpx.JSON(w, 200, map[string]bool{"ok": true})
}

func inWorld(x, z float64) bool { return x >= 0 && x <= worldLimit && z >= 0 && z <= worldLimit }

// ApplyLive stores a push and returns what changed for browsers.
func (rc *Receiver) ApplyLive(serverID string, up LiveUpdate, at time.Time) LiveDelta {
	players := make([]Player, 0, len(up.Players))
	for _, p := range up.Players {
		if reLiveSteam.MatchString(p.Steam64) && inWorld(p.X, p.Z) {
			if len(p.Name) > 64 {
				p.Name = p.Name[:64]
			}
			players = append(players, p)
		}
	}
	changed := make([]Vehicle, 0, len(up.Vehicles))
	for _, v := range up.Vehicles {
		if reLiveID.MatchString(v.ID) && reLiveClass.MatchString(v.Class) && inWorld(v.X, v.Z) {
			changed = append(changed, v)
		}
	}
	gone := make([]string, 0, len(up.Gone))
	for _, g := range up.Gone {
		if reLiveID.MatchString(g) {
			gone = append(gone, g)
		}
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()
	st := rc.liveStates[serverID]
	if st == nil {
		st = &liveState{vehicles: map[string]Vehicle{}}
		rc.liveStates[serverID] = st
	}
	st.at, st.interval, st.players = at, up.IntervalSec, players
	if up.Full {
		st.vehicles = make(map[string]Vehicle, len(changed))
		st.vehiclesReady = true
	}
	for _, v := range changed {
		st.vehicles[v.ID] = v
	}
	for _, g := range gone {
		delete(st.vehicles, g)
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].ID < changed[j].ID })
	return LiveDelta{ServerID: serverID, ReceivedAt: at, IntervalSec: up.IntervalSec, Players: rc.mergePlayersLocked(serverID, st),
		VehiclesReady: st.vehiclesReady, Full: up.Full, Vehicles: changed, Gone: gone}
}

// mergePlayersLocked gives the bridge's positions and vitals, plus the details only
// MetricZ reports (ping, kills, god mode…) for players it also knows about.
func (rc *Receiver) mergePlayersLocked(serverID string, st *liveState) []Player {
	details := map[string]Player{}
	for _, p := range rc.snaps[serverID].Players {
		details[p.Steam64] = p
	}
	out := make([]Player, 0, len(st.players))
	for _, p := range st.players {
		m := details[p.Steam64]
		m.Steam64, m.X, m.Y, m.Z = p.Steam64, p.X, p.Y, p.Z
		if p.Name != "" {
			m.Name = p.Name
		}
		for _, f := range []struct{ to, from **float64 }{{&m.Orientation, &p.Orientation}, {&m.Health, &p.Health}, {&m.Blood, &p.Blood}, {&m.Shock, &p.Shock}, {&m.Water, &p.Water}, {&m.Energy, &p.Energy}} {
			if *f.from != nil {
				*f.to = *f.from
			}
		}
		for _, f := range []struct{ to, from **bool }{{&m.Unconscious, &p.Unconscious}, {&m.Bleeding, &p.Bleeding}, {&m.Restrained, &p.Restrained}} {
			if *f.from != nil {
				*f.to = *f.from
			}
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// applyLiveLocked overlays a fresh live layer on the map state.
func (rc *Receiver) applyLiveLocked(st *MapState, serverID string) {
	live := rc.liveStates[serverID]
	if live == nil || time.Since(live.at) > liveStaleAfter {
		return
	}
	at := live.at
	st.Available, st.Fresh, st.ReceivedAt, st.Reason = true, true, &at, ""
	st.Source, st.IntervalSec = "bridge", live.interval
	st.Players = rc.mergePlayersLocked(serverID, live)
	if live.vehiclesReady {
		st.Vehicles = make([]Vehicle, 0, len(live.vehicles))
		for _, v := range live.vehicles {
			st.Vehicles = append(st.Vehicles, v)
		}
		sort.Slice(st.Vehicles, func(i, j int) bool { return st.Vehicles[i].ID < st.Vehicles[j].ID })
		if st.Perf != nil {
			perf := *st.Perf
			perf.Vehicles = len(st.Vehicles)
			st.Perf = &perf
		}
	}
}
