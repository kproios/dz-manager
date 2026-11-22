package metricz

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kproios/dz-manager/internal/caps"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/profiles"
	"github.com/kproios/dz-manager/internal/store"
)

const (
	maxBody   = 4 << 20
	txnTTL    = 2 * time.Minute
	maxChunks = 256
)

// Receiver handles /ingest/metricz/api/v1/... and keeps the latest snapshot per server.
type Receiver struct {
	DB       *store.DB
	Profiles *profiles.Store
	Caps     *caps.Registry
	Log      *slog.Logger
	// Publish is called with each new snapshot.
	Publish func(Snapshot)
	// OnPlayers is called with identities seen in each snapshot (feeds player history).
	OnPlayers func(serverID string, ps []Player, at time.Time)
	// PublishLive is called with each bridge position push (see live.go).
	PublishLive func(LiveDelta)
	// StaleAfter marks the layer stale when no push arrived (default 45 s = 3× MetricZ's 15 s).
	StaleAfter time.Duration

	mu          sync.Mutex
	snaps       map[string]Snapshot
	txns        map[string]*txn
	liveStates  map[string]*liveState
	bridgePerfs map[string]bridgePerf
}

type txn struct {
	server  string
	chunks  map[int]string
	started time.Time
}

// Register adds the ingest routes.
func (rc *Receiver) Register(mux *http.ServeMux) {
	rc.snaps, rc.txns, rc.liveStates, rc.bridgePerfs = map[string]Snapshot{}, map[string]*txn{}, map[string]*liveState{}, map[string]bridgePerf{}
	if rc.StaleAfter == 0 {
		rc.StaleAfter = 45 * time.Second
	}
	mux.HandleFunc("POST /ingest/v1/{token}/live", rc.live)
	mux.HandleFunc("POST /ingest/metricz/api/v1/ingest/{instance}", rc.single)
	mux.HandleFunc("POST /ingest/metricz/api/v1/ingest/{instance}/{txn}/{seq}", rc.chunk)
	mux.HandleFunc("POST /ingest/metricz/api/v1/commit/{instance}/{txn}", rc.commit)
}

// RegisterAPI adds the authenticated read routes.
func (rc *Receiver) RegisterAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/map/state", rc.mapState)
	mux.HandleFunc("GET /api/perf", rc.perf)
}

func (rc *Receiver) auth(w http.ResponseWriter, r *http.Request) (string, bool) {
	user, pass, ok := r.BasicAuth()
	if ok {
		if id, good := rc.Profiles.AuthMetricZ(r.Context(), user, pass); good {
			return id, true
		}
	}
	w.Header().Set("WWW-Authenticate", `Basic realm="dzm-ingest"`)
	httpx.Error(w, http.StatusUnauthorized, "invalid MetricZ credentials")
	return "", false
}

