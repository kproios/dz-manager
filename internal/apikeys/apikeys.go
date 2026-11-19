// Package apikeys manages named API keys for the public API (/public/v1) and
// the MCP server (/mcp). Each key carries a fixed set of scopes chosen when it
// is created; only a keyed hash of the token is stored and the token is shown
// once. Every request made with a key is attributed to it in the audit log.
package apikeys

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/secret"
	"github.com/kproios/dz-manager/internal/store"
)

// Scope is one permission a key can hold.
type Scope struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Help  string `json:"help"`
	// Sensitive scopes expose Steam IDs, positions or change the game.
	Sensitive bool `json:"sensitive"`
}

// Scopes lists every scope in UI order.
var Scopes = []Scope{
	{"public", "Public stats", "Server status, players online (names), kill feed, leaderboards, player stats by name, ban list (names and reasons), world event counts, server info and the event feed. Safe for a website or Discord bot.", false},
	{"players:read", "Players", "Player list and profiles with Steam64 IDs, kills, deaths and playtime. No IP addresses.", true},
	{"bans:read", "Ban list export", "Active bans with Steam64 and BattlEye GUID, as JSON, text or a BattlEye bans.txt, to share with other servers.", true},
	{"priority:read", "Priority queue export", "Players with queue priority and when it ends (Steam64), as JSON or text.", true},
	{"map:read", "Live map", "Live positions of players and vehicles and the active events. Admin-level information.", true},
	{"actions:write", "Game actions", "Teleport, heal, give items, kill, strip, message players, and refuel, repair, flip, stop or delete vehicles. Destructive ones still need their confirmation word.", true},
}

// ScopeNames returns the valid scope names.
func ScopeNames() []string {
	out := make([]string, 0, len(Scopes))
	for _, s := range Scopes {
		out = append(out, s.Name)
	}
	return out
}

func validScope(s string) bool {
	for _, x := range Scopes {
		if x.Name == s {
			return true
		}
	}
	return false
}

// Key is a stored key (never the token).
type Key struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	Scopes     []string `json:"scopes"`
	CreatedAt  string   `json:"created_at"`
	CreatedBy  string   `json:"created_by"`
	ExpiresAt  *string  `json:"expires_at,omitempty"`
	LastUsedAt *string  `json:"last_used_at,omitempty"`
	RevokedAt  *string  `json:"revoked_at,omitempty"`
}

// Has reports whether the key holds a scope.
func (k *Key) Has(scope string) bool {
	if k == nil {
		return false
	}
	for _, s := range k.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Actor is how a key appears in the audit log.
func (k *Key) Actor() audit.Actor {
	return audit.Actor{Sub: "apikey:" + k.ID, Name: "API key “" + k.Name + "”"}
}

// Store reads and writes keys.
type Store struct {
	DB    *store.DB
	Box   *secret.Box
	Audit *audit.Log

	mu       sync.Mutex
	lastUsed map[string]time.Time
}

const cols = `id,name,prefix,scopes,created_at,created_by,expires_at,last_used_at,revoked_at`

func scan(row interface{ Scan(...any) error }) (*Key, error) {
	var k Key
	var scopes string
	var exp, used, rev sql.NullString
	if err := row.Scan(&k.ID, &k.Name, &k.Prefix, &scopes, &k.CreatedAt, &k.CreatedBy, &exp, &used, &rev); err != nil {
		return nil, err
	}
	k.Scopes = strings.Fields(scopes)
	if exp.Valid {
		k.ExpiresAt = &exp.String
	}
	if used.Valid {
		k.LastUsedAt = &used.String
	}
	if rev.Valid {
		k.RevokedAt = &rev.String
	}
	return &k, nil
}

// List returns every key, newest first.
func (s *Store) List(ctx context.Context) ([]Key, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+cols+` FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Key{}
	for rows.Next() {
		k, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

// Create stores a new key and returns it with its token (shown once).
func (s *Store) Create(ctx context.Context, name string, scopes []string, ttl time.Duration, by string) (*Key, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 60 {
		return nil, "", errors.New("give the key a name (up to 60 characters), e.g. “Website stats”")
	}
	seen := map[string]bool{}
	clean := []string{}
	for _, sc := range scopes {
		if !validScope(sc) {
			return nil, "", errors.New("unknown scope " + sc)
		}
		if !seen[sc] {
			seen[sc] = true
			clean = append(clean, sc)
		}
	}
	if len(clean) == 0 {
		return nil, "", errors.New("choose at least one scope")
	}
	sort.Strings(clean)
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return nil, "", err
	}
	token := "dzm_" + hex.EncodeToString(b)
	id := hex.EncodeToString(b[:6])
	now := time.Now().UTC()
	var exp any
	if ttl > 0 {
		exp = now.Add(ttl).Format(time.RFC3339)
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO api_keys(id,name,prefix,hash,scopes,created_at,created_by,expires_at) VALUES(?,?,?,?,?,?,?,?)`,
		id, name, token[:10], s.Box.Hash(token), strings.Join(clean, " "), now.Format(time.RFC3339), by, exp)
	if err != nil {
		return nil, "", err
	}
	k, err := scan(s.DB.QueryRowContext(ctx, `SELECT `+cols+` FROM api_keys WHERE id=?`, id))
	return k, token, err
}

