package rcon_test

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/woozymasta/bercon/server"

	"github.com/kproios/dz-manager/internal/apptest"
	"github.com/kproios/dz-manager/internal/profiles"
	"github.com/kproios/dz-manager/internal/rcon"
)

const playersOut = "Players on server:\n[#] [IP Address]:[Port] [Ping] [GUID] [Name]\n--------------------------------------------------\n0  127.0.0.1:2302 42 0123456789abcdef0123456789abcdef(OK) Survivor One\n3  10.1.1.1:2302 80 fedcba9876543210fedcba9876543210(OK) Second\n(2 players in total)"

type fakeBE struct {
	mu      sync.Mutex
	cmds    []string
	players string
	addr    *net.UDPAddr
}

func startBE(t *testing.T, password string) *fakeBE {
	f := &fakeBE{players: playersOut}
	srv, err := server.New("127.0.0.1:0", server.HandlerFunc(func(_ context.Context, r server.Request) server.Response {
		f.mu.Lock()
		defer f.mu.Unlock()
		c := string(r.Command)
		f.cmds = append(f.cmds, c)
		if c == "players" {
			return server.Response{Data: []byte(f.players)}
		}
		return server.Response{Data: []byte("")}
	}), &server.Options{Password: password})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Serve(ctx)
	<-srv.Ready()
	t.Cleanup(func() { cancel(); srv.Close() })
	f.addr = srv.Addr().(*net.UDPAddr)
	return f
}

func (f *fakeBE) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cmds...)
}

func setup(t *testing.T, password string) (*apptest.Env, *fakeBE, *bool) {
	e := apptest.New(t, "")
	e.Login()
	be := startBE(t, "secret")
	queryOK := true
	e.App.RCON.Query = func(ctx context.Context, addr string) (*rcon.QueryInfo, error) {
		if !queryOK {
			return nil, errors.New("i/o timeout")
		}
		return &rcon.QueryInfo{Name: "EndPoint Test", Map: "chernarusplus", Players: 2, MaxPlayers: 60}, nil
	}
	var p profiles.Profile
	if code := e.JSON("POST", "/api/profiles", profiles.Input{Name: "TestServer", GameHost: "127.0.0.1", RconPort: be.addr.Port, RconPassword: password}, &p); code != 201 {
		t.Fatalf("create = %d", code)
	}
	return e, be, &queryOK
}

func TestRosterAndStates(t *testing.T) {
	e, _, queryOK := setup(t, "secret")
	e.App.RCON.Poll(e.Ctx)
	st := e.App.RCON.Status()
	if st.RCON.State != rcon.StateOK || st.Query.State != rcon.StateOK || len(st.Players) != 2 {
		t.Fatalf("status: %+v", st)
	}
	if p := st.Players[1]; p.Number != 3 || p.BEGUID != "fedcba9876543210fedcba9876543210" || p.Name != "Second" || !p.Verified {
		t.Fatalf("player parse: %+v", p)
	}
	if !e.App.Caps.OK(st.ServerID, "rcon") || !e.App.Caps.OK(st.ServerID, "a2s") {
		t.Fatal("caps not ok")
	}
	// IP hidden unless explicitly revealed.
	var pl struct{ Players []map[string]any }
	e.JSON("GET", "/api/server/players", nil, &pl)
	if _, has := pl.Players[0]["ip"]; has {
		t.Fatal("ip leaked without reveal")
	}
	e.JSON("GET", "/api/server/players?reveal_ip=1", nil, &pl)
	if pl.Players[0]["ip"] != "127.0.0.1" {
		t.Fatalf("reveal: %+v", pl.Players[0])
	}
	// Query failure right after success → stale, not unreachable.
	*queryOK = false
	e.App.RCON.Poll(e.Ctx)
	if s := e.App.RCON.Status(); s.Query.State != rcon.StateStale {
		t.Fatalf("query = %s, want stale", s.Query.State)
	}
	*queryOK = true
	e.App.RCON.Poll(e.Ctx)
	if s := e.App.RCON.Status(); s.Query.State != rcon.StateOK {
		t.Fatalf("query recovered = %s", s.Query.State)
	}
}

