// Package intel enriches players with public reputation signals: the Steam
// profile (avatar, name) and VAC / game / community bans, plus the country and
// VPN or hosting flags of the player's current IP. It is evidence for an admin
// to read; nothing here kicks or bans automatically.
//
// Sources: the public Steam community profile (?xml=1, no key) or, when the
// owner adds a Steam Web API key, ISteamUser/GetPlayerBans + GetPlayerSummaries;
// ip-api.com for IP data (free tier, non-commercial, HTTP only). BattlEye global
// bans are not publicly queryable: a BattlEye-banned player is refused by
// BattlEye itself. DayZ does not use EasyAntiCheat.
package intel

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/secret"
	"github.com/kproios/dz-manager/internal/store"
)

// Intel is the cached view of one player.
type Intel struct {
	Steam64         string `json:"steam64"`
	Avatar          string `json:"avatar,omitempty"`
	Persona         string `json:"persona,omitempty"`
	ProfilePrivate  bool   `json:"profile_private"`
	Limited         bool   `json:"limited"`
	VACBanned       bool   `json:"vac_banned"`
	VACBans         *int   `json:"vac_bans,omitempty"`
	GameBans        *int   `json:"game_bans,omitempty"`
	DaysSinceBan    *int   `json:"days_since_ban,omitempty"`
	CommunityBanned bool   `json:"community_banned"`
	TradeBan        string `json:"trade_ban,omitempty"`
	SteamCheckedAt  string `json:"steam_checked_at,omitempty"`
	SteamError      string `json:"steam_error,omitempty"`
	IP              string `json:"ip,omitempty"`
	HasIP           bool   `json:"has_ip"`
	Country         string `json:"country,omitempty"`
	CountryCode     string `json:"country_code,omitempty"`
	ISP             string `json:"isp,omitempty"`
	Proxy           bool   `json:"proxy"`
	Hosting         bool   `json:"hosting"`
	IPCheckedAt     string `json:"ip_checked_at,omitempty"`
}

// Seen is one online player as RCON reports it.
type Seen struct {
	BEGUID string
	IP     string
}

// Settings are owner-editable.
type Settings struct {
	IPLookup    bool   `json:"ip_lookup"`
	SteamKeyEnc string `json:"steam_key_enc,omitempty"`
}

const (
	settingsKey = "intel_settings"
	steamTTL    = 24 * time.Hour
	ipTTL       = 7 * 24 * time.Hour
)

var reSteam64 = regexp.MustCompile(`^7656119\d{10}$`)
var reSteamKey = regexp.MustCompile(`^[0-9A-Fa-f]{32}$`)

// Service caches and refreshes intel in the background.
type Service struct {
	DB      *store.DB
	Box     *secret.Box
	Audit   *audit.Log
	Log     *slog.Logger
	HTTP    *http.Client
	Now     func() time.Time
	Publish func()
	// Base URLs (overridden in tests).
	SteamCommunity, SteamAPI, IPAPI string

	mu           sync.Mutex
	pendingSteam map[string]bool
	pendingIP    map[string]string
	lastObserve  time.Time
}

// Init sets defaults.
func (s *Service) Init() {
	if s.HTTP == nil {
		s.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.SteamCommunity == "" {
		s.SteamCommunity = "https://steamcommunity.com"
	}
	if s.SteamAPI == "" {
		s.SteamAPI = "https://api.steampowered.com"
	}
	if s.IPAPI == "" {
		s.IPAPI = "http://ip-api.com"
	}
	s.pendingSteam, s.pendingIP = map[string]bool{}, map[string]string{}
}

// LoadSettings returns the stored settings (IP lookup on by default).
func (s *Service) LoadSettings(ctx context.Context) Settings {
	var raw string
	st := Settings{IPLookup: true}
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, settingsKey).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &st)
	}
	return st
}

