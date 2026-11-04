package apptest

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/woozymasta/bercon/server"
)

// FakeBE is a scriptable BattlEye RCON server with an in-memory ban list.
type FakeBE struct {
	mu      sync.Mutex
	Cmds    []string
	Players string
	Bans    []FakeBan
	Port    int
}

// FakeBan is one GUID ban.
type FakeBan struct {
	GUID, Reason string
	Minutes      int
}

// StartBE starts a fake server with the given password.
func StartBE(t *testing.T, password string) *FakeBE {
	f := &FakeBE{Players: "Players on server:\n[#] [IP Address]:[Port] [Ping] [GUID] [Name]\n--------------------------------------------------\n(0 players in total)"}
	srv, err := server.New("127.0.0.1:0", server.HandlerFunc(func(_ context.Context, r server.Request) server.Response {
		return server.Response{Data: []byte(f.handle(string(r.Command)))}
	}), &server.Options{Password: password})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Serve(ctx)
	<-srv.Ready()
	t.Cleanup(func() { cancel(); srv.Close() })
	f.Port = srv.Addr().(*net.UDPAddr).Port
	return f
}

func (f *FakeBE) handle(cmd string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Cmds = append(f.Cmds, cmd)
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	switch fields[0] {
	case "players":
		return f.Players
	case "bans":
		var sb strings.Builder
		sb.WriteString("GUID Bans:\n[#] [GUID] [Minutes left] [Reason]\n----------------------------------------\n")
		for i, b := range f.Bans {
			m := "perm"
			if b.Minutes > 0 {
				m = fmt.Sprint(b.Minutes)
			}
			fmt.Fprintf(&sb, "%d  %s %s %s\n", i, b.GUID, m, b.Reason)
		}
		sb.WriteString("\nIP Bans:\n[#] [IP Address] [Minutes left] [Reason]\n----------------------------------------------\n")
		return sb.String()
	case "addBan":
		if len(fields) >= 3 {
			var m int
			fmt.Sscan(fields[2], &m)
			f.Bans = append(f.Bans, FakeBan{GUID: fields[1], Minutes: m, Reason: strings.Join(fields[3:], " ")})
		}
	case "removeBan":
		var n int
		fmt.Sscan(fields[1], &n)
		if n >= 0 && n < len(f.Bans) {
			f.Bans = append(f.Bans[:n], f.Bans[n+1:]...)
		}
	}
	return ""
}

// Sent returns non-polling commands sent so far.
func (f *FakeBE) Sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.Cmds {
		if c != "players" && c != "bans" && c != "" {
			out = append(out, c)
		}
	}
	return out
}

// SetPlayers replaces the players response.
func (f *FakeBE) SetPlayers(s string) { f.mu.Lock(); f.Players = s; f.mu.Unlock() }

// AddForeignBan inserts a ban created by someone else.
func (f *FakeBE) AddForeignBan(guid, reason string) {
	f.mu.Lock()
	f.Bans = append(f.Bans, FakeBan{GUID: guid, Reason: reason})
	f.mu.Unlock()
}

// BanCount returns the number of bans held.
func (f *FakeBE) BanCount() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.Bans) }
