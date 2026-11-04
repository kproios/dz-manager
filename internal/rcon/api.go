package rcon

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/httpx"
)

// API exposes status, roster and RCON-native actions.
type API struct {
	Mon   *Monitor
	Audit *audit.Log

	mu     sync.Mutex
	recent map[string]time.Time
}

// Register adds routes to the authenticated mux.
func (a *API) Register(mux *http.ServeMux) {
	a.recent = map[string]time.Time{}
	mux.HandleFunc("GET /api/server/status", a.status)
	mux.HandleFunc("GET /api/server/players", a.players)
	mux.HandleFunc("POST /api/server/refresh", a.refresh)
	mux.HandleFunc("POST /api/rcon/say", a.say)
	mux.HandleFunc("POST /api/rcon/kick", a.kick)
	mux.HandleFunc("POST /api/rcon/lock", a.lock(true))
	mux.HandleFunc("POST /api/rcon/unlock", a.lock(false))
}

// PlayerView adds the optional IP reveal.
type PlayerView struct {
	Player
	IP string `json:"ip,omitempty"`
}

func (a *API) status(w http.ResponseWriter, r *http.Request) {
	s := a.Mon.Status()
	httpx.JSON(w, 200, s)
}

func (a *API) players(w http.ResponseWriter, r *http.Request) {
	s := a.Mon.Status()
	reveal := r.URL.Query().Get("reveal_ip") == "1"
	out := make([]PlayerView, 0, len(s.Players))
	for _, p := range s.Players {
		v := PlayerView{Player: p}
		if reveal {
			v.IP = p.IP
		}
		out = append(out, v)
	}
	if reveal {
		_ = a.Audit.Write(r.Context(), actor(r), "players.reveal_ip", s.ServerID, nil, "ok", "")
	}
	httpx.JSON(w, 200, map[string]any{"server_id": s.ServerID, "rcon": s.RCON, "players": out})
}

func (a *API) refresh(w http.ResponseWriter, r *http.Request) {
	a.Mon.Poll(r.Context())
	httpx.JSON(w, 200, a.Mon.Status())
}

func actor(r *http.Request) audit.Actor {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.Actor()
	}
	return audit.System
}

// clean makes user text safe for a single RCON command line.
func clean(s string, max int) (string, error) {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("text is required")
	}
	if len([]rune(s)) > max {
		return "", fmt.Errorf("text too long (max %d characters)", max)
	}
	return s, nil
}

// dedupe rejects an identical action repeated within 3 s (double-click guard).
func (a *API) dedupe(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, t := range a.recent {
		if now.Sub(t) > 3*time.Second {
			delete(a.recent, k)
		}
	}
	if _, dup := a.recent[key]; dup {
		return false
	}
	a.recent[key] = now
	return true
}

type sayReq struct {
	Message string `json:"message"`
	BEGUID  string `json:"be_guid,omitempty"` // empty = broadcast
}

func (a *API) say(w http.ResponseWriter, r *http.Request) {
	var in sayReq
	if err := httpx.Decode(r, 4<<10, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	msg, err := clean(in.Message, 200)
	if err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	action, target := "rcon.broadcast", "all"
	if in.BEGUID != "" {
		action, target = "rcon.message", in.BEGUID
	}
	if !a.dedupe(action + target + msg) {
		httpx.Error(w, 409, "identical action sent less than 3 seconds ago")
		return
	}
	num := -1
	if in.BEGUID != "" {
		p, err := a.resolve(r, in.BEGUID)
		if err != nil {
			a.audited(w, r, action, target, map[string]any{"message": msg}, err, nil)
			return
		}
		num = p.Number
	}
	_, out, err := a.Mon.Exec(r.Context(), fmt.Sprintf("say %d %s", num, msg))
	a.audited(w, r, action, target, map[string]any{"message": msg}, err, out)
}

type kickReq struct {
	BEGUID string `json:"be_guid"`
	Reason string `json:"reason"`
}

func (a *API) kick(w http.ResponseWriter, r *http.Request) {
	var in kickReq
	if err := httpx.Decode(r, 4<<10, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	reason, err := clean(in.Reason, 120)
	if err != nil {
		httpx.Error(w, 400, "reason: "+err.Error())
		return
	}
	if !a.dedupe("kick" + in.BEGUID) {
		httpx.Error(w, 409, "identical action sent less than 3 seconds ago")
		return
	}
	params := map[string]any{"reason": reason}
	p, err := a.resolve(r, in.BEGUID)
	if err != nil {
		a.audited(w, r, "rcon.kick", in.BEGUID, params, err, nil)
		return
	}
	params["name"] = p.Name
	_, out, err := a.Mon.Exec(r.Context(), fmt.Sprintf("kick %d %s", p.Number, reason))
	a.audited(w, r, "rcon.kick", in.BEGUID, params, err, out)
}

func (a *API) lock(locked bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cmd, action := "#unlock", "rcon.unlock"
		if locked {
			cmd, action = "#lock", "rcon.lock"
		}
		if !a.dedupe(action) {
			httpx.Error(w, 409, "identical action sent less than 3 seconds ago")
			return
		}
		_, out, err := a.Mon.Exec(r.Context(), cmd)
		if err == nil {
			a.Mon.mu.Lock()
			l := locked
			a.Mon.status.Locked = &l
			a.Mon.mu.Unlock()
		}
		a.audited(w, r, action, "server", nil, err, out)
	}
}

// resolve finds the player by BE GUID in a fresh roster, so a reused player
// number can never redirect an action to someone else.
func (a *API) resolve(r *http.Request, guid string) (*Player, error) {
	guid = strings.ToLower(strings.TrimSpace(guid))
	if len(guid) != 32 {
		return nil, errors.New("be_guid must be a 32-character BattlEye GUID")
	}
	ps, err := a.Mon.FreshPlayers(r.Context())
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		if strings.EqualFold(p.BEGUID, guid) {
			return &p, nil
		}
	}
	return nil, errors.New("player is not online")
}

func (a *API) audited(w http.ResponseWriter, r *http.Request, action, target string, params map[string]any, err error, out []byte) {
	reply := strings.TrimSpace(string(out))
	if err != nil {
		_ = a.Audit.Write(r.Context(), actor(r), action, target, params, "error", err.Error())
		httpx.JSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	_ = a.Audit.Write(r.Context(), actor(r), action, target, params, "ok", reply)
	httpx.JSON(w, 200, map[string]any{"ok": true, "reply": reply})
}
