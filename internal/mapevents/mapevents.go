// Package mapevents tracks actually-active world events (heli crashes,
// airdrops, loot boxes, custom events) and historical rare-item spawn markers
// (#6), emitted by @DZMBridge. Potential spawn points are never shown as live.
package mapevents

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/bridge"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/profiles"
	"github.com/kproios/dz-manager/internal/store"
)

// Categories.
var Categories = []string{"heli_crash", "plane_crash", "airdrop", "loot_box", "military_convoy", "police", "train", "custom", "rare_item", "tracked_item", "base_flag"}

const (
	defaultTTL   = 3 * time.Hour // fallback end when a despawn is never reported
	rareItemKeep = 6 * time.Hour
	settingsKey  = "bridge_tracking"
)

var reClass = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{1,63}$`)

// Tracking is the owner-editable configuration rendered into the bridge config.
type Tracking struct {
	Revision int64 `json:"revision"`
	// EventClasses maps an entity classname to a category (e.g. Wreck_UH1Y → heli_crash).
	EventClasses map[string]string `json:"event_classes"`
	// RareItems is the allow-list of classnames whose CE spawns are recorded.
	RareItems []string `json:"rare_items"`
}

// DefaultTracking covers vanilla heli crashes; mod events are added by the owner
// after inventorying the active mod list.
func DefaultTracking() Tracking {
	return Tracking{EventClasses: map[string]string{"Wreck_UH1Y": "heli_crash", "Wreck_Mi8_Crashed": "heli_crash"}, RareItems: []string{}}
}

// Service stores event lifecycles.
type Service struct {
	DB       *store.DB
	Profiles *profiles.Store
	Audit    *audit.Log
	Log      *slog.Logger
	Now      func() time.Time
	Publish  func()
}

// Attach subscribes to bridge events.
func (s *Service) Attach(b *bridge.Bridge) {
	if s.Now == nil {
		s.Now = time.Now
	}
	b.Subscribe(s.onStart, "event.start", "event.seen")
	b.Subscribe(s.onEnd, "event.end")
	b.Subscribe(s.onItem, "item.spawn", "item.observed")
	b.Subscribe(s.onServerStart, "server.start")
}

type startData struct {
	Key      string `json:"key"`
	Category string `json:"category"`
	Class    string `json:"class"`
	TTLS     int    `json:"ttl_s"`
}

func validCategory(c string) bool {
	for _, x := range Categories {
		if x == c && c != "rare_item" {
			return true
		}
	}
	return false
}

func (s *Service) onStart(ctx context.Context, ev bridge.Event) {
	var d startData
	_ = json.Unmarshal(ev.Data, &d)
	if d.Key == "" || ev.Pos == nil || !validCategory(d.Category) {
		s.Log.Warn("event.start ignored: missing key/pos or unknown category", "id", ev.ID)
		return
	}
	ttl := time.Duration(d.TTLS) * time.Second
	if ttl <= 0 || ttl > 24*time.Hour {
		ttl = defaultTTL
	}
	t := ev.Time.UTC().Format(time.RFC3339)
	// Out-of-order safety: if an end for this key already arrived, keep it ended.
	// A sighting *after* the recorded end proves the object still exists (for
	// example an end inferred from a restart while the census was still running),
	// so the marker comes back instead of staying hidden for the whole session.
	const revive = `(map_events.status='expired' OR (map_events.status='ended' AND map_events.ended_at IS NOT NULL AND excluded.last_seen_at > map_events.ended_at))`
	_, err := s.DB.ExecContext(ctx, `INSERT INTO map_events(server_id,key,category,class,x,y,z,started_at,last_seen_at,expires_at,status,source,data) VALUES(?,?,?,?,?,?,?,?,?,?, 'active',?,?)
		ON CONFLICT(server_id,key) DO UPDATE SET x=excluded.x, y=excluded.y, z=excluded.z, last_seen_at=excluded.last_seen_at,
		started_at=MIN(map_events.started_at, excluded.started_at), category=excluded.category, class=excluded.class, data=excluded.data,
		expires_at=CASE WHEN map_events.status='ended' AND NOT `+revive+` THEN map_events.expires_at ELSE excluded.expires_at END,
		ended_at=CASE WHEN `+revive+` THEN NULL ELSE map_events.ended_at END,
		status=CASE WHEN `+revive+` THEN 'active' ELSE map_events.status END
		WHERE map_events.x IS NULL OR excluded.last_seen_at >= map_events.last_seen_at`,
		ev.ServerID, d.Key, d.Category, trunc(d.Class), ev.Pos.X, ev.Pos.Y, ev.Pos.Z, t, t, ev.Time.Add(ttl).UTC().Format(time.RFC3339), ev.Source, string(ev.Data))
	if err != nil {
		s.Log.Error("map event insert", "err", err)
	}
	s.pub()
}

func (s *Service) onEnd(ctx context.Context, ev bridge.Event) {
	var d startData
	_ = json.Unmarshal(ev.Data, &d)
	if d.Key == "" {
		return
	}
	t := ev.Time.UTC().Format(time.RFC3339)
	res, _ := s.DB.ExecContext(ctx, `UPDATE map_events SET status='ended', ended_at=? WHERE server_id=? AND key=? AND status='active'`, t, ev.ServerID, d.Key)
	if n, _ := res.RowsAffected(); n == 0 {
		// End before start (out of order): record a placeholder so the late start stays ended.
		_, _ = s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO map_events(server_id,key,category,class,started_at,last_seen_at,expires_at,ended_at,status,source,data) VALUES(?,?, 'custom','',?,?,?,?, 'ended',?, '{}')`,
			ev.ServerID, d.Key, t, t, t, t, ev.Source)
	}
	s.pub()
}