func TestEmptyServer(t *testing.T) {
	e, be, _ := setup(t, "secret")
	be.mu.Lock()
	be.players = "Players on server:\n[#] [IP Address]:[Port] [Ping] [GUID] [Name]\n--------------------------------------------------\n(0 players in total)"
	be.mu.Unlock()
	e.App.RCON.Poll(e.Ctx)
	if st := e.App.RCON.Status(); st.RCON.State != rcon.StateOK || len(st.Players) != 0 {
		t.Fatalf("empty: %+v", st)
	}
}

func TestWrongPassword(t *testing.T) {
	e, _, _ := setup(t, "nope")
	e.App.RCON.Poll(e.Ctx)
	if st := e.App.RCON.Status(); st.RCON.State != rcon.StateAuthFailed {
		t.Fatalf("rcon = %s (%s), want auth_failed", st.RCON.State, st.RCON.Error)
	}
}

func TestUnreachable(t *testing.T) {
	e := apptest.New(t, "")
	e.Login()
	e.App.RCON.Query = func(context.Context, string) (*rcon.QueryInfo, error) { return nil, errors.New("timeout") }
	e.JSON("POST", "/api/profiles", profiles.Input{Name: "x", GameHost: "127.0.0.1", RconPort: freePort(t), RconPassword: "pw"}, nil)
	e.App.RCON.Poll(e.Ctx)
	if st := e.App.RCON.Status(); st.RCON.State != rcon.StateUnreachable || st.Query.State != rcon.StateUnreachable {
		t.Fatalf("status: %+v", st)
	}
}

func TestActions(t *testing.T) {
	e, be, _ := setup(t, "secret")
	e.App.RCON.Poll(e.Ctx)
	// Kick resolves the GUID to the *current* player number.
	if code := e.JSON("POST", "/api/rcon/kick", map[string]string{"be_guid": "FEDCBA9876543210FEDCBA9876543210", "reason": "test\nkick"}, nil); code != 200 {
		t.Fatalf("kick = %d", code)
	}
	// Player number reuse: GUID now on a different number.
	be.mu.Lock()
	be.players = strings.Replace(playersOut, "3  10.1.1.1", "7  10.1.1.1", 1)
	be.mu.Unlock()
	if code := e.JSON("POST", "/api/rcon/say", map[string]string{"be_guid": "fedcba9876543210fedcba9876543210", "message": "hi"}, nil); code != 200 {
		t.Fatalf("pm = %d", code)
	}
	if code := e.JSON("POST", "/api/rcon/say", map[string]string{"message": "Restart in 5 minutes"}, nil); code != 200 {
		t.Fatalf("broadcast = %d", code)
	}
	// Double-click guard.
	if code := e.JSON("POST", "/api/rcon/say", map[string]string{"message": "Restart in 5 minutes"}, nil); code != 409 {
		t.Fatalf("duplicate = %d, want 409", code)
	}
	// Offline target: no command sent.
	if code := e.JSON("POST", "/api/rcon/kick", map[string]string{"be_guid": strings.Repeat("a", 32), "reason": "x"}, nil); code != 502 {
		t.Fatalf("offline kick = %d", code)
	}
	if code := e.JSON("POST", "/api/rcon/lock", nil, nil); code != 200 {
		t.Fatalf("lock = %d", code)
	}
	var want = []string{"kick 3 test kick", "say 7 hi", "say -1 Restart in 5 minutes", "#lock"}
	got := []string{}
	for _, c := range be.sent() {
		if c != "players" && c != "" {
			got = append(got, c)
		}
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("commands sent:\n got %q\nwant %q", got, want)
	}
	if code := e.JSON("POST", "/api/rcon/say", map[string]string{"message": strings.Repeat("x", 300)}, nil); code != 400 {
		t.Fatalf("long message = %d", code)
	}
}

func freePort(t *testing.T) int {
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, p, _ := net.SplitHostPort(c.LocalAddr().String())
	n, _ := strconv.Atoi(p)
	return n
}
