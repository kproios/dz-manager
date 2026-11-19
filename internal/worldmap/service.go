package worldmap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // local files may be JPEG too
	"image/png"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/bridge"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/profiles"
	"github.com/kproios/dz-manager/internal/store"
)

// Place is a named location from CfgWorlds <world> Names.
type Place struct {
	Name string `json:"name"`
	// Local is the name as the game's own map shows it, when it differs
	// (Cyrillic on Chernarus).
	Local string  `json:"local_name,omitempty"`
	Kind  string  `json:"kind"` // Capital, City, Village, Local, Marine, Hill, Camp, Ruin, ViewPoint…
	X     float64 `json:"x"`
	Z     float64 `json:"z"`
}

// Info is what the panel knows about a server's world.
type Info struct {
	World      string  `json:"world"`
	Label      string  `json:"label"`
	Size       float64 `json:"size"`
	Places     []Place `json:"places"`
	Source     string  `json:"source"` // bridge | manual | default
	ReportedAt string  `json:"reported_at,omitempty"`
}

// Job is the state of the one install that may run at a time.
type Job struct {
	State      string `json:"state"` // idle | downloading | decoding | tiling | done | error
	World      string `json:"world,omitempty"`
	Source     string `json:"source,omitempty"`
	Bytes      int64  `json:"bytes,omitempty"`
	Total      int64  `json:"total,omitempty"`
	Tiles      int    `json:"tiles,omitempty"`
	TilesTotal int    `json:"tiles_total,omitempty"`
	Error      string `json:"error,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// Service serves /api/map/world, /api/map/sources and /api/map/install.
type Service struct {
	DB       *store.DB
	Profiles *profiles.Store
	Audit    *audit.Log
	Log      *slog.Logger
	DataDir  string
	// MaxPx refuses source images wider than this (decoding needs 4 bytes/px).
	MaxPx int
	// Publish notifies the UI (SSE) that world info or the install job changed.
	Publish func(kind string, v any)
	HTTP    *http.Client

	mu      sync.Mutex
	pending map[string]*pendingPlaces
	job     Job
	srcAt   time.Time
	sources []Source
}

// Init sets defaults.
func (s *Service) Init() {
	if s.MaxPx == 0 {
		s.MaxPx = 10000
	}
	if s.HTTP == nil {
		s.HTTP = &http.Client{Timeout: 15 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Host != downloadHost && req.URL.Host != "tools.dzhosts.com" {
				return errors.New("redirect to another host refused")
			}
			if len(via) > 3 {
				return errors.New("too many redirects")
			}
			return nil
		}}
	}
	s.job = Job{State: "idle"}
}

// Attach listens for the bridge's world report: world.info (name, label,
// size, how many place chunks follow) then world.places chunks (an event may
// carry at most 16 KB, and a map has hundreds of names).
func (s *Service) Attach(b *bridge.Bridge) {
	b.Subscribe(s.onWorldInfo, "world.info")
	b.Subscribe(s.onWorldPlaces, "world.places")
}

func key(serverID string) string       { return "world:" + serverID }
func manualKey(serverID string) string { return "world_manual:" + serverID }

type pendingPlaces struct {
	snapshot string
	parts    int
	got      map[int][]Place
}

// wirePlace is the compact form the bridge sends: n = the name the game shows
// (Cyrillic on Chernarus), c = the config class (Settlement_Chernogorsk).
type wirePlace struct {
	N string  `json:"n"`
	C string  `json:"c"`
	K string  `json:"k"`
	X float64 `json:"x"`
	Z float64 `json:"z"`
}

// LatinName turns a CfgWorlds place class into a readable Latin-script name:
// Settlement_Chernogorsk → "Chernogorsk", Hill_SvyatoyRoman → "Svyatoy Roman",
// Local_AF_Balota → "AF Balota". Numbered classes (ViewPoint_17) give "".
func LatinName(class string) string {
	_, rest, ok := strings.Cut(class, "_")
	if !ok || rest == "" {
		return ""
	}
	if strings.Trim(rest, "0123456789_") == "" {
		return ""
	}
	var b strings.Builder
	prev := rune(0)
	for _, r := range strings.ReplaceAll(rest, "_", " ") {
		if r >= 'A' && r <= 'Z' && prev >= 'a' && prev <= 'z' {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
		prev = r
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func (s *Service) save(ctx context.Context, serverID string, info Info) {
	b, _ := json.Marshal(info)
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key(serverID), string(b))
	if s.Publish != nil {
		s.Publish("world", map[string]string{"server_id": serverID, "world": info.World})
	}
}

func (s *Service) onWorldInfo(ctx context.Context, ev bridge.Event) {
	var d struct {
		World    string  `json:"world"`
		Label    string  `json:"label"`
		Size     float64 `json:"size"`
		Snapshot string  `json:"snapshot"`
		Parts    int     `json:"parts"`
	}
	if json.Unmarshal(ev.Data, &d) != nil {
		return
	}
	w, ok := NormalizeWorld(d.World)
	if !ok || math.IsNaN(d.Size) || d.Size < 1000 || d.Size > 100000 || d.Parts < 0 || d.Parts > 100 || len(d.Snapshot) > 80 {
		return
	}
	label := strings.TrimSpace(d.Label)
	if label == "" || len(label) > 60 || strings.HasPrefix(label, "$") || strings.HasPrefix(label, "#") {
		label = KnownLabel(w)
	}
	// Keep the previous place names until the new set is complete (same world only).
	old := s.For(ctx, ev.ServerID)
	places := []Place{}
	if old.Source == "bridge" && old.World == w {
		places = old.Places
	}
	s.save(ctx, ev.ServerID, Info{World: w, Label: label, Size: d.Size, Places: places, Source: "bridge", ReportedAt: ev.Time.UTC().Format(time.RFC3339)})
	s.mu.Lock()
	if s.pending == nil {
		s.pending = map[string]*pendingPlaces{}
	}
	s.pending[ev.ServerID] = &pendingPlaces{snapshot: d.Snapshot, parts: d.Parts, got: map[int][]Place{}}
	s.mu.Unlock()
}

func (s *Service) onWorldPlaces(ctx context.Context, ev bridge.Event) {
	var d struct {
		Snapshot string      `json:"snapshot"`
		Part     int         `json:"part"`
		Places   []wirePlace `json:"places"`
	}
	if json.Unmarshal(ev.Data, &d) != nil {
		return
	}
	s.mu.Lock()
	p := s.pending[ev.ServerID]
	if p == nil || p.snapshot != d.Snapshot || d.Part < 0 || d.Part >= p.parts {
		s.mu.Unlock()
		return
	}
	list := make([]Place, 0, len(d.Places))
	for _, x := range d.Places {
		p := Place{Name: strings.TrimSpace(x.N), Kind: x.K, X: x.X, Z: x.Z}
		if latin := LatinName(x.C); latin != "" && latin != p.Name {
			p.Local, p.Name = p.Name, latin
		}
		list = append(list, p)
	}
	p.got[d.Part] = list
	done := len(p.got) == p.parts
	var all []Place
	if done {
		for i := 0; i < p.parts; i++ {
			all = append(all, p.got[i]...)
		}
		delete(s.pending, ev.ServerID)
	}
	s.mu.Unlock()
	if !done {
		return
	}
	info := s.For(ctx, ev.ServerID)
	if info.Source != "bridge" {
		return
	}
	info.Places = cleanPlaces(all, info.Size)
	s.save(ctx, ev.ServerID, info)
}

func cleanPlaces(in []Place, size float64) []Place {
	out := make([]Place, 0, len(in))
	for _, p := range in {
		if p.Name == "" || len(p.Name) > 80 || strings.HasPrefix(p.Name, "$") || len(p.Kind) > 40 ||
			math.IsNaN(p.X) || math.IsNaN(p.Z) || p.X < 0 || p.Z < 0 || p.X > size || p.Z > size {
			continue
		}
		out = append(out, p)
		if len(out) >= 3000 {
			break
		}
	}
	return out
}

// For returns the world of a server: bridge report, manual choice, or default.
func (s *Service) For(ctx context.Context, serverID string) Info {
	var raw string
	if serverID != "" {
		if s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key(serverID)).Scan(&raw) == nil {
			var i Info
			if json.Unmarshal([]byte(raw), &i) == nil && i.World != "" && i.Size > 0 {
				if i.Places == nil {
					i.Places = []Place{}
				}
				return i
			}
		}
		if s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, manualKey(serverID)).Scan(&raw) == nil {
			var i Info
			if json.Unmarshal([]byte(raw), &i) == nil && i.World != "" && i.Size > 0 {
				i.Source, i.Places = "manual", []Place{}
				return i
			}
		}
	}
	return Info{World: DefaultWorld, Label: KnownLabel(DefaultWorld), Size: KnownSize(DefaultWorld), Places: []Place{}, Source: "default"}
}

// Active returns the world of the active server profile.
func (s *Service) Active(ctx context.Context) Info {
	id := ""
	if p, _, err := s.Profiles.Active(ctx); err == nil {
		id = p.ID
	}
	return s.For(ctx, id)
}

// Size returns the active world size (for coordinate checks).
func (s *Service) Size(ctx context.Context) float64 {
	return s.Active(ctx).Size
}

// Label returns the active world's display name.
func (s *Service) Label(ctx context.Context) string {
	return s.Active(ctx).Label
}

func actor(r *http.Request) audit.Actor {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.Actor()
	}
	return audit.System
}

// Register adds the API routes.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/map/world", func(w http.ResponseWriter, r *http.Request) {
		i := s.Active(r.Context())
		out := map[string]any{"world": i, "max_px": s.MaxPx}
		if m, err := ReadMeta(s.DataDir, i.World); err == nil {
			out["tiles"] = m
			out["tile_url"] = "/tiles/" + i.World + "/terrain/{z}/{x}/{y}.jpg?v=" + strings.NewReplacer(":", "", "-", "").Replace(m.InstalledAt)
		}
		httpx.JSON(w, 200, out)
	})
	mux.HandleFunc("PUT /api/map/world", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			World string  `json:"world"`
			Size  float64 `json:"size"`
			Label string  `json:"label"`
		}
		if err := httpx.Decode(r, 4096, &in); err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
		p, _, err := s.Profiles.Active(r.Context())
		if err != nil {
			httpx.Error(w, 409, "no active server profile")
			return
		}
		if strings.TrimSpace(in.World) == "" {
			_, _ = s.DB.ExecContext(r.Context(), `DELETE FROM settings WHERE key=?`, manualKey(p.ID))
			_ = s.Audit.Write(r.Context(), actor(r), "map.world_manual", p.ID, nil, "ok", "cleared")
			httpx.JSON(w, 200, s.For(r.Context(), p.ID))
			return
		}
		world, ok := NormalizeWorld(in.World)
		if !ok {
			httpx.Error(w, 400, "world must be the CfgWorlds name (letters, digits, underscore), e.g. chernarusplus or enoch")
			return
		}
		if in.Size == 0 {
			in.Size = KnownSize(world)
		}
		if in.Size < 1000 || in.Size > 100000 {
			httpx.Error(w, 400, "size must be the world size in metres (1000–100000)")
			return
		}
		label := strings.TrimSpace(in.Label)
		if label == "" || len(label) > 60 {
			label = KnownLabel(world)
		}
		b, _ := json.Marshal(Info{World: world, Label: label, Size: in.Size})
		_, _ = s.DB.ExecContext(r.Context(), `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, manualKey(p.ID), string(b))
		_ = s.Audit.Write(r.Context(), actor(r), "map.world_manual", p.ID, map[string]any{"world": world, "size": in.Size}, "ok", "")
		if s.Publish != nil {
			s.Publish("world", map[string]string{"server_id": p.ID, "world": world})
		}
		httpx.JSON(w, 200, s.For(r.Context(), p.ID))
	})
	mux.HandleFunc("GET /api/map/sources", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Sources(r.Context(), r.URL.Query().Get("refresh") == "1")
		if err != nil {
			httpx.Error(w, 502, err.Error())
			return
		}
		slugs := make([]string, 0, len(list))
		for _, x := range list {
			slugs = append(slugs, x.Slug)
		}
		httpx.JSON(w, 200, map[string]any{"list_url": ListURL, "sources": list, "suggested": SuggestSlug(s.Active(r.Context()).World, slugs), "max_px": s.MaxPx})
	})
	mux.HandleFunc("GET /api/map/install", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, s.Job())
	})
	mux.HandleFunc("POST /api/map/install", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			URL string `json:"url"`
		}
		if err := httpx.Decode(r, 4096, &in); err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
		if err := CheckDownloadURL(in.URL); err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
		// Only URLs from the current list: the pixel size must be known and allowed.
		list, err := s.Sources(r.Context(), false)
		if err != nil {
			httpx.Error(w, 502, err.Error())
			return
		}
		var file *SourceFile
		for _, src := range list {
			for i := range src.Files {
				if src.Files[i].URL == in.URL {
					file = &src.Files[i]
				}
			}
		}
		if file == nil {
			httpx.Error(w, 400, "that image is not on the dzhosts.com map list")
			return
		}
		if !file.Fits {
			httpx.Error(w, 400, fmt.Sprintf("%d px is over this server's limit of %d px (DZM_MAP_MAX_PX); pick a smaller size", file.Px, s.MaxPx))
			return
		}
		world := s.Active(r.Context()).World
		if err := s.StartInstall(world, file.URL, file.Name); err != nil {
			httpx.Error(w, 409, err.Error())
			return
		}
		_ = s.Audit.Write(r.Context(), actor(r), "map.install", world, map[string]any{"url": file.URL}, "ok", "started")
		httpx.JSON(w, 202, s.Job())
	})
}

