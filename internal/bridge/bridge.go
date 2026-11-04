// Package bridge is the DZ Manager side of the @DZMBridge server-side PBO
// (#16): authenticated event ingest and a pull-based command queue. The
// contract is docs/contracts/ingest-v1.md.
package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/caps"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/profiles"
	"github.com/kproios/dz-manager/internal/secret"
	"github.com/kproios/dz-manager/internal/store"
)

// Limits.
const (
	MaxEvents     = 500
	MaxBody       = 1 << 20
	DefaultTTL    = 30 * time.Second
	HeartbeatGone = 90 * time.Second
)

var (
	reType = regexp.MustCompile(`^[a-z][a-z0-9_.]{1,63}$`)
	reID   = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,80}$`)
)

// Pos is a world position (x east, y altitude, z north).
type Pos struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Event is one emitted event.
type Event struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	TS     string          `json:"ts"`
	Actor  string          `json:"actor,omitempty"`  // Steam64
	Target string          `json:"target,omitempty"` // Steam64
	Pos    *Pos            `json:"pos,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`

	ServerID string    `json:"-"`
	Source   string    `json:"-"`
	Time     time.Time `json:"-"`
}

// Envelope is the batch body.
type Envelope struct {
	V        int     `json:"v"`
	ServerID string  `json:"server_id"`
	Emitter  string  `json:"emitter"`
	Seq      int64   `json:"seq"`
	Auth     string  `json:"auth,omitempty"`
	Events   []Event `json:"events"`
}

// Handler consumes stored, de-duplicated events of the types it subscribed to.
type Handler func(ctx context.Context, ev Event)

// Info is the latest heartbeat.
type Info struct {
	ModVersion  string    `json:"mod_version"`
	GameVersion string    `json:"game_version"`
	Commands    []string  `json:"commands"`
	Emitter     string    `json:"emitter"`
	LastSeen    time.Time `json:"last_seen"`
	Dropped     int64     `json:"dropped"`
	QueueDepth  int64     `json:"queue_depth"`
}

// Bridge implements ingest + commands.
type Bridge struct {
	DB       *store.DB
	Profiles *profiles.Store
	Caps     *caps.Registry
	Audit    *audit.Log
	Log      *slog.Logger
	Now      func() time.Time
	// Publish notifies the UI about new events (type, server).
	Publish func(serverID string, types []string)

	mu       sync.RWMutex
	handlers map[string][]Handler
	info     map[string]Info
}

// Init prepares the bridge.
func (b *Bridge) Init() {
	if b.Now == nil {
		b.Now = time.Now
	}
	b.handlers, b.info = map[string][]Handler{}, map[string]Info{}
}

// Subscribe registers a handler for event types ("*" for all).
func (b *Bridge) Subscribe(h Handler, types ...string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, t := range types {
		b.handlers[t] = append(b.handlers[t], h)
	}
}

// InfoFor returns the latest heartbeat for a server.
func (b *Bridge) InfoFor(serverID string) (Info, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	i, ok := b.info[serverID]
	return i, ok
}

// Supports reports whether the server's bridge advertised a command handler.
func (b *Bridge) Supports(serverID, action string) bool {
	if !b.Caps.OK(serverID, caps.BridgeCommands) {
		return false
	}
	i, ok := b.InfoFor(serverID)
	if !ok {
		return false
	}
	for _, c := range i.Commands {
		if c == action {
			return true
		}
	}
	return false
}

// Register adds ingest routes. The token travels in the path because DayZ's
// RestContext cannot set arbitrary headers; a Bearer header is also accepted.
func (b *Bridge) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /ingest/v1/{token}/events", b.events)
	mux.HandleFunc("POST /ingest/v1/{token}/commands/poll", b.poll)
	mux.HandleFunc("POST /ingest/v1/{token}/commands/{id}/result", b.result)
}

func (b *Bridge) auth(w http.ResponseWriter, r *http.Request, bodyToken string) (string, bool) {
	tok := r.PathValue("token")
	if tok == "" || tok == "-" {
		tok = bodyToken
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok = strings.TrimPrefix(h, "Bearer ")
	}
	id, ok := b.Profiles.AuthBridge(r.Context(), tok)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "invalid server token")
		return "", false
	}
	return id, true
}

