package metricz

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kproios/dz-manager/internal/caps"
)

// bridgePerfFresh is how long a @DZMBridge performance report (sent every
// minute) counts as current. While it is, it is preferred over MetricZ.
const bridgePerfFresh = 3 * time.Minute

type bridgePerf struct {
	perf Perf
	at   time.Time
}

// BridgePerf records a server.perf report from @DZMBridge: frame rate,
// uptime and entity counts. It feeds the same per-minute history as MetricZ
// and replaces MetricZ's numbers while it keeps arriving.
func (rc *Receiver) BridgePerf(ctx context.Context, serverID string, data []byte, at time.Time) error {
	var p Perf
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	now := time.Now().UTC()
	if at.IsZero() || at.After(now.Add(time.Minute)) {
		at = now
	}
	if now.Sub(at) > bridgePerfFresh {
		return nil // a late backlog report would rewrite the past as current
	}
	rc.mu.Lock()
	if rc.bridgePerfs == nil {
		rc.bridgePerfs = map[string]bridgePerf{}
	}
	rc.bridgePerfs[serverID] = bridgePerf{perf: p, at: at}
	_, hasMetricZ := rc.snaps[serverID]
	rc.mu.Unlock()
	if !hasMetricZ {
		rc.Caps.Set(serverID, caps.MetricZ, caps.Unconfigured, "not used: @DZMBridge reports server performance")
	}
	return rc.writePerf(ctx, serverID, at.UTC().Format("2006-01-02T15:04"), p)
}

// bridgePerfLocked returns the current bridge report for a server, if fresh.
// Call with rc.mu held.
func (rc *Receiver) bridgePerfLocked(serverID string) (Perf, bool) {
	bp, ok := rc.bridgePerfs[serverID]
	if !ok || time.Since(bp.at) > bridgePerfFresh {
		return Perf{}, false
	}
	return bp.perf, true
}
