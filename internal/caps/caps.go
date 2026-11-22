// Package caps is the capability registry: every feature declares which
// capability it needs, and the UI disables features whose capability is not ok.
package caps

import (
	"sort"
	"sync"
	"time"
)

// Name of a capability.
type Name string

// Known capabilities.
const (
	RCON             Name = "rcon"
	A2S              Name = "a2s"
	MetricZ          Name = "metricz"
	Bridge           Name = "bridge"
	BridgeCommands   Name = "bridge.commands"
	ProviderSchedule Name = "provider.schedule"
	RestartDrive     Name = "restart.drive"
)

// All lists capabilities in display order.
var All = []Name{RCON, A2S, MetricZ, Bridge, BridgeCommands, ProviderSchedule, RestartDrive}

// Optional capabilities are integrations nothing depends on: List leaves them
// out until they are in use, so an unused one never shows as missing.
var Optional = map[Name]bool{MetricZ: true}

// State of a capability for one server.
type State string

// States.
const (
	Unconfigured State = "unconfigured"
	Unverified   State = "unverified"
	OK           State = "ok"
	Degraded     State = "degraded"
	Unsupported  State = "unsupported"
)

// Status is the current state plus a human-readable reason.
type Status struct {
	Name      Name      `json:"name"`
	State     State     `json:"state"`
	Reason    string    `json:"reason"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Registry is safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	byServer map[string]map[Name]Status
	onChange func(serverID string)
}

// NewRegistry creates an empty registry. onChange may be nil.
func NewRegistry(onChange func(serverID string)) *Registry {
	return &Registry{byServer: map[string]map[Name]Status{}, onChange: onChange}
}

// Set records a capability state.
func (r *Registry) Set(serverID string, n Name, s State, reason string) {
	r.mu.Lock()
	m := r.byServer[serverID]
	if m == nil {
		m = map[Name]Status{}
		r.byServer[serverID] = m
	}
	prev, had := m[n]
	m[n] = Status{Name: n, State: s, Reason: reason, UpdatedAt: time.Now().UTC()}
	r.mu.Unlock()
	if r.onChange != nil && (!had || prev.State != s || prev.Reason != reason) {
		r.onChange(serverID)
	}
}

// Get returns one capability (Unconfigured if never set).
func (r *Registry) Get(serverID string, n Name) Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s, ok := r.byServer[serverID][n]; ok {
		return s
	}
	return Status{Name: n, State: Unconfigured, Reason: "not configured"}
}

// OK reports whether a capability is usable.
func (r *Registry) OK(serverID string, n Name) bool { return r.Get(serverID, n).State == OK }

// List returns all known capabilities for a server in display order.
// Optional ones appear only while configured (any state but Unconfigured).
func (r *Registry) List(serverID string) []Status {
	out := make([]Status, 0, len(All))
	for _, n := range All {
		s := r.Get(serverID, n)
		if Optional[n] && s.State == Unconfigured {
			continue
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return indexOf(out[i].Name) < indexOf(out[j].Name) })
	return out
}

// Forget drops all state for a server (e.g. profile deleted).
func (r *Registry) Forget(serverID string) {
	r.mu.Lock()
	delete(r.byServer, serverID)
	r.mu.Unlock()
}

func indexOf(n Name) int {
	for i, x := range All {
		if x == n {
			return i
		}
	}
	return len(All)
}