type itemData struct {
	Class string `json:"class"`
	Key   string `json:"key"`
}

func (s *Service) onItem(ctx context.Context, ev bridge.Event) {
	var d itemData
	_ = json.Unmarshal(ev.Data, &d)
	if !reClass.MatchString(d.Class) || ev.Pos == nil {
		return
	}
	if !s.allowed(ctx, d.Class) {
		return // only owner-configured classnames are ever recorded
	}
	key := d.Key
	if key == "" {
		key = "item:" + ev.ID
	}
	t := ev.Time.UTC().Format(time.RFC3339)
	_, _ = s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO map_events(server_id,key,category,class,x,y,z,started_at,last_seen_at,expires_at,status,source,data) VALUES(?,?, 'rare_item',?,?,?,?,?,?,?, 'historical',?,?)`,
		ev.ServerID, key, d.Class, ev.Pos.X, ev.Pos.Y, ev.Pos.Z, t, t, ev.Time.Add(rareItemKeep).UTC().Format(time.RFC3339), ev.Source, string(ev.Data))
	s.pub()
}

func (s *Service) onServerStart(ctx context.Context, ev bridge.Event) {
	t := ev.Time.UTC().Format(time.RFC3339)
	_, _ = s.DB.ExecContext(ctx, `UPDATE map_events SET status='ended', ended_at=? WHERE server_id=? AND status='active' AND started_at < ?`, t, ev.ServerID, t)
	s.pub()
}

// Sweep expires events whose end was never reported.
func (s *Service) Sweep(ctx context.Context) {
	now := s.Now().UTC().Format(time.RFC3339)
	_, _ = s.DB.ExecContext(ctx, `UPDATE map_events SET status='expired', ended_at=expires_at WHERE status='active' AND expires_at < ?`, now)
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM map_events WHERE COALESCE(ended_at, expires_at) < ?`, s.Now().UTC().Add(-30*24*time.Hour).Format(time.RFC3339))
}

// Run sweeps every minute.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.Sweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) pub() {
	if s.Publish != nil {
		s.Publish()
	}
}

func trunc(v string) string {
	if len(v) > 64 {
		return v[:64]
	}
	return v
}

// LoadTracking returns the configured tracking lists.
func (s *Service) LoadTracking(ctx context.Context) Tracking {
	var raw string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, settingsKey).Scan(&raw); err == nil {
		var t Tracking
		if json.Unmarshal([]byte(raw), &t) == nil {
			if t.EventClasses == nil {
				t.EventClasses = map[string]string{}
			}
			if t.RareItems == nil {
				t.RareItems = []string{}
			}
			return t
		}
	}
	return DefaultTracking()
}

func (s *Service) allowed(ctx context.Context, class string) bool {
	for _, c := range s.LoadTracking(ctx).RareItems {
		if strings.EqualFold(c, class) {
			return true
		}
	}
	return false
}

// BridgeConfig returns the extra keys merged into the generated bridge config.
func (s *Service) BridgeConfig(ctx context.Context) map[string]any {
	t := s.LoadTracking(ctx)
	return map[string]any{"event_classes": t.EventClasses, "rare_items": t.RareItems, "revision": t.Revision}
}

