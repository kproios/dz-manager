// Package retention keeps the database small: rows older than the configured
// age are moved into gzip-compressed JSON-lines archives (one file per table
// per day) and deleted from SQLite; archives older than the keep period are
// removed. The audit log is append-only and never archived.
package retention

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/store"
)

// Fixed choices (days).
var (
	ArchiveAfter = []int{7, 14, 30, 60}
	KeepArchives = []int{90, 180, 365, 730}
)

const settingsKey = "retention"

// Settings are owner-editable.
type Settings struct {
	ArchiveAfterDays int `json:"archive_after_days"`
	KeepArchiveDays  int `json:"keep_archive_days"`
}

// table describes one archivable table: the time column and the condition
// that marks a row as finished (so live data is never archived).
type table struct {
	name, timeCol, done string
}

var tables = []table{
	{"events", "received_at", "1"},
	{"chat_messages", "ts", "1"},
	{"map_events", "COALESCE(ended_at, expires_at)", "status <> 'active'"},
	{"metricz_perf", "minute", "1"},
	{"bridge_commands", "created_at", "status IN ('ok','error','expired')"},
}

var reArchive = regexp.MustCompile(`^[a-z_]+-\d{4}-\d{2}-\d{2}(-\d+)?\.jsonl\.gz$`)

// Service runs the daily job.
type Service struct {
	DB      *store.DB
	Audit   *audit.Log
	Log     *slog.Logger
	DataDir string
	Now     func() time.Time

	mu sync.Mutex
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) dir() string { return filepath.Join(s.DataDir, "archive") }

// Load returns the settings (14 days in the database, archives kept a year).
func (s *Service) Load(ctx context.Context) Settings {
	st := Settings{ArchiveAfterDays: 14, KeepArchiveDays: 365}
	var raw string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, settingsKey).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &st)
	}
	return st
}

func oneOf(v int, set []int) bool {
	for _, x := range set {
		if x == v {
			return true
		}
	}
	return false
}

// Result summarises one run.
type Result struct {
	Archived map[string]int `json:"archived"`
	Removed  []string       `json:"removed_archives"`
	Vacuumed bool           `json:"vacuumed"`
}

// RunOnce archives old rows and prunes old archives.
func (s *Service) RunOnce(ctx context.Context) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.Load(ctx)
	res := Result{Archived: map[string]int{}}
	if err := os.MkdirAll(s.dir(), 0o750); err != nil {
		return res, err
	}
	now := s.now().UTC()
	cutoff := now.AddDate(0, 0, -st.ArchiveAfterDays)
	total := 0
	for _, t := range tables {
		n, err := s.archiveTable(ctx, t, cutoff, now)
		if err != nil {
			return res, fmt.Errorf("%s: %w", t.name, err)
		}
		if n > 0 {
			res.Archived[t.name] = n
			total += n
		}
	}
	keep := now.AddDate(0, 0, -st.KeepArchiveDays)
	entries, _ := os.ReadDir(s.dir())
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !reArchive.MatchString(e.Name()) || info.ModTime().After(keep) {
			continue
		}
		if os.Remove(filepath.Join(s.dir(), e.Name())) == nil {
			res.Removed = append(res.Removed, e.Name())
		}
	}
	if total > 20000 {
		if _, err := s.DB.ExecContext(ctx, `VACUUM`); err == nil {
			res.Vacuumed = true
		}
	}
	return res, nil
}

func timeFormat(t table) string {
	if t.name == "metricz_perf" {
		return "2006-01-02T15:04"
	}
	return time.RFC3339
}

