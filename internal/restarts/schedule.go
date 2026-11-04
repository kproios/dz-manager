// Package restarts implements restart observation (#4). The provider
// (Architect / VYKIX panel) is the restart authority; DZ Manager mirrors the
// schedule, optionally announces, detects down/up transitions, classifies and
// alerts. The DZ-Manager-driven mode is off unless explicitly verified.
package restarts

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Modes.
const (
	ModeProvider = "provider" // provider schedule is the authority (default)
	ModeDrive    = "drive"    // DZ Manager sends #lock / #shutdown (needs restart.drive)
)

// Schedule is the mirrored restart plan for one server.
type Schedule struct {
	ServerID        string   `json:"server_id"`
	Enabled         bool     `json:"enabled"`
	Times           []string `json:"times"` // "HH:MM" local
	Timezone        string   `json:"timezone"`
	Mode            string   `json:"mode"`
	Announce        bool     `json:"announce"`
	AnnounceMinutes []int    `json:"announce_minutes"`
	AnnounceText    string   `json:"announce_text"`
	WindowMinutes   int      `json:"window_minutes"`  // planned-restart detection window ±
	TimeoutMinutes  int      `json:"timeout_minutes"` // alert if not back within
	DryRun          bool     `json:"dry_run"`         // drive mode: log only
	// LockMinutes locks the server this many minutes before a restart so nobody
	// new joins (0 = off; drive mode still locks one minute before).
	LockMinutes int `json:"lock_minutes"`
	// KickMinutes kicks every player this many minutes before a restart with
	// KickMessage, so they log out cleanly (0 = off).
	KickMinutes int    `json:"kick_minutes"`
	KickMessage string `json:"kick_message"`
}

// DefaultKickMessage is shown to kicked players when no message is set.
const DefaultKickMessage = "Server restarting now. Back in a few minutes."

// Announcement renders the announcement for m minutes before a restart at at.
// {m} is the minutes left and {time} the restart's local clock time.
func (s Schedule) Announcement(m int, at time.Time) string {
	text := s.AnnounceText
	if !strings.Contains(text, "{m}") {
		text = "Server restart in {m} minute(s)."
	}
	if loc, err := time.LoadLocation(s.Timezone); err == nil {
		at = at.In(loc)
	}
	return strings.NewReplacer("{m}", fmt.Sprint(m), "{time}", at.Format("15:04")).Replace(text)
}

// Defaults returns a disabled schedule with sensible defaults.
func Defaults(serverID, tz string) Schedule {
	return Schedule{ServerID: serverID, Times: []string{"00:00", "04:00", "08:00", "12:00", "16:00", "20:00"}, Timezone: tz,
		Mode: ModeProvider, AnnounceMinutes: []int{15, 10, 5, 1}, AnnounceText: "Server restart in {m} minute(s).",
		WindowMinutes: 3, TimeoutMinutes: 10, DryRun: true, KickMessage: DefaultKickMessage}
}

type hm struct{ h, m int }

func parseTimes(ts []string) ([]hm, error) {
	if len(ts) == 0 {
		return nil, errors.New("at least one time is required")
	}
	seen := map[string]bool{}
	out := make([]hm, 0, len(ts))
	for _, t := range ts {
		parts := strings.Split(strings.TrimSpace(t), ":")
		if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
			return nil, fmt.Errorf("time %q must be HH:MM", t)
		}
		h, e1 := strconv.Atoi(parts[0])
		m, e2 := strconv.Atoi(parts[1])
		if e1 != nil || e2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			return nil, fmt.Errorf("time %q out of range", t)
		}
		key := parts[0] + ":" + parts[1]
		if seen[key] {
			return nil, fmt.Errorf("duplicate time %s", key)
		}
		seen[key] = true
		out = append(out, hm{h, m})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].h*60+out[i].m < out[j].h*60+out[j].m })
	for i := 1; i < len(out); i++ {
		if (out[i].h*60+out[i].m)-(out[i-1].h*60+out[i-1].m) < 15 {
			return nil, errors.New("restart times must be at least 15 minutes apart")
		}
	}
	return out, nil
}