// Marker is one map marker.
type Marker struct {
	ID          int64   `json:"id"`
	Key         string  `json:"key"`
	Category    string  `json:"category"`
	Class       string  `json:"class"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Z           float64 `json:"z"`
	StartedAt   string  `json:"started_at"`
	EndedAt     *string `json:"ended_at,omitempty"`
	Status      string  `json:"status"` // active | ended | expired | historical
	Source      string  `json:"source"`
	Observation string  `json:"observation,omitempty"`
	Name        string  `json:"name,omitempty"`
	Kind        string  `json:"kind,omitempty"`
	Group       string  `json:"group,omitempty"`
	Radius      float64 `json:"radius,omitempty"`
}

// Register adds API routes.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/map/events", s.list)
	mux.HandleFunc("GET /api/settings/tracking", func(w http.ResponseWriter, r *http.Request) { httpx.JSON(w, 200, s.LoadTracking(r.Context())) })
	mux.HandleFunc("PUT /api/settings/tracking", s.putTracking)
}

func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	p, _, err := s.Profiles.Active(r.Context())
	if err != nil {
		if r.URL.Query().Get("paged") == "true" {
			httpx.JSON(w, 200, map[string]any{"items": []Marker{}, "next_before": 0})
			return
		}
		httpx.JSON(w, 200, []Marker{})
		return
	}
	s.Sweep(r.Context())
	since := s.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	now := s.Now().UTC().Format(time.RFC3339)
	paged := r.URL.Query().Get("paged") == "true"
	limit, before, query := 1000, 0, ""
	if paged {
		limit = httpx.QueryInt(r, "limit", 25)
		if limit < 1 || limit > 100 {
			limit = 25
		}
		before = httpx.QueryInt(r, "before", 0)
		query = strings.TrimSpace(r.URL.Query().Get("q"))
		if len(query) > 200 {
			query = query[:200]
		}
	}
	pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query) + "%"
	// The live map (unpaged) lists every active marker before any history, so a
	// long-running event can never be pushed out of the list by newer history.
	order, cursor := "id DESC", "id<?"
	if !paged {
		order = "(status='active') DESC, id DESC"
	} else if r.URL.Query().Get("order") == "asc" {
		order, cursor = "id ASC", "id>?"
	}
	// Optional category filter (comma list of known categories only).
	cats := []string{}
	for _, c := range strings.Split(r.URL.Query().Get("cat"), ",") {
		for _, known := range Categories {
			if c == known {
				cats = append(cats, c)
			}
		}
	}
	catSQL := "1"
	args := []any{p.ID, since, now, before, before, query, pattern}
	if len(cats) > 0 {
		catSQL = "category IN (?" + strings.Repeat(",?", len(cats)-1) + ")"
		for _, c := range cats {
			args = append(args, c)
		}
	}
	args = append(args, limit+1)
	rows, err := s.DB.QueryContext(r.Context(), `SELECT id,key,category,class,x,y,z,started_at,ended_at,status,source,COALESCE(json_extract(data,'$.observation'),'item.spawn'),COALESCE(json_extract(data,'$.name'),''),COALESCE(json_extract(data,'$.kind'),''),COALESCE(json_extract(data,'$.group'),''),COALESCE(json_extract(data,'$.radius'),0) FROM map_events
		WHERE server_id=? AND x IS NOT NULL AND (status='active' OR (status IN ('ended','expired') AND ended_at >= ?) OR (status='historical' AND expires_at >= ?))
		AND (?=0 OR `+cursor+`) AND (?='' OR (class||' '||replace(category,'_',' ')||' '||status) LIKE ? ESCAPE '\') AND `+catSQL+`
		ORDER BY `+order+` LIMIT ?`, args...)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []Marker{}
	for rows.Next() {
		var m Marker
		var ended sql.NullString
		if err := rows.Scan(&m.ID, &m.Key, &m.Category, &m.Class, &m.X, &m.Y, &m.Z, &m.StartedAt, &ended, &m.Status, &m.Source, &m.Observation, &m.Name, &m.Kind, &m.Group, &m.Radius); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		if ended.Valid {
			m.EndedAt = &ended.String
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	var next int64
	if len(out) > limit {
		out = out[:limit]
		next = out[limit-1].ID
	}
	if paged {
		httpx.JSON(w, 200, map[string]any{"items": out, "next_before": next})
		return
	}
	httpx.JSON(w, 200, out)
}

func (s *Service) putTracking(w http.ResponseWriter, r *http.Request) {
	var t Tracking
	if err := httpx.Decode(r, 32<<10, &t); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	if len(t.EventClasses) > 200 || len(t.RareItems) > 200 {
		httpx.Error(w, 400, "too many entries (max 200 each)")
		return
	}
	for c, cat := range t.EventClasses {
		if !reClass.MatchString(c) || !validCategory(cat) {
			httpx.Error(w, 400, "invalid event class mapping "+c+" → "+cat)
			return
		}
	}
	for _, c := range t.RareItems {
		if !reClass.MatchString(c) {
			httpx.Error(w, 400, "invalid classname "+c)
			return
		}
	}
	if t.RareItems == nil {
		t.RareItems = []string{}
	}
	t.Revision = s.LoadTracking(r.Context()).Revision + 1
	b, _ := json.Marshal(t)
	if _, err := s.DB.ExecContext(r.Context(), `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, settingsKey, string(b)); err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	a := audit.System
	if u := auth.FromContext(r.Context()); u != nil {
		a = u.Actor()
	}
	_ = s.Audit.Write(r.Context(), a, "settings.tracking_update", "", t, "ok", "sent on the bridge's next command poll (requires an up-to-date bridge)")
	httpx.JSON(w, 200, t)
}