// Sources returns the dzhosts list, cached for an hour.
func (s *Service) Sources(ctx context.Context, refresh bool) ([]Source, error) {
	s.mu.Lock()
	if !refresh && s.sources != nil && time.Since(s.srcAt) < time.Hour {
		out := s.sources
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	list, err := FetchList(cctx, s.HTTP, s.MaxPx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.sources, s.srcAt = list, time.Now()
	s.mu.Unlock()
	return list, nil
}

// Job returns a copy of the install state.
func (s *Service) Job() Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.job
}

func (s *Service) update(f func(j *Job), notify bool) {
	s.mu.Lock()
	f(&s.job)
	j := s.job
	s.mu.Unlock()
	if notify && s.Publish != nil {
		s.Publish("map_install", j)
	}
}

// StartInstall downloads and tiles an image in the background.
func (s *Service) StartInstall(world, url, name string) error {
	s.mu.Lock()
	if st := s.job.State; st == "downloading" || st == "decoding" || st == "tiling" {
		s.mu.Unlock()
		return errors.New("a map install is already running")
	}
	s.job = Job{State: "downloading", World: world, Source: "dzhosts:" + strings.TrimSuffix(name, ".png"), StartedAt: time.Now().UTC().Format(time.RFC3339)}
	s.mu.Unlock()
	go s.runInstall(world, url)
	return nil
}

func (s *Service) runInstall(world, url string) {
	fail := func(err error) {
		if s.Log != nil {
			s.Log.Warn("map install failed", "world", world, "err", err)
		}
		s.update(func(j *Job) {
			j.State, j.Error, j.FinishedAt = "error", err.Error(), time.Now().UTC().Format(time.RFC3339)
		}, true)
	}
	tmpDir := filepath.Join(s.DataDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		fail(err)
		return
	}
	dst := filepath.Join(tmpDir, "map-download.png")
	defer os.Remove(dst)
	last := time.Time{}
	err := Download(context.Background(), s.HTTP, url, dst, 400<<20, func(done, total int64) {
		notify := time.Since(last) > time.Second
		if notify {
			last = time.Now()
		}
		s.update(func(j *Job) { j.Bytes, j.Total = done, total }, notify)
	})
	if err != nil {
		fail(err)
		return
	}
	src := s.Job().Source
	if _, err := s.InstallFile(world, dst, src, func(state string, done, total int) {
		notify := time.Since(last) > time.Second || done == total
		if notify {
			last = time.Now()
		}
		s.update(func(j *Job) { j.State, j.Tiles, j.TilesTotal = state, done, total }, notify || done == 0)
	}); err != nil {
		fail(err)
		return
	}
	s.update(func(j *Job) { j.State, j.FinishedAt = "done", time.Now().UTC().Format(time.RFC3339) }, true)
	if s.Audit != nil {
		_ = s.Audit.Write(context.Background(), audit.System, "map.install", world, map[string]any{"source": src}, "ok", "tiles installed")
	}
}

// InstallFile decodes a local PNG/JPEG and installs it as the world's tiles.
func (s *Service) InstallFile(world, path, source string, progress func(state string, done, total int)) (*Meta, error) {
	return InstallFile(s.DataDir, world, path, source, s.MaxPx, progress)
}

// InstallFile is the shared implementation (also used by `dzm map install`).
func InstallFile(dataDir, world, path, source string, maxPx int, progress func(state string, done, total int)) (*Meta, error) {
	if _, ok := NormalizeWorld(world); !ok {
		return nil, errors.New("invalid world name")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("not a PNG or JPEG image: %w", err)
	}
	if cfg.Width > maxPx || cfg.Height > maxPx {
		f.Close()
		return nil, fmt.Errorf("the image is %d×%d px, over the %d px limit (needs about %d MB of memory to tile)", cfg.Width, cfg.Height, maxPx, cfg.Width*cfg.Height*4>>20)
	}
	if r := float64(cfg.Width) / float64(cfg.Height); r < 0.9 || r > 1.1 {
		f.Close()
		return nil, fmt.Errorf("the image is %d×%d px; a DayZ terrain map is square", cfg.Width, cfg.Height)
	}
	if progress != nil {
		progress("decoding", 0, 0)
	}
	// The decoded image is one large allocation (4 bytes per pixel); collect
	// garbage eagerly while it is alive so the peak stays near that size
	// instead of twice it (the service may run under a memory limit).
	old := debug.SetGCPercent(10)
	defer debug.SetGCPercent(old)
	if _, err := f.Seek(0, 0); err != nil {
		f.Close()
		return nil, err
	}
	var img image.Image
	if format == "png" {
		img, err = png.Decode(f)
	} else {
		img, _, err = image.Decode(f)
	}
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("cannot decode the image: %w", err)
	}
	m, err := InstallImage(dataDir, world, source, img, func(done, total int) {
		if progress != nil {
			progress("tiling", done, total)
		}
	})
	img = nil
	debug.FreeOSMemory()
	return m, err
}
