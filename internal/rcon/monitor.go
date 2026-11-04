// Package rcon implements the RCON + A2S connector (#15): real status, roster
// and RCON-native actions (broadcast, private message, kick, lock/unlock).
package rcon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/woozymasta/a2s/pkg/a2s"
	"github.com/woozymasta/bercon"
	"github.com/woozymasta/bercon/parser/rv"

	"github.com/kproios/dz-manager/internal/caps"
	"github.com/kproios/dz-manager/internal/profiles"
)

// Link states shown in the UI.
const (
	StateOK            = "ok"
	StateStale         = "stale"
	StateUnreachable   = "unreachable"
	StateAuthFailed    = "auth_failed"
	StateMisconfigured = "misconfigured"
	StateUnconfigured  = "unconfigured"
)

// Conn is the subset of *bercon.Client we use (fakeable in tests).
type Conn interface {
	Command(ctx context.Context, cmd string) ([]byte, error)
	Messages() <-chan bercon.Message
	Done() <-chan struct{}
	Close() error
}

// Dialer opens an authenticated RCON connection.
type Dialer func(ctx context.Context, addr, password string) (Conn, error)

// QueryInfo is the A2S subset we display.
type QueryInfo struct {
	Name       string `json:"name"`
	Map        string `json:"map"`
	Players    int    `json:"players"`
	MaxPlayers int    `json:"max_players"`
	Version    string `json:"version"`
	LatencyMS  int64  `json:"latency_ms"`
}

// Querier performs an A2S_INFO query.
type Querier func(ctx context.Context, addr string) (*QueryInfo, error)

// DefaultDialer uses github.com/woozymasta/bercon (MIT).
func DefaultDialer(ctx context.Context, addr, password string) (Conn, error) {
	o := bercon.DefaultOptions()
	o.LoginTimeout = 5 * time.Second
	o.LoginAttempts = 2
	o.ResponseTimeout = 5 * time.Second
	o.KeepAliveInterval = 30 * time.Second
	o.DropOldestMessages = true
	return bercon.Dial(ctx, addr, password, &o)
}

// DefaultQuerier uses github.com/woozymasta/a2s (MIT).
func DefaultQuerier(ctx context.Context, addr string) (*QueryInfo, error) {
	host, ps, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, _ := strconv.Atoi(ps)
	c, err := a2s.New(host, port, a2s.WithTimeout(4*time.Second))
	if err != nil {
		return nil, err
	}
	defer c.Close()
	start := time.Now()
	info, err := c.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	return &QueryInfo{Name: info.Name, Map: info.Map, Players: int(info.Players), MaxPlayers: int(info.MaxPlayers), Version: info.Version, LatencyMS: time.Since(start).Milliseconds()}, nil
}

// Player is one roster row. IP is kept server-side and only shown on request.
type Player struct {
	Number   int    `json:"number"`
	Name     string `json:"name"`
	BEGUID   string `json:"be_guid"`
	Verified bool   `json:"verified"`
	Lobby    bool   `json:"lobby"`
	Ping     int    `json:"ping"`
	IP       string `json:"-"`
}

// Link is the state of one transport.
type Link struct {
	State  string     `json:"state"`
	Error  string     `json:"error,omitempty"`
	LastOK *time.Time `json:"last_ok,omitempty"`
}

