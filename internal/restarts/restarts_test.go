package restarts_test

import (
	"context"
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