func (s *Service) steamKey(ctx context.Context) string {
	st := s.LoadSettings(ctx)
	if st.SteamKeyEnc == "" || s.Box == nil {
		return ""
	}
	k, err := s.Box.OpenString(st.SteamKeyEnc, settingsKey)
	if err != nil {
		return ""
	}
	return k
}

// Observe records the online roster; lookups run in the background.
func (s *Service) Observe(ctx context.Context, seen []Seen) {
	s.mu.Lock()
	if s.Now().Sub(s.lastObserve) < 30*time.Second {
		s.mu.Unlock()
		return
	}
	s.lastObserve = s.Now()
	s.mu.Unlock()
	ipOn := s.LoadSettings(ctx).IPLookup
	for _, p := range seen {
		var steam string
		if err := s.DB.QueryRowContext(ctx, `SELECT steam64 FROM players WHERE be_guid=? LIMIT 1`, strings.ToLower(p.BEGUID)).Scan(&steam); err != nil {
			continue
		}
		in := s.Get(ctx, steam)
		s.mu.Lock()
		if stale(in.SteamCheckedAt, steamTTL, s.Now()) {
			s.pendingSteam[steam] = true
		}
		if ipOn && p.IP != "" && (in.IP != p.IP || stale(in.IPCheckedAt, ipTTL, s.Now())) {
			s.pendingIP[steam] = p.IP
		}
		s.mu.Unlock()
	}
}

func stale(at string, ttl time.Duration, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, at)
	return err != nil || now.Sub(t) > ttl
}

// Run processes queued lookups, respecting the free-tier rate limits.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		changed := s.flushIP(ctx)
		if s.flushSteam(ctx, 10) {
			changed = true
		}
		if changed && s.Publish != nil {
			s.Publish()
		}
	}
}