// Status is the published server status.
type Status struct {
	ServerID  string     `json:"server_id"`
	Query     Link       `json:"query"`
	Info      *QueryInfo `json:"info,omitempty"`
	RCON      Link       `json:"rcon"`
	Players   []Player   `json:"players"`
	Locked    *bool      `json:"locked,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Online reports whether either transport currently reaches the server.
func (s Status) Online() bool { return s.Query.State == StateOK || s.RCON.State == StateOK }

// Monitor polls the active profile. Exactly one Monitor runs per process.
type Monitor struct {
	Profiles *profiles.Store
	Caps     *caps.Registry
	Log      *slog.Logger
	Dial     Dialer
	Query    Querier
	Interval time.Duration
	// Publish is called with every new Status (SSE fan-out).
	Publish func(Status)
	// OnMessage receives raw RCON server messages (connect/disconnect/chat lines).
	OnMessage func(serverID, text string)

	mu       sync.Mutex
	status   Status
	conn     Conn
	connKey  string
	failures int
	reload   chan struct{}
	cmdMu    sync.Mutex
}

// Reload asks the monitor to re-read the active profile now.
func (m *Monitor) Reload() {
	if m.reload == nil {
		return
	}
	select {
	case m.reload <- struct{}{}:
	default:
	}
}

// Status returns the latest status.
func (m *Monitor) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status
	s.Players = append([]Player(nil), m.status.Players...)
	return s
}

// Init must be called before Run/Reload.
func (m *Monitor) Init() {
	if m.Dial == nil {
		m.Dial = DefaultDialer
	}
	if m.Query == nil {
		m.Query = DefaultQuerier
	}
	if m.Interval == 0 {
		m.Interval = 10 * time.Second
	}
	m.reload = make(chan struct{}, 1)
	m.status = Status{Query: Link{State: StateUnconfigured}, RCON: Link{State: StateUnconfigured}, Players: []Player{}}
}

// Run polls until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	defer m.closeConn()
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.reload:
			m.closeConn()
		case <-t.C:
		}
		m.Poll(ctx)
		// Back off when the server is unreachable (max 6× interval).
		wait := m.Interval
		if f := m.failures; f > 0 {
			wait = m.Interval * time.Duration(min(1<<min(f, 3), 6))
		}
		t.Reset(wait)
	}
}

// Poll performs one query+RCON cycle.
func (m *Monitor) Poll(ctx context.Context) {
	p, sec, err := m.Profiles.Active(ctx)
	if err != nil {
		m.set(Status{Query: Link{State: StateUnconfigured, Error: "no active server profile"}, RCON: Link{State: StateUnconfigured, Error: "no active server profile"}, Players: []Player{}})
		return
	}
	prev := m.Status()
	if prev.ServerID != p.ID {
		prev = Status{ServerID: p.ID}
	}
	next := Status{ServerID: p.ID, Players: prev.Players, Info: prev.Info, Locked: prev.Locked}
	now := time.Now().UTC()

	// A2S (may be cached by the provider: never equate it with joinability).
	if p.GameHost == "" && p.QueryHost == "" {
		next.Query = Link{State: StateMisconfigured, Error: "no game/query host set"}
	} else {
		qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		info, err := m.Query(qctx, p.QueryAddr())
		cancel()
		if err != nil {
			next.Query = staleOr(prev.Query, StateUnreachable, err)
		} else {
			next.Query, next.Info = Link{State: StateOK, LastOK: &now}, info
		}
	}

	// RCON.
	switch {
	case sec == nil:
		next.RCON = Link{State: StateMisconfigured, Error: "stored secrets cannot be decrypted (DZM_SECRET_KEY changed?)"}
	case !p.HasRconPassword:
		next.RCON = Link{State: StateUnconfigured, Error: "no RCON password set"}
	case p.GameHost == "" && p.RconHost == "":
		next.RCON = Link{State: StateMisconfigured, Error: "no RCON host set"}
	default:
		players, err := m.players(ctx, p.RconAddr(), sec.RconPassword, p.ID)
		if err != nil {
			st := StateUnreachable
			if errors.Is(err, bercon.ErrLoginFailed) {
				st = StateAuthFailed
			}
			next.RCON = staleOr(prev.RCON, st, err)
			if st == StateAuthFailed {
				next.RCON = Link{State: StateAuthFailed, Error: "RCON password rejected", LastOK: prev.RCON.LastOK}
			}
		} else {
			next.RCON, next.Players = Link{State: StateOK, LastOK: &now}, players
		}
	}
	if next.RCON.State != StateOK && next.RCON.State != StateStale {
		next.Players = []Player{}
	}
	if next.Online() {
		m.failures = 0
	} else {
		m.failures++
	}
	m.set(next)
}

// staleOr keeps a recently-ok link as "stale" for one missed cycle before declaring the failure.
func staleOr(prev Link, failState string, err error) Link {
	if prev.State == StateOK && prev.LastOK != nil && time.Since(*prev.LastOK) < 45*time.Second {
		return Link{State: StateStale, Error: err.Error(), LastOK: prev.LastOK}
	}
	return Link{State: failState, Error: err.Error(), LastOK: prev.LastOK}
}

func (m *Monitor) set(s Status) {
	s.UpdatedAt = time.Now().UTC()
	m.mu.Lock()
	m.status = s
	m.mu.Unlock()
	if s.ServerID != "" {
		m.Caps.Set(s.ServerID, caps.A2S, capState(s.Query), s.Query.Error)
		m.Caps.Set(s.ServerID, caps.RCON, capState(s.RCON), s.RCON.Error)
	}
	if m.Publish != nil {
		m.Publish(s)
	}
}

func capState(l Link) caps.State {
	switch l.State {
	case StateOK:
		return caps.OK
	case StateStale:
		return caps.Degraded
	case StateUnconfigured:
		return caps.Unconfigured
	case StateMisconfigured, StateAuthFailed:
		return caps.Degraded
	default:
		return caps.Degraded
	}
}

// conn returns a live connection, dialing if needed.
func (m *Monitor) connFor(ctx context.Context, addr, pw string) (Conn, error) {
	key := addr + "\x00" + pw
	m.mu.Lock()
	c := m.conn
	if c != nil && m.connKey == key {
		select {
		case <-c.Done():
			c = nil
		default:
			m.mu.Unlock()
			return c, nil
		}
	}
	m.mu.Unlock()
	m.closeConn()
	dctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	nc, err := m.Dial(dctx, addr, pw)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.conn, m.connKey = nc, key
	m.mu.Unlock()
	return nc, nil
}

func (m *Monitor) closeConn() {
	m.mu.Lock()
	c := m.conn
	m.conn, m.connKey = nil, ""
	m.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

func (m *Monitor) players(ctx context.Context, addr, pw, serverID string) ([]Player, error) {
	out, err := m.command(ctx, addr, pw, serverID, "players")
	if err != nil {
		return nil, err
	}
	rows, perr := rv.ParsePlayers(out)
	if perr != nil && len(rows) == 0 && len(out) > 0 {
		return nil, fmt.Errorf("cannot parse players response: %w", perr)
	}
	ps := make([]Player, 0, len(rows))
	for _, r := range rows {
		ip := r.AddressText
		if r.AddressValid {
			ip = r.Address.Addr().String()
		}
		ps = append(ps, Player{Number: r.ID, Name: r.Name, BEGUID: r.GUID, Verified: r.Verified, Lobby: r.Lobby, Ping: r.Ping, IP: ip})
	}
	return ps, nil
}

// command runs one RCON command, draining server messages first.
func (m *Monitor) command(ctx context.Context, addr, pw, serverID, cmd string) ([]byte, error) {
	m.cmdMu.Lock()
	defer m.cmdMu.Unlock()
	c, err := m.connFor(ctx, addr, pw)
	if err != nil {
		return nil, err
	}
	m.drain(c, serverID)
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := c.Command(cctx, cmd)
	if err != nil {
		m.closeConn()
		return nil, err
	}
	return out, nil
}

func (m *Monitor) drain(c Conn, serverID string) {
	for {
		select {
		case msg, ok := <-c.Messages():
			if !ok {
				return
			}
			if m.OnMessage != nil {
				m.OnMessage(serverID, string(msg.Data))
			}
		default:
			return
		}
	}
}

// Exec runs an RCON command against the active profile (used by Actions).
func (m *Monitor) Exec(ctx context.Context, cmd string) (serverID string, out []byte, err error) {
	p, sec, err := m.Profiles.Active(ctx)
	if err != nil {
		return "", nil, errors.New("no active server profile")
	}
	if sec == nil || !p.HasRconPassword {
		return p.ID, nil, errors.New("RCON is not configured for the active server")
	}
	out, err = m.command(ctx, p.RconAddr(), sec.RconPassword, p.ID, cmd)
	return p.ID, out, err
}

// FreshPlayers re-reads the roster from RCON (used to resolve targets at execution time).
func (m *Monitor) FreshPlayers(ctx context.Context) ([]Player, error) {
	p, sec, err := m.Profiles.Active(ctx)
	if err != nil {
		return nil, errors.New("no active server profile")
	}
	if sec == nil || !p.HasRconPassword {
		return nil, errors.New("RCON is not configured for the active server")
	}
	return m.players(ctx, p.RconAddr(), sec.RconPassword, p.ID)
}