// archiveTable writes rows older than cutoff to a new gzip file, then deletes them.
func (s *Service) archiveTable(ctx context.Context, t table, cutoff, now time.Time) (int, error) {
	where := fmt.Sprintf("%s < ? AND %s", t.timeCol, t.done)
	var n int
	c := cutoff.Format(timeFormat(t))
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+t.name+` WHERE `+where, c).Scan(&n); err != nil || n == 0 {
		return 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT rowid AS _rowid, * FROM `+t.name+` WHERE `+where+` ORDER BY rowid`, c)
	if err != nil {
		return 0, err
	}
	cols, _ := rows.Columns()
	name := fmt.Sprintf("%s-%s.jsonl.gz", t.name, now.Format("2006-01-02"))
	for i := 1; fileExists(filepath.Join(s.dir(), name)); i++ {
		name = fmt.Sprintf("%s-%s-%d.jsonl.gz", t.name, now.Format("2006-01-02"), i)
	}
	tmp := filepath.Join(s.dir(), name+".tmp")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		rows.Close()
		return 0, err
	}
	zw := gzip.NewWriter(f)
	enc := json.NewEncoder(zw)
	var maxRowID int64
	count := 0
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			rows.Close()
			f.Close()
			os.Remove(tmp)
			return 0, err
		}
		rec := map[string]any{}
		for i, col := range cols {
			v := vals[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			if col == "_rowid" {
				if id, ok := v.(int64); ok && id > maxRowID {
					maxRowID = id
				}
				continue
			}
			rec[col] = v
		}
		if err := enc.Encode(rec); err != nil {
			rows.Close()
			f.Close()
			os.Remove(tmp)
			return 0, err
		}
		count++
	}
	rows.Close()
	if err := zw.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return 0, err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, filepath.Join(s.dir(), name)); err != nil {
		return 0, err
	}
	// Only rows that were written are deleted.
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM `+t.name+` WHERE `+where+` AND rowid <= ?`, c, maxRowID); err != nil {
		return 0, err
	}
	return count, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Run executes the job a few minutes after start and then daily.
func (s *Service) Run(ctx context.Context) {
	wait := 5 * time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = 24 * time.Hour
		res, err := s.RunOnce(ctx)
		if err != nil {
			if s.Log != nil {
				s.Log.Error("retention", "err", err)
			}
			continue
		}
		if len(res.Archived) > 0 || len(res.Removed) > 0 {
			_ = s.Audit.Write(ctx, audit.System, "data.archive", "", map[string]any{"archived": res.Archived, "removed": len(res.Removed)}, "ok", "")
		}
	}
}

// Archive is one file.
type Archive struct {
	Name    string `json:"name"`
	Table   string `json:"table"`
	Bytes   int64  `json:"bytes"`
	Created string `json:"created"`
}

func actor(r *http.Request) audit.Actor {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.Actor()
	}
	return audit.System
}

// Register adds the API.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/retention", func(w http.ResponseWriter, r *http.Request) {
		var dbBytes int64
		_ = s.DB.QueryRowContext(r.Context(), `SELECT page_count*page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&dbBytes)
		counts := map[string]int64{}
		for _, t := range tables {
			var n int64
			if s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM `+t.name).Scan(&n) == nil {
				counts[t.name] = n
			}
		}
		httpx.JSON(w, 200, map[string]any{"settings": s.Load(r.Context()), "archive_after": ArchiveAfter, "keep_archives": KeepArchives, "db_bytes": dbBytes, "rows": counts, "archives": s.list()})
	})
	mux.HandleFunc("PUT /api/settings/retention", func(w http.ResponseWriter, r *http.Request) {
		var st Settings
		if err := httpx.Decode(r, 1024, &st); err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
		if !oneOf(st.ArchiveAfterDays, ArchiveAfter) || !oneOf(st.KeepArchiveDays, KeepArchives) {
			httpx.Error(w, 400, "choose one of the offered periods")
			return
		}
		b, _ := json.Marshal(st)
		if _, err := s.DB.ExecContext(r.Context(), `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, settingsKey, string(b)); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		_ = s.Audit.Write(r.Context(), actor(r), "settings.retention_update", "", st, "ok", "")
		httpx.JSON(w, 200, st)
	})
	mux.HandleFunc("POST /api/archives/run", func(w http.ResponseWriter, r *http.Request) {
		res, err := s.RunOnce(r.Context())
		if err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		_ = s.Audit.Write(r.Context(), actor(r), "data.archive", "", map[string]any{"archived": res.Archived, "removed": len(res.Removed)}, "ok", "run manually")
		httpx.JSON(w, 200, res)
	})
	mux.HandleFunc("GET /api/archives/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !reArchive.MatchString(name) {
			httpx.Error(w, 404, "not found")
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		http.ServeFile(w, r, filepath.Join(s.dir(), name))
	})
}

func (s *Service) list() []Archive {
	out := []Archive{}
	entries, _ := os.ReadDir(s.dir())
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !reArchive.MatchString(e.Name()) {
			continue
		}
		out = append(out, Archive{Name: e.Name(), Table: strings.SplitN(e.Name(), "-", 2)[0], Bytes: info.Size(), Created: info.ModTime().UTC().Format(time.RFC3339)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out
}