// Revoke ends a key immediately.
func (s *Store) Revoke(ctx context.Context, id string) error {
	r, err := s.DB.ExecContext(ctx, `UPDATE api_keys SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ErrNoKey means no usable key was presented.
var ErrNoKey = errors.New("missing or invalid API key")

// Token reads the bearer token of a request.
func Token(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if t, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(t)
	}
	return ""
}

// Authenticate returns the key behind a bearer token.
func (s *Store) Authenticate(ctx context.Context, token string) (*Key, error) {
	if !strings.HasPrefix(token, "dzm_") || len(token) > 100 {
		return nil, ErrNoKey
	}
	// The lookup is by a keyed HMAC of the token, so comparing in the database
	// leaks nothing useful about the token itself.
	k, err := scan(s.DB.QueryRowContext(ctx, `SELECT `+cols+` FROM api_keys WHERE hash=?`, s.Box.Hash(token)))
	if err != nil || k.RevokedAt != nil {
		return nil, ErrNoKey
	}
	if k.ExpiresAt != nil {
		if t, err := time.Parse(time.RFC3339, *k.ExpiresAt); err == nil && time.Now().After(t) {
			return nil, ErrNoKey
		}
	}
	s.touch(ctx, k.ID)
	return k, nil
}

// touch records use at most once a minute per key.
func (s *Store) touch(ctx context.Context, id string) {
	s.mu.Lock()
	if s.lastUsed == nil {
		s.lastUsed = map[string]time.Time{}
	}
	if time.Since(s.lastUsed[id]) < time.Minute {
		s.mu.Unlock()
		return
	}
	s.lastUsed[id] = time.Now()
	s.mu.Unlock()
	_, _ = s.DB.ExecContext(ctx, `UPDATE api_keys SET last_used_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339), id)
}

type ctxKey struct{}

// FromContext returns the key of an authenticated API request.
func FromContext(ctx context.Context) *Key {
	k, _ := ctx.Value(ctxKey{}).(*Key)
	return k
}

// WithKey stores the key in a context.
func WithKey(ctx context.Context, k *Key) context.Context {
	return context.WithValue(ctx, ctxKey{}, k)
}

// Require wraps a handler: the request needs a valid key holding scope.
func (s *Store) Require(scope string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// In-process calls (the MCP server) carry the already authenticated
		// key in the context; a context can't be set from outside.
		k := FromContext(r.Context())
		var err error
		if k == nil {
			k, err = s.Authenticate(r.Context(), Token(r))
		}
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="dz-manager"`)
			httpx.Error(w, 401, "missing or invalid API key (Authorization: Bearer dzm_…)")
			return
		}
		if !k.Has(scope) {
			httpx.Error(w, 403, "this API key does not have the "+scope+" scope")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		h(w, r.WithContext(WithKey(r.Context(), k)))
	}
}

// MigrateBotToken turns the single bot token of older versions into a key
// with the public scope, so existing bots keep working.
func (s *Store) MigrateBotToken(ctx context.Context) {
	var raw string
	if s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='bot_api'`).Scan(&raw) != nil {
		return
	}
	var st struct {
		TokenHash string `json:"token_hash"`
		CreatedAt string `json:"created_at"`
	}
	if json.Unmarshal([]byte(raw), &st) != nil || st.TokenHash == "" {
		return
	}
	created := st.CreatedAt
	if created == "" {
		created = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO api_keys(id,name,prefix,hash,scopes,created_at,created_by) VALUES('botapi1','Bot API (from the old single token)','dzm_…',?,'public',?,'migration')`, st.TokenHash, created)
	if err == nil {
		var m map[string]any
		_ = json.Unmarshal([]byte(raw), &m)
		delete(m, "token_hash")
		delete(m, "created_at")
		b, _ := json.Marshal(m)
		_, _ = s.DB.ExecContext(ctx, `UPDATE settings SET value=? WHERE key='bot_api'`, string(b))
	}
}

func actor(r *http.Request) audit.Actor {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.Actor()
	}
	return audit.System
}

// RegisterAdmin adds the key management routes (session-authenticated).
func (s *Store) RegisterAdmin(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/api-keys", func(w http.ResponseWriter, r *http.Request) {
		keys, err := s.List(r.Context())
		if err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		httpx.JSON(w, 200, map[string]any{"keys": keys, "scopes": Scopes, "base_path": "/public/v1", "mcp_path": "/mcp", "docs": "/public/v1/openapi.json"})
	})
	mux.HandleFunc("POST /api/settings/api-keys", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name        string   `json:"name"`
			Scopes      []string `json:"scopes"`
			ExpiresDays int      `json:"expires_days"`
		}
		if err := httpx.Decode(r, 4096, &in); err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
		if in.ExpiresDays < 0 || in.ExpiresDays > 3650 {
			httpx.Error(w, 400, "expires_days must be 0 (never) to 3650")
			return
		}
		a := actor(r)
		k, token, err := s.Create(r.Context(), in.Name, in.Scopes, time.Duration(in.ExpiresDays)*24*time.Hour, a.Name)
		if err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
		_ = s.Audit.Write(r.Context(), a, "settings.api_key_create", k.ID, map[string]any{"name": k.Name, "scopes": k.Scopes, "expires_at": k.ExpiresAt}, "ok", "")
		httpx.JSON(w, 201, map[string]any{"key": k, "token": token})
	})
	mux.HandleFunc("DELETE /api/settings/api-keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := s.Revoke(r.Context(), id); err != nil {
			httpx.Error(w, 404, "no active key with that id")
			return
		}
		_ = s.Audit.Write(r.Context(), actor(r), "settings.api_key_revoke", id, nil, "ok", "")
		httpx.JSON(w, 200, map[string]bool{"revoked": true})
	})
}