func (rc *Receiver) single(w http.ResponseWriter, r *http.Request) {
	id, ok := rc.auth(w, r)
	if !ok {
		return
	}
	body, err := httpx.ReadLimited(r, maxBody)
	if err != nil {
		httpx.Error(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	if err := rc.ingest(r.Context(), id, body); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("OK"))
}

func (rc *Receiver) chunk(w http.ResponseWriter, r *http.Request) {
	id, ok := rc.auth(w, r)
	if !ok {
		return
	}
	seq, err := strconv.Atoi(r.PathValue("seq"))
	if err != nil || seq < 0 || seq >= maxChunks {
		httpx.Error(w, 400, "invalid chunk sequence")
		return
	}
	body, err := httpx.ReadLimited(r, maxBody)
	if err != nil {
		httpx.Error(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	text, err := Decode(body)
	if err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	key := id + "/" + r.PathValue("txn")
	rc.mu.Lock()
	rc.gcLocked()
	t := rc.txns[key]
	if t == nil {
		if len(rc.txns) > 32 {
			rc.mu.Unlock()
			httpx.Error(w, 429, "too many open transactions")
			return
		}
		t = &txn{server: id, chunks: map[int]string{}, started: time.Now()}
		rc.txns[key] = t
	}
	t.chunks[seq] = text
	rc.mu.Unlock()
	_, _ = w.Write([]byte("OK"))
}

func (rc *Receiver) commit(w http.ResponseWriter, r *http.Request) {
	id, ok := rc.auth(w, r)
	if !ok {
		return
	}
	key := id + "/" + r.PathValue("txn")
	rc.mu.Lock()
	t := rc.txns[key]
	delete(rc.txns, key)
	rc.mu.Unlock()
	if t == nil {
		httpx.Error(w, 404, "unknown or expired transaction")
		return
	}
	seqs := make([]int, 0, len(t.chunks))
	for s := range t.chunks {
		seqs = append(seqs, s)
	}
	sort.Ints(seqs)
	for i, s := range seqs {
		if s != i {
			httpx.Error(w, 400, "missing chunk "+strconv.Itoa(i))
			return
		}
	}
	var sb strings.Builder
	for _, s := range seqs {
		sb.WriteString(t.chunks[s])
		sb.WriteByte('\n')
	}
	if err := rc.ingestText(r.Context(), id, sb.String()); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	_, _ = w.Write([]byte("OK"))
}

func (rc *Receiver) gcLocked() {
	for k, t := range rc.txns {
		if time.Since(t.started) > txnTTL {
			delete(rc.txns, k)
		}
	}
}

func (rc *Receiver) ingest(ctx context.Context, serverID string, body []byte) error {
	text, err := Decode(body)
	if err != nil {
		return err
	}
	return rc.ingestText(ctx, serverID, text)
}

func (rc *Receiver) ingestText(ctx context.Context, serverID, text string) error {
	samples, bad := Parse(text)
	if len(samples) == 0 {
		return errEmpty
	}
	snap := Build(samples)
	snap.ServerID, snap.BadLines, snap.ReceivedAt = serverID, bad, time.Now().UTC()
	rc.mu.Lock()
	previous, hadPrevious := rc.snaps[serverID]
	rc.snaps[serverID] = snap
	rc.mu.Unlock()
	if !hadPrevious || len(previous.Players) != len(snap.Players) || len(previous.Vehicles) != len(snap.Vehicles) {
		rc.Log.Info("MetricZ map entity counts", "players", len(snap.Players), "vehicles", len(snap.Vehicles), "samples", snap.Samples, "bad_lines", snap.BadLines)
	}
	rc.Caps.Set(serverID, caps.MetricZ, caps.OK, "")
	if err := rc.recordPerf(ctx, snap); err != nil {
		rc.Log.Warn("metricz: perf write failed", "err", err)
	}
	if rc.OnPlayers != nil {
		rc.OnPlayers(serverID, snap.Players, snap.ReceivedAt)
	}
	if rc.Publish != nil {
		rc.Publish(snap)
	}
	return nil
}

type ingestError string

func (e ingestError) Error() string { return string(e) }

const errEmpty = ingestError("payload contained no valid samples")

// Latest returns the latest snapshot for a server and whether it is fresh.
func (rc *Receiver) Latest(serverID string) (Snapshot, bool, bool) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	s, ok := rc.snaps[serverID]
	return s, ok, ok && time.Since(s.ReceivedAt) <= rc.StaleAfter
}

// Watch marks the capability stale when pushes stop.
func (rc *Receiver) Watch(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	prune := time.NewTicker(time.Hour)
	defer prune.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-prune.C:
			_, _ = rc.DB.ExecContext(ctx, `DELETE FROM metricz_perf WHERE minute < ?`, time.Now().UTC().Add(-30*24*time.Hour).Format("2006-01-02T15:04"))
		case <-t.C:
			rc.mu.Lock()
			for id, s := range rc.snaps {
				if time.Since(s.ReceivedAt) > rc.StaleAfter {
					rc.Caps.Set(id, caps.MetricZ, caps.Degraded, "no MetricZ push since "+s.ReceivedAt.Format(time.RFC3339))
				}
			}
			rc.mu.Unlock()
		}
	}
}

func (rc *Receiver) recordPerf(ctx context.Context, s Snapshot) error {
	rc.mu.Lock()
	_, bridge := rc.bridgePerfLocked(s.ServerID)
	rc.mu.Unlock()
	if bridge {
		return nil // @DZMBridge's report owns the history while it arrives
	}
	return rc.writePerf(ctx, s.ServerID, s.ReceivedAt.Format("2006-01-02T15:04"), s.Perf)
}

// writePerf stores one per-minute performance bucket.
func (rc *Receiver) writePerf(ctx context.Context, serverID, minute string, p Perf) error {
	val := func(p *float64) any {
		if p == nil {
			return nil
		}
		return *p
	}
	fps := p.FPSAvg
	if fps == nil {
		fps = p.FPS
	}
	_, err := rc.DB.ExecContext(ctx, `INSERT INTO metricz_perf(server_id,minute,fps,fps_min,players,infected,animals,items,vehicles) VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(server_id,minute) DO UPDATE SET fps=excluded.fps, fps_min=MIN(COALESCE(metricz_perf.fps_min, excluded.fps_min), COALESCE(excluded.fps_min, metricz_perf.fps_min)),
		players=excluded.players, infected=excluded.infected, animals=excluded.animals, items=excluded.items, vehicles=excluded.vehicles`,
		serverID, minute, val(fps), val(p.FPSMin), val(p.Players), val(p.Infected), val(p.Animals), val(p.Items), p.Vehicles)
	return err
}