// Validate normalises and checks a schedule.
func (s *Schedule) Validate() error {
	ts, err := parseTimes(s.Times)
	if err != nil {
		return err
	}
	s.Times = s.Times[:0]
	for _, t := range ts {
		s.Times = append(s.Times, fmt.Sprintf("%02d:%02d", t.h, t.m))
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return fmt.Errorf("timezone %q: %w", s.Timezone, err)
	}
	switch s.Mode {
	case ModeProvider, ModeDrive:
	case "":
		s.Mode = ModeProvider
	default:
		return fmt.Errorf("mode must be %s or %s", ModeProvider, ModeDrive)
	}
	if len(s.AnnounceMinutes) > 12 {
		return errors.New("at most 12 announcement times")
	}
	seenMin := map[int]bool{}
	mins := make([]int, 0, len(s.AnnounceMinutes))
	for _, m := range s.AnnounceMinutes {
		if m < 1 || m > 60 {
			return errors.New("announce minutes must be 1..60")
		}
		if !seenMin[m] {
			seenMin[m] = true
			mins = append(mins, m)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(mins)))
	s.AnnounceMinutes = mins
	if s.LockMinutes < 0 || s.LockMinutes > 30 {
		return errors.New("lock_minutes must be 0..30")
	}
	if s.KickMinutes < 0 || s.KickMinutes > 10 {
		return errors.New("kick_minutes must be 0..10")
	}
	if s.KickMinutes > 0 && s.LockMinutes > 0 && s.KickMinutes > s.LockMinutes {
		return errors.New("kick players after locking: kick minutes must not be more than lock minutes")
	}
	s.KickMessage = strings.TrimSpace(s.KickMessage)
	if s.KickMessage == "" {
		s.KickMessage = DefaultKickMessage
	}
	if len([]rune(s.KickMessage)) > 120 || strings.IndexFunc(s.KickMessage, unicode.IsControl) >= 0 {
		return errors.New("kick_message must be one line of at most 120 characters")
	}
	if strings.IndexFunc(s.AnnounceText, unicode.IsControl) >= 0 {
		return errors.New("announce_text must be one line")
	}
	if s.WindowMinutes < 1 || s.WindowMinutes > 30 {
		return errors.New("window_minutes must be 1..30")
	}
	if s.TimeoutMinutes < 2 || s.TimeoutMinutes > 120 {
		return errors.New("timeout_minutes must be 2..120")
	}
	if len(s.AnnounceText) > 150 || (s.Announce && !strings.Contains(s.AnnounceText, "{m}")) {
		return errors.New("announce_text must contain {m} and be at most 150 characters")
	}
	return nil
}

// Occurrences returns planned restart instants in [from, to). DST rules:
// a local time skipped by spring-forward is normalised forward by Go (e.g.
// 03:30 → 04:30); an ambiguous fall-back time resolves to its first instant.
// Instants are de-duplicated.
func (s Schedule) Occurrences(from, to time.Time) []time.Time {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil
	}
	ts, err := parseTimes(s.Times)
	if err != nil {
		return nil
	}
	var out []time.Time
	seen := map[int64]bool{}
	d := from.In(loc)
	day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -1)
	for ; !day.After(to.In(loc)); day = day.AddDate(0, 0, 1) {
		for _, t := range ts {
			at := time.Date(day.Year(), day.Month(), day.Day(), t.h, t.m, 0, 0, loc)
			// Ambiguous (fall-back) wall time: Go may pick either instant; take the first.
			if alt := at.Add(-time.Hour).In(loc); alt.Hour() == t.h && alt.Minute() == t.m && alt.Day() == day.Day() {
				at = alt
			}
			if at.Before(from) || !at.Before(to) || seen[at.Unix()] {
				continue
			}
			seen[at.Unix()] = true
			out = append(out, at)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// Next returns the first planned restart strictly after now.
func (s Schedule) Next(now time.Time) (time.Time, bool) {
	occ := s.Occurrences(now.Add(time.Second), now.Add(49*time.Hour))
	if len(occ) == 0 {
		return time.Time{}, false
	}
	return occ[0], true
}

// Nearest returns the planned instant within ±window of t, if any.
func (s Schedule) Nearest(t time.Time) (time.Time, bool) {
	w := time.Duration(s.WindowMinutes) * time.Minute
	for _, o := range s.Occurrences(t.Add(-w), t.Add(w+time.Second)) {
		return o, true
	}
	return time.Time{}, false
}