// Rejected describes an event that was not stored.
type Rejected struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// Result of an events POST.
type Result struct {
	Accepted   int        `json:"accepted"`
	Duplicates int        `json:"duplicates"`
	Rejected   []Rejected `json:"rejected"`
}

func (b *Bridge) events(w http.ResponseWriter, r *http.Request) {
	raw, err := httpx.ReadLimited(r, MaxBody)
	if err != nil {
		httpx.Error(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		httpx.Error(w, 400, "invalid JSON: "+err.Error())
		return
	}
	serverID, ok := b.auth(w, r, env.Auth)
	if !ok {
		return
	}
	if env.V != 1 {
		httpx.Error(w, 400, fmt.Sprintf("unsupported contract version %d (want 1)", env.V))
		return
	}
	if env.ServerID != "" && env.ServerID != serverID {
		httpx.Error(w, 403, "server_id does not match token")
		return
	}
	if len(env.Events) > MaxEvents {
		httpx.Error(w, 413, fmt.Sprintf("too many events (max %d)", MaxEvents))
		return
	}
	res, err := b.Ingest(r.Context(), serverID, env)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	httpx.JSON(w, 200, res)
}

// Ingest validates, stores and dispatches a batch.
func (b *Bridge) Ingest(ctx context.Context, serverID string, env Envelope) (Result, error) {
	res := Result{Rejected: []Rejected{}}
	source := env.Emitter
	if source == "" {
		source = "unknown"
	}
	if len(source) > 64 {
		source = source[:64]
	}
	now := b.Now().UTC()
	var fresh []Event
	types := map[string]bool{}
	for _, ev := range env.Events {
		if !reID.MatchString(ev.ID) {
			res.Rejected = append(res.Rejected, Rejected{ev.ID, "invalid id"})
			continue
		}
		if !reType.MatchString(ev.Type) {
			res.Rejected = append(res.Rejected, Rejected{ev.ID, "invalid type"})
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, ev.TS)
		if err != nil {
			res.Rejected = append(res.Rejected, Rejected{ev.ID, "ts must be RFC3339 UTC"})
			continue
		}
		if t.After(now.Add(10*time.Minute)) || t.Before(now.Add(-30*24*time.Hour)) {
			res.Rejected = append(res.Rejected, Rejected{ev.ID, "ts outside accepted range"})
			continue
		}
		if len(ev.Data) > 16<<10 {
			res.Rejected = append(res.Rejected, Rejected{ev.ID, "data too large"})
			continue
		}
		if len(ev.Data) == 0 || string(ev.Data) == "null" {
			ev.Data = json.RawMessage("{}")
		}
		if !json.Valid(ev.Data) || ev.Data[0] != '{' {
			res.Rejected = append(res.Rejected, Rejected{ev.ID, "data must be a JSON object"})
			continue
		}
		ev.ServerID, ev.Source, ev.Time = serverID, source, t.UTC()
		var x, y, z any
		if ev.Pos != nil {
			x, y, z = ev.Pos.X, ev.Pos.Y, ev.Pos.Z
		}
		r, err := b.DB.ExecContext(ctx, `INSERT OR IGNORE INTO events(server_id,event_id,type,ts,received_at,source,actor,target,x,y,z,data,known) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			serverID, ev.ID, ev.Type, ev.Time.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), source, ev.Actor, ev.Target, x, y, z, string(ev.Data), b.known(ev.Type))
		if err != nil {
			return res, err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			res.Duplicates++
			continue
		}
		res.Accepted++
		fresh = append(fresh, ev)
		types[ev.Type] = true
	}
	for _, ev := range fresh {
		b.dispatch(ctx, ev)
	}
	if len(fresh) > 0 && b.Publish != nil {
		ts := make([]string, 0, len(types))
		for t := range types {
			ts = append(ts, t)
		}
		b.Publish(serverID, ts)
	}
	return res, nil
}

func (b *Bridge) known(t string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.handlers[t]) > 0 || t == "heartbeat" {
		return 1
	}
	return 0
}

func (b *Bridge) dispatch(ctx context.Context, ev Event) {
	if ev.Type == "heartbeat" {
		b.heartbeat(ev)
	}
	b.mu.RLock()
	hs := append(append([]Handler(nil), b.handlers[ev.Type]...), b.handlers["*"]...)
	b.mu.RUnlock()
	for _, h := range hs {
		func() {
			defer func() {
				if v := recover(); v != nil {
					b.Log.Error("bridge handler panicked", "type", ev.Type, "panic", v)
				}
			}()
			h(ctx, ev)
		}()
	}
}

func (b *Bridge) heartbeat(ev Event) {
	var d struct {
		ModVersion  string   `json:"mod_version"`
		GameVersion string   `json:"game_version"`
		Commands    []string `json:"commands"`
		Dropped     int64    `json:"dropped"`
		Queue       int64    `json:"queue"`
	}
	_ = json.Unmarshal(ev.Data, &d)
	info := Info{ModVersion: d.ModVersion, GameVersion: d.GameVersion, Commands: d.Commands, Emitter: ev.Source, LastSeen: b.Now().UTC(), Dropped: d.Dropped, QueueDepth: d.Queue}
	b.mu.Lock()
	b.info[ev.ServerID] = info
	b.mu.Unlock()
	b.Caps.Set(ev.ServerID, caps.Bridge, caps.OK, "@DZMBridge "+d.ModVersion)
	if len(d.Commands) > 0 {
		b.Caps.Set(ev.ServerID, caps.BridgeCommands, caps.OK, "handlers: "+strings.Join(d.Commands, ", "))
	} else {
		b.Caps.Set(ev.ServerID, caps.BridgeCommands, caps.Unsupported, "bridge advertises no command handlers")
	}
}

// Watch expires commands and marks the bridge stale when heartbeats stop.
func (b *Bridge) Watch(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	prune := time.NewTicker(time.Hour)
	defer prune.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-prune.C:
			_, _ = b.DB.ExecContext(ctx, `DELETE FROM events WHERE received_at < ?`, b.Now().UTC().Add(-90*24*time.Hour).Format(time.RFC3339Nano))
			_, _ = b.DB.ExecContext(ctx, `DELETE FROM bridge_commands WHERE created_at < ?`, b.Now().UTC().Add(-30*24*time.Hour).Format(time.RFC3339Nano))
		case <-t.C:
			b.expire(ctx)
			b.mu.RLock()
			for id, i := range b.info {
				if b.Now().Sub(i.LastSeen) > HeartbeatGone {
					b.Caps.Set(id, caps.Bridge, caps.Degraded, "no heartbeat since "+i.LastSeen.Format(time.RFC3339))
					b.Caps.Set(id, caps.BridgeCommands, caps.Degraded, "bridge offline")
				}
			}
			b.mu.RUnlock()
		}
	}
}

// Command is a queued administrative command.
type Command struct {
	ID        string          `json:"id"`
	ServerID  string          `json:"server_id"`
	Action    string          `json:"action"`
	Target    string          `json:"target,omitempty"`
	Params    json.RawMessage `json:"params"`
	Status    string          `json:"status"` // queued | delivered | ok | error | expired
	CreatedAt string          `json:"created_at"`
	ExpiresAt string          `json:"expires_at"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// ErrBridgeUnavailable is returned when no live bridge can take commands.
var ErrBridgeUnavailable = errors.New("@DZMBridge is not connected or does not support this action")

// Enqueue queues a command for the server's bridge.
func (b *Bridge) Enqueue(ctx context.Context, serverID, action, target string, params any, a audit.Actor, ttl time.Duration) (string, error) {
	if !b.Supports(serverID, action) {
		return "", ErrBridgeUnavailable
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	p, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	id := secret.UUID()
	now := b.Now().UTC()
	_, err = b.DB.ExecContext(ctx, `INSERT INTO bridge_commands(id,server_id,action,target,params,status,created_at,expires_at,actor_sub,actor_name) VALUES(?,?,?,?,?, 'queued',?,?,?,?)`,
		id, serverID, action, target, string(p), now.Format(time.RFC3339Nano), now.Add(ttl).Format(time.RFC3339Nano), a.Sub, a.Name)
	return id, err
}

func (b *Bridge) expire(ctx context.Context) {
	now := b.Now().UTC().Format(time.RFC3339Nano)
	_, _ = b.DB.ExecContext(ctx, `UPDATE bridge_commands SET status='expired', error='timed out: no result from the game server before expiry' WHERE status IN ('queued','delivered') AND expires_at < ?`, now)
}

func (b *Bridge) poll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Auth string `json:"auth"`
	}
	raw, _ := httpx.ReadLimited(r, 16<<10)
	_ = json.Unmarshal(raw, &body)
	serverID, ok := b.auth(w, r, body.Auth)
	if !ok {
		return
	}
	cmds, err := b.Deliver(r.Context(), serverID, 10)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	// Slim shape for Enforce Script's JsonSerializer: only what the mod needs.
	type slim struct {
		ID      string          `json:"id"`
		Action  string          `json:"action"`
		Target  string          `json:"target"`
		Params  json.RawMessage `json:"params"`
		Expires string          `json:"expires_at"`
	}
	out := make([]slim, 0, len(cmds))
	for _, c := range cmds {
		p := c.Params
		if len(p) == 0 || string(p) == "null" {
			p = json.RawMessage("{}")
		}
		out = append(out, slim{c.ID, c.Action, c.Target, p, c.ExpiresAt})
	}
	httpx.JSON(w, 200, map[string]any{"commands": out})
}

// Deliver marks up to n queued, unexpired commands as delivered and returns them.
func (b *Bridge) Deliver(ctx context.Context, serverID string, n int) ([]Command, error) {
	b.expire(ctx)
	rows, err := b.DB.QueryContext(ctx, `SELECT id,action,target,params,created_at,expires_at FROM bridge_commands WHERE server_id=? AND status='queued' ORDER BY created_at LIMIT ?`, serverID, n)
	if err != nil {
		return nil, err
	}
	var out []Command
	for rows.Next() {
		c := Command{ServerID: serverID, Status: "delivered"}
		var p string
		if err := rows.Scan(&c.ID, &c.Action, &c.Target, &p, &c.CreatedAt, &c.ExpiresAt); err != nil {
			rows.Close()
			return nil, err
		}
		c.Params = json.RawMessage(p)
		out = append(out, c)
	}
	rows.Close()
	now := b.Now().UTC().Format(time.RFC3339Nano)
	for _, c := range out {
		_, _ = b.DB.ExecContext(ctx, `UPDATE bridge_commands SET status='delivered', delivered_at=? WHERE id=? AND status='queued'`, now, c.ID)
	}
	if out == nil {
		out = []Command{}
	}
	return out, nil
}

func (b *Bridge) result(w http.ResponseWriter, r *http.Request) {
	raw, err := httpx.ReadLimited(r, 64<<10)
	if err != nil {
		httpx.Error(w, 413, err.Error())
		return
	}
	var body struct {
		Auth  string          `json:"auth"`
		OK    bool            `json:"ok"`
		Error string          `json:"error"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		httpx.Error(w, 400, "invalid JSON")
		return
	}
	serverID, ok := b.auth(w, r, body.Auth)
	if !ok {
		return
	}
	switch err := b.Complete(r.Context(), serverID, r.PathValue("id"), body.OK, body.Error, body.Data); {
	case errors.Is(err, sql.ErrNoRows):
		httpx.Error(w, 404, "unknown command id")
	case err != nil:
		httpx.Error(w, 409, err.Error())
	default:
		httpx.JSON(w, 200, map[string]bool{"ok": true})
	}
}

// Complete records a command result. Results for expired commands are kept
// for the record but never flip the command back to success.
func (b *Bridge) Complete(ctx context.Context, serverID, id string, ok bool, errMsg string, data json.RawMessage) error {
	b.expire(ctx)
	var status string
	if err := b.DB.QueryRowContext(ctx, `SELECT status FROM bridge_commands WHERE id=? AND server_id=?`, id, serverID).Scan(&status); err != nil {
		return err
	}
	if len(data) == 0 {
		data = json.RawMessage("null")
	}
	now := b.Now().UTC().Format(time.RFC3339Nano)
	switch status {
	case "delivered":
		st := "ok"
		if !ok {
			st = "error"
		}
		_, err := b.DB.ExecContext(ctx, `UPDATE bridge_commands SET status=?, result=?, error=?, finished_at=? WHERE id=?`, st, string(data), errMsg, now, id)
		return err
	case "expired":
		_, err := b.DB.ExecContext(ctx, `UPDATE bridge_commands SET result=?, error=error || ' (late result received: ok=' || ? || ')', finished_at=? WHERE id=?`, string(data), fmt.Sprint(ok), now, id)
		return err
	default:
		return fmt.Errorf("command is %s, not awaiting a result", status)
	}
}

// Get loads a command.
func (b *Bridge) Get(ctx context.Context, id string) (*Command, error) {
	var c Command
	var p string
	var res, e sql.NullString
	err := b.DB.QueryRowContext(ctx, `SELECT id,server_id,action,target,params,status,created_at,expires_at,result,error FROM bridge_commands WHERE id=?`, id).
		Scan(&c.ID, &c.ServerID, &c.Action, &c.Target, &p, &c.Status, &c.CreatedAt, &c.ExpiresAt, &res, &e)
	if err != nil {
		return nil, err
	}
	c.Params = json.RawMessage(p)
	if res.Valid {
		c.Result = json.RawMessage(res.String)
	}
	c.Error = e.String
	return &c, nil
}

// Await waits until the command finishes or ctx ends.
func (b *Bridge) Await(ctx context.Context, id string) (*Command, error) {
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		b.expire(ctx)
		c, err := b.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if c.Status == "ok" || c.Status == "error" || c.Status == "expired" {
			return c, nil
		}
		select {
		case <-ctx.Done():
			return c, ctx.Err()
		case <-t.C:
		}
	}
}

// Recent lists recent stored events (for the Events page and debugging).
func (b *Bridge) Recent(ctx context.Context, serverID, typ string, limit int) ([]map[string]any, error) {
	q := `SELECT event_id,type,ts,source,actor,target,x,y,z,data,known FROM events WHERE server_id=?`
	args := []any{serverID}
	if typ != "" {
		q += ` AND type=?`
		args = append(args, typ)
	}
	q += ` ORDER BY ts DESC LIMIT ?`
	args = append(args, limit)
	rows, err := b.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, t, ts, src, actor, target, data string
		var x, y, z sql.NullFloat64
		var known int
		if err := rows.Scan(&id, &t, &ts, &src, &actor, &target, &x, &y, &z, &data, &known); err != nil {
			return nil, err
		}
		m := map[string]any{"id": id, "type": t, "ts": ts, "source": src, "actor": actor, "target": target, "data": json.RawMessage(data), "known": known == 1}
		if x.Valid {
			m["pos"] = Pos{x.Float64, y.Float64, z.Float64}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// RegisterAPI adds authenticated routes.
func (b *Bridge) RegisterAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/bridge", func(w http.ResponseWriter, r *http.Request) {
		p, _, err := b.Profiles.Active(r.Context())
		if err != nil {
			httpx.Error(w, 409, "no active server profile")
			return
		}
		i, ok := b.InfoFor(p.ID)
		out := map[string]any{"server_id": p.ID, "connected": ok, "bridge": b.Caps.Get(p.ID, caps.Bridge), "commands": b.Caps.Get(p.ID, caps.BridgeCommands)}
		if ok {
			out["info"] = i
		}
		httpx.JSON(w, 200, out)
	})
	mux.HandleFunc("GET /api/events/raw", func(w http.ResponseWriter, r *http.Request) {
		p, _, err := b.Profiles.Active(r.Context())
		if err != nil {
			httpx.JSON(w, 200, []any{})
			return
		}
		limit := httpx.QueryInt(r, "limit", 100)
		if limit < 1 || limit > 500 {
			limit = 100
		}
		evs, err := b.Recent(r.Context(), p.ID, r.URL.Query().Get("type"), limit)
		if err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		httpx.JSON(w, 200, evs)
	})
}