// MapState is the API response for the live map.
type MapState struct {
	ServerID   string     `json:"server_id"`
	Available  bool       `json:"available"`
	Fresh      bool       `json:"fresh"`
	ReceivedAt *time.Time `json:"received_at,omitempty"`
	World      string     `json:"world,omitempty"`
	Players    []Player   `json:"players"`
	Vehicles   []Vehicle  `json:"vehicles"`
	Perf       *Perf      `json:"perf,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	// Source says who is providing positions right now: "bridge" (live, every
	// IntervalSec seconds) or "metricz" (the slower fallback).
	Source      string  `json:"source,omitempty"`
	IntervalSec float64 `json:"interval_s,omitempty"`
	// PerfSource says who reports server performance: "bridge" or "metricz".
	PerfSource string `json:"perf_source,omitempty"`
}

// State builds the map state for the active profile.
func (rc *Receiver) State(ctx context.Context) MapState {
	p, _, err := rc.Profiles.Active(ctx)
	if err != nil {
		return MapState{Players: []Player{}, Vehicles: []Vehicle{}, Reason: "no active server profile"}
	}
	snap, ok, fresh := rc.Latest(p.ID)
	st := MapState{ServerID: p.ID, Players: []Player{}, Vehicles: []Vehicle{}, Reason: "no position data received yet: install @DZMBridge with the generated config (Settings → Server → Connect…)"}
	if ok {
		st = MapState{ServerID: p.ID, Available: true, Fresh: fresh, ReceivedAt: &snap.ReceivedAt, World: snap.World, Players: snap.Players, Vehicles: snap.Vehicles, Perf: &snap.Perf, Source: "metricz"}
		if !fresh {
			st.Reason = "stale: last MetricZ push " + snap.ReceivedAt.Format(time.RFC3339)
		}
	}
	rc.mu.Lock()
	rc.applyLiveLocked(&st, p.ID)
	if bp, ok := rc.bridgePerfLocked(p.ID); ok {
		st.Perf, st.PerfSource = &bp, "bridge"
	} else if st.Perf != nil {
		st.PerfSource = "metricz"
	}
	rc.mu.Unlock()
	return st
}

func (rc *Receiver) mapState(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, 200, rc.State(r.Context()))
}

// PerfPoint is one minute bucket.
type PerfPoint struct {
	Minute   string   `json:"minute"`
	FPS      *float64 `json:"fps"`
	FPSMin   *float64 `json:"fps_min"`
	Players  *float64 `json:"players"`
	Infected *float64 `json:"infected"`
	Animals  *float64 `json:"animals"`
	Items    *float64 `json:"items"`
	Vehicles *float64 `json:"vehicles"`
}

func (rc *Receiver) perf(w http.ResponseWriter, r *http.Request) {
	p, _, err := rc.Profiles.Active(r.Context())
	if err != nil {
		httpx.JSON(w, 200, []PerfPoint{})
		return
	}
	hours := httpx.QueryInt(r, "hours", 24)
	if hours < 1 || hours > 24*30 {
		hours = 24
	}
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour).Format("2006-01-02T15:04")
	rows, err := rc.DB.QueryContext(r.Context(), `SELECT minute,fps,fps_min,players,infected,animals,items,vehicles FROM metricz_perf WHERE server_id=? AND minute>=? ORDER BY minute`, p.ID, since)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []PerfPoint{}
	for rows.Next() {
		var pt PerfPoint
		var vals [7]sql.NullFloat64
		if err := rows.Scan(&pt.Minute, &vals[0], &vals[1], &vals[2], &vals[3], &vals[4], &vals[5], &vals[6]); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		ptr := func(n sql.NullFloat64) *float64 {
			if !n.Valid {
				return nil
			}
			return &n.Float64
		}
		pt.FPS, pt.FPSMin, pt.Players, pt.Infected, pt.Animals, pt.Items, pt.Vehicles = ptr(vals[0]), ptr(vals[1]), ptr(vals[2]), ptr(vals[3]), ptr(vals[4]), ptr(vals[5]), ptr(vals[6])
		out = append(out, pt)
	}
	httpx.JSON(w, 200, out)
}
