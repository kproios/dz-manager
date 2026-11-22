package restarts

import (
	"context"
	"fmt"
	"time"

	"github.com/kproios/dz-manager/internal/hooks"
)

// startLookback is how far back a shutdown request or planned restart can be
// and still explain a server start.
const startLookback = 10 * time.Minute

// ServerStarted records a restart the game server itself reported (the
// bridge's server.start event). A provider that relaunches within seconds is
// never seen offline by the status poll, so without this a restart that
// happened would be logged as missed.
func (s *Service) ServerStarted(ctx context.Context, serverID string, at time.Time) {
	if serverID == "" {
		return
	}
	now := s.Now().UTC()
	if now.Sub(at) > startLookback && !at.IsZero() {
		return // an old start delivered late (backlog replay) says nothing about now
	}
	if at.IsZero() || at.After(now.Add(time.Minute)) {
		at = now // the game clock is not trusted beyond a small skew
	}
	at = at.UTC()
	// A freshly started DayZ server accepts joins again.
	s.setLocked(false)

	// A restart the poll saw going down ends here.
	var id int64
	var downAt, outcome string
	if err := s.DB.QueryRowContext(ctx, `SELECT id, down_at, outcome FROM restart_events WHERE server_id=? AND up_at IS NULL AND down_at IS NOT NULL ORDER BY id DESC LIMIT 1`, serverID).Scan(&id, &downAt, &outcome); err == nil {
		if d, err := time.Parse(time.RFC3339, downAt); err == nil && !at.Before(d) {
			res := "ok"
			if outcome == "not_back" {
				res = "recovered_late"
			}
			_, _ = s.DB.ExecContext(ctx, `UPDATE restart_events SET up_at=?, duration_s=?, outcome=? WHERE id=?`, at.Format(time.RFC3339), int64(at.Sub(d).Seconds()), res, id)
			s.recorded(ctx, serverID)
			return
		}
	}
	// Already recorded by the poll (down and back up around this start).
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM restart_events WHERE server_id=? AND down_at IS NOT NULL AND up_at BETWEEN ? AND ?`, serverID,
		at.Add(-startLookback).Format(time.RFC3339), at.Add(startLookback).Format(time.RFC3339)).Scan(&n)
	if n > 0 {
		return
	}

	// What explains this start: a restart DZ Manager requested, a planned one, or neither.
	kind, down := "unplanned", at
	var planned any
	var manualUntil int64
	_ = s.DB.QueryRowContext(ctx, `SELECT CAST(value AS INTEGER) FROM settings WHERE key=?`, "restart_now_until:"+serverID).Scan(&manualUntil)
	// restart_now_until is the request time plus three minutes.
	if req := time.Unix(manualUntil, 0).UTC().Add(-3 * time.Minute); manualUntil > 0 && !req.After(at) && at.Sub(req) <= startLookback {
		kind, down = "manual", req
	} else if sc := s.Load(ctx, serverID); sc.Enabled {
		for _, o := range sc.Occurrences(at.Add(-startLookback), at.Add(time.Duration(sc.WindowMinutes)*time.Minute)) {
			kind, planned = "planned", o.UTC().Format(time.RFC3339)
			if o.Before(at) {
				down = o.UTC()
			}
			// A late start may already have been written off as missed.
			_, _ = s.DB.ExecContext(ctx, `DELETE FROM restart_events WHERE server_id=? AND kind='missed' AND planned_for=?`, serverID, planned)
			break
		}
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO restart_events(server_id,down_at,up_at,planned_for,kind,outcome,duration_s) VALUES(?,?,?,?,?, 'ok', ?)`,
		serverID, down.Format(time.RFC3339), at.Format(time.RFC3339), planned, kind, int64(at.Sub(down).Seconds())); err != nil {
		s.Log.Error("restart event insert", "err", err)
		return
	}
	if kind == "unplanned" {
		s.Alert.Send(fmt.Sprintf("server restarted outside the restart schedule at %s (crash or manual restart?)", at.Format("15:04 MST")))
		s.hook(hooks.Server, "Server restarted unexpectedly", "It is back up.")
	}
	s.recorded(ctx, serverID)
}

// Reconcile corrects planned restarts written off as missed although the game
// server reported starting right after them (the bridge's server.start in the
// events table). Older versions only learnt of a restart from the status poll
// and missed every fast relaunch. Safe to run repeatedly.
func (s *Service) Reconcile(ctx context.Context) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, server_id, planned_for FROM restart_events WHERE kind='missed' AND planned_for IS NOT NULL`)
	if err != nil {
		return
	}
	type missed struct {
		id              int64
		server, planned string
	}
	var list []missed
	for rows.Next() {
		var m missed
		if rows.Scan(&m.id, &m.server, &m.planned) == nil {
			list = append(list, m)
		}
	}
	rows.Close()
	fixed := map[string]bool{}
	for _, m := range list {
		p, err := time.Parse(time.RFC3339, m.planned)
		if err != nil {
			continue
		}
		var startTS string
		if s.DB.QueryRowContext(ctx, `SELECT ts FROM events WHERE server_id=? AND type='server.start' AND ts >= ? AND ts <= ? ORDER BY ts LIMIT 1`,
			m.server, p.UTC().Format(time.RFC3339), p.Add(startLookback).UTC().Format(time.RFC3339)).Scan(&startTS) != nil {
			continue
		}
		up, err := time.Parse(time.RFC3339Nano, startTS)
		if err != nil {
			continue
		}
		up = up.UTC().Truncate(time.Second)
		if _, err := s.DB.ExecContext(ctx, `UPDATE restart_events SET kind='planned', outcome='ok', down_at=?, up_at=?, duration_s=? WHERE id=? AND kind='missed'`,
			p.UTC().Format(time.RFC3339), up.Format(time.RFC3339), int64(up.Sub(p).Seconds()), m.id); err == nil {
			fixed[m.server] = true
		}
	}
	for id := range fixed {
		s.Log.Info("restart history: planned restarts recorded from server starts", "server_id", id)
		s.refreshCaps(ctx, s.Load(ctx, id))
	}
	if len(fixed) > 0 {
		s.publish()
	}
}

func (s *Service) recorded(ctx context.Context, serverID string) {
	s.refreshCaps(ctx, s.Load(ctx, serverID))
	s.publish()
}