// Get returns the cached intel (zero values when unknown).
func (s *Service) Get(ctx context.Context, steam64 string) Intel {
	in := Intel{Steam64: steam64}
	var vac, game, days sql.NullInt64
	var steamAt, ipAt sql.NullString
	var priv, lim, vacB, comm, proxy, host int
	err := s.DB.QueryRowContext(ctx, `SELECT avatar,persona,profile_private,limited,vac_banned,vac_bans,game_bans,days_since_ban,community_banned,trade_ban,steam_checked_at,steam_error,ip,country,country_code,isp,proxy,hosting,ip_checked_at FROM player_intel WHERE steam64=?`, steam64).
		Scan(&in.Avatar, &in.Persona, &priv, &lim, &vacB, &vac, &game, &days, &comm, &in.TradeBan, &steamAt, &in.SteamError, &in.IP, &in.Country, &in.CountryCode, &in.ISP, &proxy, &host, &ipAt)
	if err != nil {
		return in
	}
	in.ProfilePrivate, in.Limited, in.VACBanned, in.CommunityBanned, in.Proxy, in.Hosting = priv == 1, lim == 1, vacB == 1, comm == 1, proxy == 1, host == 1
	if vac.Valid {
		v := int(vac.Int64)
		in.VACBans = &v
	}
	if game.Valid {
		v := int(game.Int64)
		in.GameBans = &v
	}
	if days.Valid {
		v := int(days.Int64)
		in.DaysSinceBan = &v
	}
	in.SteamCheckedAt, in.IPCheckedAt = steamAt.String, ipAt.String
	in.HasIP = in.IP != ""
	return in
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Service) ensureRow(ctx context.Context, steam64 string) {
	_, _ = s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO player_intel(steam64) VALUES(?)`, steam64)
}

// ---- Steam ----

type steamXML struct {
	SteamID   string `xml:"steamID"`
	Avatar    string `xml:"avatarMedium"`
	Privacy   string `xml:"privacyState"`
	VACBanned int    `xml:"vacBanned"`
	TradeBan  string `xml:"tradeBanState"`
	Limited   int    `xml:"isLimitedAccount"`
	Error     string `xml:"error"`
}

func (s *Service) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "dz-manager")
	res, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return b, nil
}

// RefreshSteam fetches Steam data for the given players now.
func (s *Service) RefreshSteam(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	now := s.Now().UTC().Format(time.RFC3339)
	if key := s.steamKey(ctx); key != "" {
		return s.refreshSteamAPI(ctx, key, ids, now)
	}
	var first error
	for _, id := range ids {
		s.ensureRow(ctx, id)
		b, err := s.get(ctx, s.SteamCommunity+"/profiles/"+url.PathEscape(id)+"/?xml=1")
		var x steamXML
		if err == nil {
			err = xml.Unmarshal(b, &x)
		}
		if err == nil && x.Error != "" {
			err = errors.New(x.Error)
		}
		if err != nil {
			_, _ = s.DB.ExecContext(ctx, `UPDATE player_intel SET steam_checked_at=?, steam_error=? WHERE steam64=?`, now, trunc(err.Error(), 200), id)
			if first == nil {
				first = err
			}
			continue
		}
		_, _ = s.DB.ExecContext(ctx, `UPDATE player_intel SET avatar=?, persona=?, profile_private=?, limited=?, vac_banned=?, trade_ban=?, steam_checked_at=?, steam_error='' WHERE steam64=?`,
			safeURL(x.Avatar), trunc(x.SteamID, 64), b2i(x.Privacy != "" && x.Privacy != "public"), x.Limited, b2i(x.VACBanned > 0), trunc(x.TradeBan, 32), now, id)
	}
	return first
}

func (s *Service) refreshSteamAPI(ctx context.Context, key string, ids []string, now string) error {
	for _, id := range ids {
		s.ensureRow(ctx, id)
	}
	list := strings.Join(ids, ",")
	b, err := s.get(ctx, s.SteamAPI+"/ISteamUser/GetPlayerBans/v1/?key="+url.QueryEscape(key)+"&steamids="+url.QueryEscape(list))
	if err != nil {
		return errors.New("Steam Web API: " + strings.ReplaceAll(err.Error(), key, "***"))
	}
	var bans struct {
		Players []struct {
			SteamID          string `json:"SteamId"`
			CommunityBanned  bool   `json:"CommunityBanned"`
			VACBanned        bool   `json:"VACBanned"`
			NumberOfVACBans  int    `json:"NumberOfVACBans"`
			DaysSinceLastBan int    `json:"DaysSinceLastBan"`
			NumberOfGameBans int    `json:"NumberOfGameBans"`
			EconomyBan       string `json:"EconomyBan"`
		} `json:"players"`
	}
	if err := json.Unmarshal(b, &bans); err != nil {
		return err
	}
	for _, p := range bans.Players {
		var days any
		if p.NumberOfVACBans+p.NumberOfGameBans > 0 {
			days = p.DaysSinceLastBan
		}
		_, _ = s.DB.ExecContext(ctx, `UPDATE player_intel SET vac_banned=?, vac_bans=?, game_bans=?, days_since_ban=?, community_banned=?, trade_ban=?, steam_checked_at=?, steam_error='' WHERE steam64=?`,
			b2i(p.VACBanned), p.NumberOfVACBans, p.NumberOfGameBans, days, b2i(p.CommunityBanned), trunc(p.EconomyBan, 32), now, p.SteamID)
	}
	b, err = s.get(ctx, s.SteamAPI+"/ISteamUser/GetPlayerSummaries/v2/?key="+url.QueryEscape(key)+"&steamids="+url.QueryEscape(list))
	if err != nil {
		return nil // bans are the important part; avatars retry next time
	}
	var sum struct {
		Response struct {
			Players []struct {
				SteamID    string `json:"steamid"`
				Name       string `json:"personaname"`
				Avatar     string `json:"avatarmedium"`
				Visibility int    `json:"communityvisibilitystate"`
			} `json:"players"`
		} `json:"response"`
	}
	if json.Unmarshal(b, &sum) == nil {
		for _, p := range sum.Response.Players {
			_, _ = s.DB.ExecContext(ctx, `UPDATE player_intel SET avatar=?, persona=?, profile_private=? WHERE steam64=?`, safeURL(p.Avatar), trunc(p.Name, 64), b2i(p.Visibility != 3), p.SteamID)
		}
	}
	return nil
}

func (s *Service) flushSteam(ctx context.Context, max int) bool {
	s.mu.Lock()
	ids := make([]string, 0, max)
	for id := range s.pendingSteam {
		if len(ids) == max {
			break
		}
		ids = append(ids, id)
		delete(s.pendingSteam, id)
	}
	s.mu.Unlock()
	if len(ids) == 0 {
		return false
	}
	if err := s.RefreshSteam(ctx, ids); err != nil && s.Log != nil {
		s.Log.Warn("steam intel refresh", "err", err)
	}
	return true
}

// ---- IP ----

func (s *Service) flushIP(ctx context.Context) bool {
	s.mu.Lock()
	batch := map[string]string{}
	for id, ip := range s.pendingIP {
		if len(batch) == 100 {
			break
		}
		batch[id] = ip
		delete(s.pendingIP, id)
	}
	s.mu.Unlock()
	if len(batch) == 0 {
		return false
	}
	now := s.Now().UTC().Format(time.RFC3339)
	queries := []map[string]string{}
	byIP := map[string][]string{}
	for id, ip := range batch {
		s.ensureRow(ctx, id)
		a, err := netip.ParseAddr(ip)
		if err != nil || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
			_, _ = s.DB.ExecContext(ctx, `UPDATE player_intel SET ip=?, country='Local network', country_code='', isp='', proxy=0, hosting=0, ip_checked_at=? WHERE steam64=?`, ip, now, id)
			continue
		}
		if len(byIP[ip]) == 0 {
			queries = append(queries, map[string]string{"query": ip})
		}
		byIP[ip] = append(byIP[ip], id)
	}
	if len(queries) == 0 {
		return true
	}
	body, _ := json.Marshal(queries)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.IPAPI+"/batch?fields=status,message,country,countryCode,isp,proxy,hosting,query", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.HTTP.Do(req)
	if err != nil {
		s.requeue(batch)
		return false
	}
	defer res.Body.Close()
	var out []struct {
		Status      string `json:"status"`
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
		ISP         string `json:"isp"`
		Proxy       bool   `json:"proxy"`
		Hosting     bool   `json:"hosting"`
		Query       string `json:"query"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out) != nil {
		s.requeue(batch)
		return false
	}
	for _, r := range out {
		if r.Status != "success" {
			continue
		}
		for _, id := range byIP[r.Query] {
			_, _ = s.DB.ExecContext(ctx, `UPDATE player_intel SET ip=?, country=?, country_code=?, isp=?, proxy=?, hosting=?, ip_checked_at=? WHERE steam64=?`,
				r.Query, trunc(r.Country, 64), trunc(strings.ToUpper(r.CountryCode), 2), trunc(r.ISP, 96), b2i(r.Proxy), b2i(r.Hosting), now, id)
		}
	}
	return true
}

