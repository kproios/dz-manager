// Package actions exposes mod-backed actions executed by @DZMBridge through
// the command queue: player actions (teleport, give item, heal, kill, strip)
// and vehicle actions (refuel, repair, flip, stop engine, delete). Every
// action is validated here *and* in the mod, audited, and reports the in-game
// postcondition returned by the mod, never just "sent".
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

// DefaultWorldSize is used when no world size is known (Chernarus).
const DefaultWorldSize = 15360.0

var (
	reClass   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{1,63}$`)
	reVehicle = regexp.MustCompile(`^-?[0-9]{1,10}:-?[0-9]{1,10}$`)
	reSteam   = regexp.MustCompile(`^[0-9]{17}$`)
)

// Spec describes one action.
type Spec struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Target      string `json:"target"` // player | vehicle
	Destructive bool   `json:"destructive"`
	// Confirm is the exact word the admin must type for destructive actions.
	Confirm string `json:"confirm,omitempty"`
	Help    string `json:"help,omitempty"`
}

// Catalog lists the player actions in UI order.
var Catalog = []Spec{
	{Name: "teleport", Label: "Teleport", Target: "player"},
	{Name: "give_item", Label: "Give item", Target: "player"},
	{Name: "heal", Label: "Heal", Target: "player"},
	{Name: "kill", Label: "Kill", Target: "player", Destructive: true, Confirm: "KILL"},
	{Name: "strip", Label: "Strip inventory", Target: "player", Destructive: true, Confirm: "STRIP"},
}

// VehicleCatalog lists the vehicle actions in UI order.
var VehicleCatalog = []Spec{
	{Name: "vehicle_refuel", Label: "Refuel", Target: "vehicle", Help: "Fills the fuel tank."},
	{Name: "vehicle_repair", Label: "Repair", Target: "vehicle", Help: "Restores the vehicle and its attached parts to pristine and tops up coolant, oil and brake fluid. Missing parts are not added."},
	{Name: "vehicle_flip", Label: "Flip upright", Target: "vehicle", Help: "Puts the vehicle back on its wheels where it is. Refused while it moves."},
	{Name: "vehicle_stop_engine", Label: "Stop engine", Target: "vehicle", Help: "Turns the engine off."},
	{Name: "vehicle_delete", Label: "Delete", Target: "vehicle", Destructive: true, Confirm: "DELETE", Help: "Removes the vehicle and everything in it. Refused while someone is inside."},
}

// Spec returns the spec of a player or vehicle action.
func SpecFor(name string) (Spec, bool) {
	for _, s := range Catalog {
		if s.Name == name {
			return s, true
		}
	}
	for _, s := range VehicleCatalog {
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
func Validate(action string, in Request, worldSize float64) (map[string]any, error) {
	sp, ok := SpecFor(action)
	if !ok {
		return nil, fmt.Errorf("unknown action %q", action)
	}
	if sp.Destructive && in.Confirm != sp.Confirm {
		return nil, fmt.Errorf("type %s to confirm this action", sp.Confirm)
	}
	if worldSize <= 0 {
		worldSize = DefaultWorldSize
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
			if !reSteam.MatchString(in.ToPlayer) {
				return nil, errors.New("to_player must be a Steam64 ID")
			}
			p["to_player"] = in.ToPlayer
			break
		}
		if in.X == nil || in.Z == nil {
			return nil, errors.New("x and z (or to_player) are required")
		}
		for _, v := range []float64{*in.X, *in.Z} {
			if math.IsNaN(v) || v < 0 || v > worldSize {
				return nil, fmt.Errorf("coordinates must be within 0..%.0f", worldSize)
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

// API serves the player and vehicle action routes.
type API struct {
	Bridge   *bridge.Bridge
	Players  *players.Service
	Profiles *profiles.Store
	Audit    *audit.Log
	// WorldSize returns the active map's size in metres (0 = unknown).
	WorldSize func(context.Context) float64
	// Wait bounds how long the API waits for the in-game result.
	Wait time.Duration

	mu       sync.Mutex
	inflight map[string]bool
}

// Register adds routes.
func (a *API) Register(mux *http.ServeMux) {
	a.init()
	mux.HandleFunc("GET /api/actions", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, a.Available(r.Context(), Catalog))
	})
	mux.HandleFunc("GET /api/vehicles/actions", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, a.Available(r.Context(), VehicleCatalog))
	})
	mux.HandleFunc("POST /api/players/{id}/actions/{action}", func(w http.ResponseWriter, r *http.Request) {
		a.serve(w, r, "player", r.PathValue("id"), r.PathValue("action"), actorOf(r))
	})
	mux.HandleFunc("POST /api/vehicles/{id}/actions/{action}", func(w http.ResponseWriter, r *http.Request) {
		a.serve(w, r, "vehicle", r.PathValue("id"), r.PathValue("action"), actorOf(r))
	})
}

func (a *API) init() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inflight == nil {
		a.inflight = map[string]bool{}
	}
	if a.Wait == 0 {
		a.Wait = 20 * time.Second
	}
}

func actorOf(r *http.Request) audit.Actor {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.Actor()
	}
	return audit.System
}

// Availability of one action for the active server.
type Availability struct {
	Spec
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// Available reports which actions the active server's bridge supports.
func (a *API) Available(ctx context.Context, list []Spec) []Availability {
	p, _, err := a.Profiles.Active(ctx)
	out := make([]Availability, 0, len(list))
	for _, s := range list {
		av := Availability{Spec: s}
		switch {
		case err != nil:
			av.Reason = "no active server profile"
		case !a.Bridge.Supports(p.ID, s.Name):
			av.Reason = "requires @DZMBridge with the " + s.Name + " handler (not connected, or an older bridge version)"
		default:
			av.Available = true
		}
		out = append(out, av)
	}
	return out
}

// Outcome is the result of one action.
type Outcome struct {
	Status    int             `json:"-"`
	OK        bool            `json:"ok"`
	CommandID string          `json:"command_id,omitempty"`
	State     string          `json:"status,omitempty"`
	Error     string          `json:"error,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

func (a *API) serve(w http.ResponseWriter, r *http.Request, target, id, action string, actor audit.Actor) {
	var in Request
	if err := httpx.Decode(r, 4<<10, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	o := a.Run(r.Context(), actor, target, id, action, in)
	httpx.JSON(w, o.Status, o)
}

// Run validates, queues and awaits one action. target is "player" (id =
// Steam64 or player id) or "vehicle" (id = the bridge's network id "lo:hi").
// Used by the panel, the public API and the MCP server.
func (a *API) Run(ctx context.Context, actor audit.Actor, target, id, action string, in Request) Outcome {
	a.init()
	sp, ok := SpecFor(action)
	if !ok || sp.Target != target {
		return Outcome{Status: 400, Error: fmt.Sprintf("unknown %s action %q", target, action)}
	}
	size := DefaultWorldSize
	if a.WorldSize != nil {
		size = a.WorldSize(ctx)
	}
	payload, err := Validate(action, in, size)
	if err != nil {
		return Outcome{Status: 400, Error: err.Error()}
	}
	prof, _, err := a.Profiles.Active(ctx)
	if err != nil {
		return Outcome{Status: 409, Error: "no active server profile"}
	}
	params := map[string]any{}
	targetID, auditTarget := "", ""
	if target == "player" {
		pl, err := a.Players.Find(ctx, id)
		if err != nil {
			return Outcome{Status: 404, Error: "unknown player"}
		}
		targetID, auditTarget = pl.Steam64, pl.Steam64
		params["name"] = pl.Name
	} else {
		if !reVehicle.MatchString(id) {
			return Outcome{Status: 400, Error: "vehicle id must be the bridge network id (\"lo:hi\") shown on the live map"}
		}
		targetID, auditTarget = id, "vehicle:"+id
	}
	for k, v := range payload {
		params[k] = v
	}
	key := targetID + "/" + action
	a.mu.Lock()
	if a.inflight[key] {
		a.mu.Unlock()
		return Outcome{Status: 409, Error: "this action is already running for this " + target}
	}
	a.inflight[key] = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.inflight, key); a.mu.Unlock() }()

	auditAction := target + "." + strings.TrimPrefix(action, "vehicle_")
	cid, err := a.Bridge.Enqueue(ctx, prof.ID, action, targetID, payload, actor, a.Wait)
	if err != nil {
		_ = a.Audit.Write(ctx, actor, auditAction, auditTarget, params, "error", err.Error())
		return Outcome{Status: 409, Error: err.Error()}
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.Wait+2*time.Second)
	defer cancel()
	c, err := a.Bridge.Await(wctx, cid)
	if err != nil || c == nil {
		msg := "no result from the game server"
		_ = a.Audit.Write(ctx, actor, auditAction, auditTarget, params, "error", msg)
		return Outcome{Status: 504, CommandID: cid, Error: msg}
	}
	if c.Status == "ok" {
		_ = a.Audit.Write(ctx, actor, auditAction, auditTarget, params, "ok", string(c.Result))
		return Outcome{Status: 200, OK: true, CommandID: cid, Result: nonEmpty(c.Result)}
	}
	_ = a.Audit.Write(ctx, actor, auditAction, auditTarget, params, "error", c.Status+": "+c.Error)
	return Outcome{Status: 502, CommandID: cid, State: c.Status, Error: c.Error}
}

func nonEmpty(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return b
}
