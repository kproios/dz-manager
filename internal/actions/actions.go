// Package actions exposes mod-backed player actions (#7): teleport, give
// item, heal, kill and strip, executed by @DZMBridge through the command queue.
// Every action is validated here *and* in the mod, audited, and reports the
// in-game postcondition returned by the mod — never just "sent".
package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/bridge"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/players"
	"github.com/kproios/dz-manager/internal/profiles"
)

// WorldSize is the playable square (metres) used for coordinate validation (Chernarus).
const WorldSize = 15360.0

var reClass = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{1,63}$`)

// Spec describes one action.
type Spec struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Destructive bool   `json:"destructive"`
	// Confirm is the exact word the admin must type for destructive actions.
	Confirm string `json:"confirm,omitempty"`
}

// Catalog lists the supported actions in UI order.
var Catalog = []Spec{
	{Name: "teleport", Label: "Teleport"},
	{Name: "give_item", Label: "Give item"},
	{Name: "heal", Label: "Heal"},
	{Name: "kill", Label: "Kill", Destructive: true, Confirm: "KILL"},
	{Name: "strip", Label: "Strip inventory", Destructive: true, Confirm: "STRIP"},
}

func spec(name string) (Spec, bool) {
	for _, s := range Catalog {
		if s.Name == name {
			return s, true
		}
	}
	return Spec{}, false
}

// Request is the API body.
type Request struct {
	X        *float64 `json:"x,omitempty"`
	Z        *float64 `json:"z,omitempty"`
	Y        *float64 `json:"y,omitempty"` // optional altitude; mod snaps to surface when absent
	ToPlayer string   `json:"to_player,omitempty"`
	Class    string   `json:"class,omitempty"`
	Quantity int      `json:"quantity,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Confirm  string   `json:"confirm,omitempty"`
}

// Validate checks parameters for an action and returns the mod payload.
func Validate(action string, in Request) (map[string]any, error) {
	sp, ok := spec(action)
	if !ok {
		return nil, fmt.Errorf("unknown action %q", action)
	}
	if sp.Destructive && in.Confirm != sp.Confirm {
		return nil, fmt.Errorf("type %s to confirm this action", sp.Confirm)
	}
	reason := strings.TrimSpace(in.Reason)
	if len(reason) > 120 {
		return nil, errors.New("reason too long (max 120)")
	}
	p := map[string]any{}
	if reason != "" {
		p["reason"] = reason
	}
	switch action {
	case "teleport":
		if in.ToPlayer != "" {
			if len(in.ToPlayer) != 17 {
				return nil, errors.New("to_player must be a Steam64 ID")
			}
			p["to_player"] = in.ToPlayer
			break
		}
		if in.X == nil || in.Z == nil {
			return nil, errors.New("x and z (or to_player) are required")
		}
		for _, v := range []float64{*in.X, *in.Z} {
			if math.IsNaN(v) || v < 0 || v > WorldSize {
				return nil, fmt.Errorf("coordinates must be within 0..%.0f", WorldSize)
			}
		}
		p["x"], p["z"] = *in.X, *in.Z
		if in.Y != nil {
			if *in.Y < -50 || *in.Y > 1500 {
				return nil, errors.New("y out of range")
			}
			p["y"], p["has_y"] = *in.Y, true
		}
	case "give_item":
		if !reClass.MatchString(in.Class) {
			return nil, errors.New("class must be a DayZ classname (letters, digits, underscore)")
		}
		q := in.Quantity
		if q == 0 {
			q = 1
		}
		if q < 1 || q > 20 {
			return nil, errors.New("quantity must be 1..20")
		}
		// "item", not "class": class is a reserved word in Enforce Script.
		p["item"], p["quantity"] = in.Class, q
	}
	return p, nil
}

// API serves /api/players/{id}/actions.
type API struct {
	Bridge   *bridge.Bridge
	Players  *players.Service
	Profiles *profiles.Store
	Audit    *audit.Log
	// Wait bounds how long the API waits for the in-game result.
	Wait time.Duration

	mu       sync.Mutex
	inflight map[string]bool
}

// Register adds routes.
func (a *API) Register(mux *http.ServeMux) {
	a.inflight = map[string]bool{}
	if a.Wait == 0 {
		a.Wait = 20 * time.Second
	}
	mux.HandleFunc("GET /api/actions", a.catalog)
	mux.HandleFunc("POST /api/players/{id}/actions/{action}", a.run)
}

// Availability of one action for the active server.
type Availability struct {
	Spec
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

func (a *API) catalog(w http.ResponseWriter, r *http.Request) {
	p, _, err := a.Profiles.Active(r.Context())
	out := make([]Availability, 0, len(Catalog))
	for _, s := range Catalog {
		av := Availability{Spec: s}
		switch {
		case err != nil:
			av.Reason = "no active server profile"
		case !a.Bridge.Supports(p.ID, s.Name):
			av.Reason = "requires @DZMBridge with the " + s.Name + " handler (not connected or not advertised)"
		default:
			av.Available = true
		}
		out = append(out, av)
	}
	httpx.JSON(w, 200, out)
}

func (a *API) run(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	action := r.PathValue("action")
	u := auth.FromContext(ctx)
	actor := audit.System
	if u != nil {
		actor = u.Actor()
	}
	var in Request
	if err := httpx.Decode(r, 4<<10, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	payload, err := Validate(action, in)
	if err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	pl, err := a.Players.Find(ctx, r.PathValue("id"))
	if err != nil {
		httpx.Error(w, 404, "unknown player")
		return
	}
	prof, _, err := a.Profiles.Active(ctx)
	if err != nil {
		httpx.Error(w, 409, "no active server profile")
		return
	}
	key := pl.Steam64 + "/" + action
	a.mu.Lock()
	if a.inflight[key] {
		a.mu.Unlock()
		httpx.Error(w, 409, "this action is already running for this player")
		return
	}
	a.inflight[key] = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.inflight, key); a.mu.Unlock() }()

	params := map[string]any{"name": pl.Name}
	for k, v := range payload {
		params[k] = v
	}
	id, err := a.Bridge.Enqueue(ctx, prof.ID, action, pl.Steam64, payload, actor, a.Wait)
	if err != nil {
		_ = a.Audit.Write(ctx, actor, "player."+action, pl.Steam64, params, "error", err.Error())
		httpx.JSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.Wait+2*time.Second)
	defer cancel()
	c, err := a.Bridge.Await(wctx, id)
	if err != nil || c == nil {
		msg := "no result from the game server"
		_ = a.Audit.Write(ctx, actor, "player."+action, pl.Steam64, params, "error", msg)
		httpx.JSON(w, 504, map[string]any{"ok": false, "command_id": id, "error": msg})
		return
	}
	switch c.Status {
	case "ok":
		_ = a.Audit.Write(ctx, actor, "player."+action, pl.Steam64, params, "ok", string(c.Result))
		httpx.JSON(w, 200, map[string]any{"ok": true, "command_id": id, "result": json.RawMessage(nonEmpty(c.Result))})
	default:
		_ = a.Audit.Write(ctx, actor, "player."+action, pl.Steam64, params, "error", c.Status+": "+c.Error)
		httpx.JSON(w, 502, map[string]any{"ok": false, "command_id": id, "status": c.Status, "error": c.Error})
	}
}

func nonEmpty(b json.RawMessage) []byte {
	if len(b) == 0 {
		return []byte("null")
	}
	return b
}