func (s *Service) requeue(batch map[string]string) {
	s.mu.Lock()
	for id, ip := range batch {
		s.pendingIP[id] = ip
	}
	s.mu.Unlock()
}

func trunc(v string, n int) string {
	if len(v) > n {
		return v[:n]
	}
	return v
}

// safeURL keeps only https image URLs from Steam's CDN.
func safeURL(u string) string {
	p, err := url.Parse(strings.TrimSpace(u))
	if err != nil || p.Scheme != "https" || !(strings.HasSuffix(p.Host, ".steamstatic.com") || strings.HasSuffix(p.Host, ".akamaihd.net") || strings.HasSuffix(p.Host, ".steamcommunity.com")) {
		return ""
	}
	return p.String()
}

// ---- API ----

// Register adds routes.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/intel", s.apiList)
	mux.HandleFunc("GET /api/players/{id}/intel", s.apiOne)
	mux.HandleFunc("POST /api/players/{id}/intel/refresh", s.apiRefresh)
	mux.HandleFunc("GET /api/settings/intel", s.apiSettings)
	mux.HandleFunc("PUT /api/settings/intel", s.apiPutSettings)
}

func actor(r *http.Request) audit.Actor {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.Actor()
	}
	return audit.System
}

func (s *Service) apiList(w http.ResponseWriter, r *http.Request) {
	out := map[string]Intel{}
	ids := strings.Split(r.URL.Query().Get("ids"), ",")
	if len(ids) > 200 {
		ids = ids[:200]
	}
	var missing []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !reSteam64.MatchString(id) {
			continue
		}
		in := s.Get(r.Context(), id)
		if in.SteamCheckedAt == "" {
			missing = append(missing, id)
		}
		in.IP = ""
		out[id] = in
	}
	// Players never looked up are fetched in the background.
	s.mu.Lock()
	for _, id := range missing {
		s.pendingSteam[id] = true
	}
	s.mu.Unlock()
	httpx.JSON(w, 200, out)
}

