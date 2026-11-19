package restarts_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kproios/dz-manager/internal/apptest"
	"github.com/kproios/dz-manager/internal/profiles"
	"github.com/kproios/dz-manager/internal/restarts"
)

var athens, _ = time.LoadLocation("Europe/Athens")

func sched(times ...string) restarts.Schedule {
	s := restarts.Defaults("srv", "Europe/Athens")
	s.Times, s.Enabled = times, true
	return s
}

func TestNextOccurrence(t *testing.T) {
	s := sched("00:00", "04:00", "08:00", "12:00", "16:00", "20:00")
	cases := []struct{ now, want string }{
		{"2026-11-04T10:59:00+02:00", "2026-11-04T12:00:00+02:00"},
		{"2026-11-04T23:59:30+02:00", "2026-11-05T00:00:00+02:00"},
		{"2026-11-04T20:00:00+02:00", "2026-11-05T00:00:00+02:00"}, // strictly after
		// Late startup at 13:37 still targets 16:00 (no catch-up).
		{"2026-11-04T13:37:00+02:00", "2026-11-04T16:00:00+02:00"},
	}
	for _, c := range cases {
		now, _ := time.Parse(time.RFC3339, c.now)
		got, ok := s.Next(now)
		want, _ := time.Parse(time.RFC3339, c.want)
		if !ok || !got.Equal(want) {
			t.Errorf("Next(%s) = %v, want %v", c.now, got, want)
		}
	}
}

func TestDST(t *testing.T) {
	// EU spring forward 2027-03-28: 03:00 local → 04:00. 03:30 does not exist.
	s := sched("02:00", "03:30", "08:00")
	from := time.Date(2027, 3, 28, 0, 0, 0, 0, athens)
	occ := s.Occurrences(from, from.Add(24*time.Hour))
	if len(occ) != 3 {
		t.Fatalf("spring occurrences: %v", occ)
	}
	if got := occ[1].In(athens).Format("15:04 MST"); got != "04:30 EEST" {
		t.Fatalf("skipped 03:30 normalised to %s, want 04:30 EEST", got)
	}
	if d := occ[2].Sub(occ[0]); d != 5*time.Hour { // 02:00 EET → 08:00 EEST is 5 real hours
		t.Fatalf("02:00→08:00 across spring = %v", d)
	}
	// Fall back 2026-10-25: 04:00 EEST → 03:00 EET, so 03:30 happens twice; we fire once.
	s = sched("03:30", "12:00")
	from = time.Date(2026, 10, 25, 0, 0, 0, 0, athens)
	occ = s.Occurrences(from, from.Add(24*time.Hour))
	if len(occ) != 2 || occ[0].In(athens).Format("15:04 MST") != "03:30 EEST" {
		t.Fatalf("fall occurrences: %v", occ)
	}
	if d := occ[1].Sub(occ[0]); d != 9*time.Hour+30*time.Minute {
		t.Fatalf("03:30 EEST → 12:00 EET = %v", d)
	}
}

func TestValidate(t *testing.T) {
	bad := [][]string{{}, {"24:00"}, {"8:00"}, {"08:00", "08:00"}, {"08:00", "08:10"}}
	for _, ts := range bad {
		s := sched(ts...)
		if err := s.Validate(); err == nil {
			t.Errorf("times %v accepted", ts)
		}
	}
	s := sched("20:00", "08:00")
	if err := s.Validate(); err != nil || s.Times[0] != "08:00" {
		t.Fatalf("sort/normalise: %v %v", err, s.Times)
	}
	s.Announce, s.AnnounceText = true, "no placeholder"
	if err := s.Validate(); err == nil {
		t.Fatal("announce text without {m} accepted")
	}
}