func (s *Service) apiOne(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !reSteam64.MatchString(id) {
		httpx.Error(w, 400, "not a Steam64 ID")
		return
	}
	in := s.Get(r.Context(), id)
	if r.URL.Query().Get("reveal_ip") == "1" && in.IP != "" {
		_ = s.Audit.Write(r.Context(), actor(r), "players.reveal_ip", id, nil, "ok", "")
	} else {
		in.IP = ""
	}
	httpx.JSON(w, 200, in)
}

func (s *Service) apiRefresh(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !reSteam64.MatchString(id) {
		httpx.Error(w, 400, "not a Steam64 ID")
		return
	}
	if err := s.RefreshSteam(r.Context(), []string{id}); err != nil {
		httpx.Error(w, 502, "Steam lookup failed: "+err.Error())
		return
	}
	in := s.Get(r.Context(), id)
	in.IP = ""
	httpx.JSON(w, 200, in)
}

func (s *Service) apiSettings(w http.ResponseWriter, r *http.Request) {
	st := s.LoadSettings(r.Context())
	httpx.JSON(w, 200, map[string]any{"ip_lookup": st.IPLookup, "has_steam_key": st.SteamKeyEnc != "", "ip_provider": "ip-api.com"})
}

func (s *Service) apiPutSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IPLookup bool    `json:"ip_lookup"`
		SteamKey *string `json:"steam_key"`
	}
	if err := httpx.Decode(r, 4<<10, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	st := s.LoadSettings(r.Context())
	st.IPLookup = in.IPLookup
	keyChange := "unchanged"
	if in.SteamKey != nil {
		k := strings.TrimSpace(*in.SteamKey)
		switch {
		case k == "":
			st.SteamKeyEnc, keyChange = "", "removed"
		case !reSteamKey.MatchString(k):
			httpx.Error(w, 400, "a Steam Web API key is 32 hexadecimal characters")
			return
		case s.Box == nil:
			httpx.Error(w, 500, "encryption is not available")
			return
		default:
			st.SteamKeyEnc, keyChange = s.Box.SealString(k, settingsKey), "set"
		}
	}
	b, _ := json.Marshal(st)
	if _, err := s.DB.ExecContext(r.Context(), `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, settingsKey, string(b)); err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	_ = s.Audit.Write(r.Context(), actor(r), "settings.intel_update", "", map[string]any{"ip_lookup": st.IPLookup, "steam_key": keyChange}, "ok", "")
	if keyChange == "set" {
		// Re-check everyone already known with the richer API.
		rows, err := s.DB.QueryContext(r.Context(), `SELECT steam64 FROM player_intel`)
		if err == nil {
			s.mu.Lock()
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					s.pendingSteam[id] = true
				}
			}
			s.mu.Unlock()
			rows.Close()
		}
	}
	s.apiSettings(w, r)
}