func TestValidateLockKick(t *testing.T) {
	s := sched("12:00")
	s.LockMinutes, s.KickMinutes = 5, 10
	if err := s.Validate(); err == nil {
		t.Fatal("kick before lock accepted")
	}
	s.LockMinutes, s.KickMinutes, s.KickMessage = 5, 2, "  "
	if err := s.Validate(); err != nil || s.KickMessage != restarts.DefaultKickMessage {
		t.Fatalf("defaults: %v %q", err, s.KickMessage)
	}
	s.LockMinutes = 31
	if err := s.Validate(); err == nil {
		t.Fatal("lock 31 accepted")
	}
	s.LockMinutes, s.AnnounceMinutes = 5, []int{1, 15, 5, 15}
	if err := s.Validate(); err != nil || len(s.AnnounceMinutes) != 3 || s.AnnounceMinutes[0] != 15 {
		t.Fatalf("announce normalise: %v %v", err, s.AnnounceMinutes)
	}
	s.AnnounceText = "Restart at {time} (in {m} min)"
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, athens)
	if got := s.Announcement(5, at.UTC()); got != "Restart at 12:00 (in 5 min)" {
		t.Fatalf("announcement %q", got)
	}
}

func TestProviderLockKickAndUnlockWhenMissed(t *testing.T) {
	e, svc, c, sent, id := setup(t)
	ctx := context.Background()
	var kicked []string
	var locks []bool
	svc.KickAll = func(_ context.Context, reason string) (int, error) { kicked = append(kicked, reason); return 3, nil }
	svc.OnLock = func(v bool) { locks = append(locks, v) }
	sc := sched("12:00")
	sc.ServerID, sc.LockMinutes, sc.KickMinutes, sc.KickMessage = id, 5, 1, "Restarting, back soon"
	if err := svc.Save(ctx, sc); err != nil {
		t.Fatal(err)
	}
	svc.Observe(ctx, id, true)
	// The provider never restarts: the window closes and DZ Manager unlocks again.
	for at := time.Date(2026, 11, 4, 11, 50, 0, 0, athens); at.Before(time.Date(2026, 11, 4, 12, 5, 0, 0, athens)); at = at.Add(10 * time.Second) {
		c.set(at)
		svc.Tick(ctx)
	}
	got := strings.Join(*sent, "|")
	if !strings.HasPrefix(got, "#lock|say -1 Server locked for the restart at 12:00.") || !strings.HasSuffix(got, "#unlock") {
		t.Fatalf("sent: %s", got)
	}
	if strings.Contains(got, "#shutdown") {
		t.Fatal("provider mode sent #shutdown")
	}
	if len(kicked) != 1 || kicked[0] != "Restarting, back soon" {
		t.Fatalf("kicked: %v", kicked)
	}
	if len(locks) != 2 || !locks[0] || locks[1] {
		t.Fatalf("lock states: %v", locks)
	}
	var n int
	e.App.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action IN ('restart.lock','restart.kick_all','restart.unlock')`).Scan(&n)
	if n != 3 {
		t.Fatalf("audit rows = %d", n)
	}
}

func TestCountdownRestartAndCancel(t *testing.T) {
	e, svc, c, sent, id := setup(t)
	ctx := context.Background()
	var kicks int
	svc.KickAll = func(context.Context, string) (int, error) { kicks++; return 1, nil }
	sc := sched("20:00")
	sc.ServerID, sc.LockMinutes, sc.KickMinutes = id, 2, 1
	if err := svc.Save(ctx, sc); err != nil {
		t.Fatal(err)
	}
	e.JSON("POST", "/api/restarts/drive-verification", map[string]any{"verified": true, "evidence": "test provider relaunches"}, nil)
	svc.Observe(ctx, id, true)
	if code := e.JSON("POST", "/api/restarts/now", map[string]any{"confirm": "RESTART", "delay_minutes": 7}, nil); code != 400 {
		t.Fatalf("unsupported delay accepted: %d", code)
	}
	start := c.now()
	if code := e.JSON("POST", "/api/restarts/now", map[string]any{"confirm": "RESTART", "delay_minutes": 5}, nil); code != 202 {
		t.Fatalf("countdown: %d", code)
	}
	if len(*sent) != 1 || (*sent)[0] != "say -1 Server restart in 5 minute(s)." {
		t.Fatalf("first warning: %v", *sent)
	}
	if code := e.JSON("POST", "/api/restarts/now", map[string]any{"confirm": "RESTART", "delay_minutes": 1}, nil); code != 409 {
		t.Fatal("second countdown accepted")
	}
	for at := start; !at.After(start.Add(5*time.Minute + 20*time.Second)); at = at.Add(10 * time.Second) {
		c.set(at)
		svc.Tick(ctx)
	}
	got := strings.Join(*sent, "|")
	if !strings.Contains(got, "#lock") || !strings.HasSuffix(got, "#shutdown") || kicks != 1 {
		t.Fatalf("countdown sequence: %s (kicks %d)", got, kicks)
	}
	if _, pending := svc.PendingManual(ctx, id); pending {
		t.Fatal("countdown still pending after shutdown")
	}
	// A new countdown can be cancelled; the lock it applied is released.
	svc.Observe(ctx, id, false)
	svc.Observe(ctx, id, false)
	svc.Observe(ctx, id, true)
	*sent = nil
	start = c.now().Add(10 * time.Minute)
	c.set(start)
	e.App.DB.Exec(`DELETE FROM settings WHERE key LIKE 'restart_now_until:%'`)
	if code := e.JSON("POST", "/api/restarts/now", map[string]any{"confirm": "RESTART", "delay_minutes": 2}, nil); code != 202 {
		t.Fatalf("second countdown: %d", code)
	}
	c.set(start.Add(10 * time.Second))
	svc.Tick(ctx)
	if code := e.JSON("POST", "/api/restarts/cancel", nil, nil); code != 200 {
		t.Fatalf("cancel: %d", code)
	}
	got = strings.Join(*sent, "|")
	if !strings.Contains(got, "#lock") || !strings.Contains(got, "#unlock") || strings.Contains(got, "#shutdown") {
		t.Fatalf("cancel sequence: %s", got)
	}
}

func TestSkipNextRestart(t *testing.T) {
	e, svc, c, sent, id := setup(t)
	ctx := context.Background()
	sc := sched("12:00", "16:00")
	sc.ServerID, sc.Mode, sc.DryRun, sc.Announce = id, restarts.ModeDrive, false, true
	if err := svc.Save(ctx, sc); err != nil {
		t.Fatal(err)
	}
	e.JSON("POST", "/api/restarts/drive-verification", map[string]any{"verified": true, "evidence": "test"}, nil)
	svc.Observe(ctx, id, true)
	if code := e.JSON("POST", "/api/restarts/skip", map[string]bool{"skip": true}, nil); code != 200 {
		t.Fatalf("skip: %d", code)
	}
	for at := time.Date(2026, 11, 4, 11, 40, 0, 0, athens); at.Before(time.Date(2026, 11, 4, 12, 6, 0, 0, athens)); at = at.Add(10 * time.Second) {
		c.set(at)
		svc.Tick(ctx)
	}
	if len(*sent) != 0 {
		t.Fatalf("skipped restart still sent: %v", *sent)
	}
	if evs, _ := svc.Events(ctx, id, 10); len(evs) != 0 {
		t.Fatalf("skipped restart reported: %+v", evs)
	}
	if _, ok := svc.SkippedAt(ctx, id); ok {
		t.Fatal("skip not cleared after it passed")
	}
	// The 16:00 restart runs normally.
	for at := time.Date(2026, 11, 4, 15, 59, 0, 0, athens); at.Before(time.Date(2026, 11, 4, 16, 0, 20, 0, athens)); at = at.Add(10 * time.Second) {
		c.set(at)
		svc.Tick(ctx)
	}
	if got := strings.Join(*sent, "|"); !strings.HasSuffix(got, "#shutdown") {
		t.Fatalf("16:00 restart: %s", got)
	}
}

// A skip pressed after warnings and the lock went out stops everything that is
// left, unlocks the server and tells players; no kick, no shutdown.
func TestSkipDuringCountdown(t *testing.T) {
	e, svc, c, sent, id := setup(t)
	ctx := context.Background()
	var kicks int
	svc.KickAll = func(context.Context, string) (int, error) { kicks++; return 1, nil }
	sc := sched("12:00", "16:00")
	sc.ServerID, sc.Mode, sc.DryRun, sc.Announce, sc.LockMinutes, sc.KickMinutes = id, restarts.ModeDrive, false, true, 5, 2
	if err := svc.Save(ctx, sc); err != nil {
		t.Fatal(err)
	}
	e.JSON("POST", "/api/restarts/drive-verification", map[string]any{"verified": true, "evidence": "test"}, nil)
	svc.Observe(ctx, id, true)
	tick := func(from, to time.Time) {
		for at := from; at.Before(to); at = at.Add(10 * time.Second) {
			c.set(at)
			svc.Tick(ctx)
		}
	}
	tick(time.Date(2026, 11, 4, 11, 40, 0, 0, athens), time.Date(2026, 11, 4, 11, 56, 0, 0, athens))
	if got := strings.Join(*sent, "|"); !strings.Contains(got, "#lock") {
		t.Fatalf("countdown did not start: %s", got)
	}
	*sent = nil
	if code := e.JSON("POST", "/api/restarts/skip", map[string]bool{"skip": true}, nil); code != 200 {
		t.Fatalf("skip: %d", code)
	}
	tick(time.Date(2026, 11, 4, 11, 56, 0, 0, athens), time.Date(2026, 11, 4, 12, 7, 0, 0, athens))
	got := strings.Join(*sent, "|")
	if !strings.HasPrefix(got, "#unlock|say -1 The 12:00 restart is cancelled.") || strings.Contains(got, "#shutdown") || strings.Contains(got, "minute") || kicks != 0 {
		t.Fatalf("after skip: %q kicks=%d", got, kicks)
	}
	if evs, _ := svc.Events(ctx, id, 10); len(evs) != 0 {
		t.Fatalf("skipped restart reported: %+v", evs)
	}
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) set(t time.Time) { c.mu.Lock(); c.t = t; c.mu.Unlock() }

func setup(t *testing.T) (*apptest.Env, *restarts.Service, *clock, *[]string, string) {
	e := apptest.New(t, "")
	e.Login()
	var p profiles.Profile
	e.JSON("POST", "/api/profiles", profiles.Input{Name: "TestServer"}, &p)
	svc := e.App.Restarts
	c := &clock{t: time.Date(2026, 11, 4, 11, 0, 0, 0, athens)}
	svc.Now = c.now
	var sent []string
	var mu sync.Mutex
	svc.Exec = func(ctx context.Context, cmd string) error {
		mu.Lock()
		sent = append(sent, cmd)
		mu.Unlock()
		return nil
	}
	return e, svc, c, &sent, p.ID
}

func TestDetectorPlannedUnplannedNotBack(t *testing.T) {
	e, svc, c, _, id := setup(t)
	ctx := context.Background()
	sc := sched("12:00", "16:00")
	sc.ServerID = id
	if err := svc.Save(ctx, sc); err != nil {
		t.Fatal(err)
	}
	obs := func(at time.Time, online bool) { c.set(at); svc.Observe(ctx, id, online) }
	base := time.Date(2026, 11, 4, 11, 58, 0, 0, athens)
	obs(base, true)
	obs(base.Add(2*time.Minute), false)                // one blip is not a restart
	obs(base.Add(2*time.Minute+10*time.Second), false) // second consecutive → down (planned)
	obs(base.Add(4*time.Minute), true)                 // back
	// Crash at 14:10, never comes back within timeout.
	crash := time.Date(2026, 11, 4, 14, 10, 0, 0, athens)
	obs(crash, false)
	obs(crash.Add(10*time.Second), false)
	c.set(crash.Add(11 * time.Minute))
	svc.Tick(ctx)
	evs, _ := svc.Events(ctx, id, 10)
	if len(evs) != 2 {
		t.Fatalf("events: %+v", evs)
	}
	if evs[1].Kind != "planned" || evs[1].Outcome != "ok" || evs[1].PlannedFor == nil || *evs[1].DurationS != 110 {
		t.Fatalf("planned event: %+v", evs[1])
	}
	if evs[0].Kind != "unplanned" || evs[0].Outcome != "not_back" {
		t.Fatalf("crash event: %+v", evs[0])
	}
	obs(crash.Add(12*time.Minute), true)
	evs, _ = svc.Events(ctx, id, 10)
	if evs[0].Outcome != "recovered_late" {
		t.Fatalf("recovery: %+v", evs[0])
	}
	if !e.App.Caps.OK(id, "provider.schedule") {
		t.Fatalf("provider.schedule should be ok after an observed planned restart: %+v", e.App.Caps.Get(id, "provider.schedule"))
	}
}

// A "Restart now" that comes back on its own proves the provider relaunches a
// cleanly exited server: both restart badges turn ok without a manual flag.
func TestManualRestartVerifiesDrive(t *testing.T) {
	e, svc, c, _, id := setup(t)
	ctx := context.Background()
	sc := sched("12:00")
	sc.ServerID = id
	if err := svc.Save(ctx, sc); err != nil {
		t.Fatal(err)
	}
	if e.App.Caps.OK(id, "restart.drive") {
		t.Fatal("restart.drive ok before any evidence")
	}
	at := time.Date(2026, 11, 4, 15, 0, 0, 0, athens)
	e.App.DB.Exec(`INSERT INTO settings(key,value) VALUES(?,?)`, "restart_now_until:"+id, fmt.Sprint(at.Add(5*time.Minute).Unix()))
	obs := func(t time.Time, online bool) { c.set(t); svc.Observe(ctx, id, online) }
	obs(at, true)
	obs(at.Add(time.Minute), false)
	obs(at.Add(time.Minute+10*time.Second), false)
	obs(at.Add(3*time.Minute), true)
	evs, _ := svc.Events(ctx, id, 10)
	if len(evs) != 1 || evs[0].Kind != "manual" || evs[0].Outcome != "ok" {
		t.Fatalf("events: %+v", evs)
	}
	if !e.App.Caps.OK(id, "restart.drive") || !e.App.Caps.OK(id, "provider.schedule") {
		t.Fatalf("caps: drive %+v, schedule %+v", e.App.Caps.Get(id, "restart.drive"), e.App.Caps.Get(id, "provider.schedule"))
	}
}

func TestMissedPlanned(t *testing.T) {
	_, svc, c, _, id := setup(t)
	ctx := context.Background()
	sc := sched("12:00")
	sc.ServerID = id
	svc.Save(ctx, sc)
	svc.Observe(ctx, id, true)
	for at := time.Date(2026, 11, 4, 12, 2, 0, 0, athens); at.Before(time.Date(2026, 11, 4, 12, 5, 0, 0, athens)); at = at.Add(10 * time.Second) {
		c.set(at)
		svc.Tick(ctx)
	}
	evs, _ := svc.Events(ctx, id, 10)
	if len(evs) != 1 || evs[0].Kind != "missed" {
		t.Fatalf("missed: %+v", evs)
	}
}

func TestAnnouncementsAndDriveDryRun(t *testing.T) {
	e, svc, c, sent, id := setup(t)
	ctx := context.Background()
	sc := sched("12:00")
	sc.ServerID, sc.Announce, sc.Mode = id, true, restarts.ModeDrive
	if err := svc.Save(ctx, sc); err != nil {
		t.Fatal(err)
	}
	for at := time.Date(2026, 11, 4, 11, 40, 0, 0, athens); at.Before(time.Date(2026, 11, 4, 12, 1, 0, 0, athens)); at = at.Add(10 * time.Second) {
		c.set(at)
		svc.Tick(ctx)
	}
	want := "say -1 Server restart in 15 minute(s).|say -1 Server restart in 10 minute(s).|say -1 Server restart in 5 minute(s).|say -1 Server restart in 1 minute(s)."
	if got := strings.Join(*sent, "|"); got != want {
		t.Fatalf("sent:\n%s\nwant\n%s", got, want)
	}
	var n int
	e.App.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='restart.drive_dryrun'`).Scan(&n)
	if n != 2 {
		t.Fatalf("dry-run lock+shutdown audit rows = %d", n)
	}
	// Late start: no catch-up announcements for past slots.
	*sent = nil
	c.set(time.Date(2026, 11, 4, 15, 52, 0, 0, athens))
	sc.Times = []string{"16:00"}
	svc.Save(ctx, sc)
	svc.Tick(ctx)
	if len(*sent) != 0 {
		t.Fatalf("caught up on past announcements: %v", *sent)
	}
	// API.
	var out map[string]any
	if code := e.JSON("GET", "/api/restarts", nil, &out); code != 200 || out["next"] == nil {
		t.Fatalf("api: %d %v", code, out)
	}
	if code := e.JSON("POST", "/api/restarts/drive-verification", map[string]any{"verified": true}, nil); code != 400 {
		t.Fatal("verification without evidence accepted")
	}
}
