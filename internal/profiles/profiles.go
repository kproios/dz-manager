// Package profiles stores game-server connection profiles (#20). The stable
// server_id survives host/provider changes so history stays attached.
package profiles

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/kproios/dz-manager/internal/secret"
	"github.com/kproios/dz-manager/internal/store"
)

// Provider kinds.
const (
	Selfhosted = "selfhosted"
	VYKIX      = "vykix"
	Generic    = "generic"
)

// ErrNotFound is returned for unknown profiles.
var ErrNotFound = errors.New("profile not found")

// Profile is the public (non-secret) view of a server profile.
type Profile struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ProviderKind    string `json:"provider_kind"`
	Active          bool   `json:"active"`
	Archived        bool   `json:"archived"`
	GameHost        string `json:"game_host"`
	GamePort        int    `json:"game_port"`
	QueryHost       string `json:"query_host"`
	QueryPort       int    `json:"query_port"`
	RconHost        string `json:"rcon_host"`
	RconPort        int    `json:"rcon_port"`
	HasRconPassword bool   `json:"has_rcon_password"`
	MetricZUser     string `json:"metricz_user"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

// QueryAddr returns host:port for A2S (falls back to the game host).
func (p *Profile) QueryAddr() string {
	h := p.QueryHost
	if h == "" {
		h = p.GameHost
	}
	return net.JoinHostPort(h, fmt.Sprint(p.QueryPort))
}

// RconAddr returns host:port for BattlEye RCON (falls back to the game host).
func (p *Profile) RconAddr() string {
	h := p.RconHost
	if h == "" {
		h = p.GameHost
	}
	return net.JoinHostPort(h, fmt.Sprint(p.RconPort))
}

// Secrets are stored sealed.
type Secrets struct {
	RconPassword    string `json:"rcon_password"`
	MetricZPassword string `json:"metricz_password"`
	BridgeToken     string `json:"bridge_token"`
}

// Input is the create/update payload. Empty RconPassword on update keeps the old one.
type Input struct {
	Name         string `json:"name"`
	ProviderKind string `json:"provider_kind"`
	GameHost     string `json:"game_host"`
	GamePort     int    `json:"game_port"`
	QueryHost    string `json:"query_host"`
	QueryPort    int    `json:"query_port"`
	RconHost     string `json:"rcon_host"`
	RconPort     int    `json:"rcon_port"`
	RconPassword string `json:"rcon_password"`
}

func (in *Input) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.GameHost = strings.TrimSpace(in.GameHost)
	in.QueryHost = strings.TrimSpace(in.QueryHost)
	in.RconHost = strings.TrimSpace(in.RconHost)
	if in.Name == "" || len(in.Name) > 80 {
		return errors.New("name is required (max 80 chars)")
	}
	switch in.ProviderKind {
	case Selfhosted, VYKIX, Generic:
	case "":
		in.ProviderKind = Generic
	default:
		return fmt.Errorf("provider_kind must be %s, %s or %s", Selfhosted, VYKIX, Generic)
	}
	if in.GamePort == 0 {
		in.GamePort = 2302
	}
	if in.QueryPort == 0 {
		in.QueryPort = 27016
	}
	if in.RconPort == 0 {
		in.RconPort = 2305
	}
	for _, p := range []int{in.GamePort, in.QueryPort, in.RconPort} {
		if p < 1 || p > 65535 {
			return errors.New("ports must be 1..65535")
		}
	}
	for _, h := range []string{in.GameHost, in.QueryHost, in.RconHost} {
		if strings.ContainsAny(h, " /:@") && net.ParseIP(h) == nil {
			return fmt.Errorf("host %q is invalid (use a hostname or IP without port)", h)
		}
	}
	if len(in.RconPassword) > 200 {
		return errors.New("rcon_password too long")
	}
	return nil
}

// Store persists profiles.
type Store struct {
	db  *store.DB
	box *secret.Box
	// OnChange is called after any write (pollers reconfigure themselves).
	OnChange func()
}

// New creates a Store.
func New(db *store.DB, box *secret.Box) *Store { return &Store{db: db, box: box} }

const cols = `id,name,provider_kind,active,archived,game_host,game_port,query_host,query_port,rcon_host,rcon_port,secrets,metricz_user,created_at,updated_at`

func (s *Store) scan(row interface{ Scan(...any) error }) (*Profile, *Secrets, error) {
	var p Profile
	var sealed []byte
	var active, archived int
	if err := row.Scan(&p.ID, &p.Name, &p.ProviderKind, &active, &archived, &p.GameHost, &p.GamePort, &p.QueryHost, &p.QueryPort, &p.RconHost, &p.RconPort, &sealed, &p.MetricZUser, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	p.Active, p.Archived = active == 1, archived == 1
	var sec Secrets
	if len(sealed) > 0 {
		pt, err := s.box.Open(sealed, []byte(p.ID))
		if err != nil {
			return &p, nil, err
		}
		if err := json.Unmarshal(pt, &sec); err != nil {
			return &p, nil, err
		}
	}
	p.HasRconPassword = sec.RconPassword != ""
	return &p, &sec, nil
}

// List returns all profiles (archived last).
func (s *Store) List(ctx context.Context) ([]Profile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM server_profiles ORDER BY archived, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Profile{}
	for rows.Next() {
		p, _, err := s.scan(rows)
		if err != nil && p == nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// Get returns a profile and its secrets.
func (s *Store) Get(ctx context.Context, id string) (*Profile, *Secrets, error) {
	return s.scan(s.db.QueryRowContext(ctx, `SELECT `+cols+` FROM server_profiles WHERE id=?`, id))
}

// Active returns the active profile, or ErrNotFound.
func (s *Store) Active(ctx context.Context) (*Profile, *Secrets, error) {
	return s.scan(s.db.QueryRowContext(ctx, `SELECT `+cols+` FROM server_profiles WHERE active=1 AND archived=0`))
}

func (s *Store) seal(id string, sec *Secrets) []byte {
	b, _ := json.Marshal(sec)
	return s.box.Seal(b, []byte(id))
}

// Create adds a profile with fresh ingest credentials. The first profile becomes active.
func (s *Store) Create(ctx context.Context, in Input) (*Profile, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	id := secret.UUID()
	sec := &Secrets{RconPassword: in.RconPassword, MetricZPassword: secret.Token(24), BridgeToken: secret.Token(32)}
	user := "srv-" + strings.ReplaceAll(id, "-", "")[:12]
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM server_profiles WHERE active=1`).Scan(&n)
	active := 0
	if n == 0 {
		active = 1
	}
	now := store.Now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO server_profiles(id,name,provider_kind,active,game_host,game_port,query_host,query_port,rcon_host,rcon_port,secrets,metricz_user,metricz_pass_hash,bridge_token_hash,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, in.Name, in.ProviderKind, active, in.GameHost, in.GamePort, in.QueryHost, in.QueryPort, in.RconHost, in.RconPort,
		s.seal(id, sec), user, s.box.Hash(sec.MetricZPassword), s.box.Hash(sec.BridgeToken), now, now)
	if err != nil {
		return nil, err
	}
	s.changed()
	p, _, err := s.Get(ctx, id)
	return p, err
}

// Update edits connection fields. The id (server_id) never changes.
func (s *Store) Update(ctx context.Context, id string, in Input) (*Profile, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	_, sec, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if sec == nil {
		return nil, errors.New("stored secrets cannot be decrypted (DZM_SECRET_KEY changed?)")
	}
	if in.RconPassword != "" {
		sec.RconPassword = in.RconPassword
	}
	res, err := s.db.ExecContext(ctx, `UPDATE server_profiles SET name=?,provider_kind=?,game_host=?,game_port=?,query_host=?,query_port=?,rcon_host=?,rcon_port=?,secrets=?,updated_at=? WHERE id=?`,
		in.Name, in.ProviderKind, in.GameHost, in.GamePort, in.QueryHost, in.QueryPort, in.RconHost, in.RconPort, s.seal(id, sec), store.Now(), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	s.changed()
	p, _, err := s.Get(ctx, id)
	return p, err
}

// Activate makes one profile the active one.
func (s *Store) Activate(ctx context.Context, id string) error {
	p, _, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if p.Archived {
		return errors.New("cannot activate an archived profile")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE server_profiles SET active=0 WHERE active=1`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE server_profiles SET active=1, updated_at=? WHERE id=?`, store.Now(), id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.changed()
	return nil
}

// Archive deactivates a profile but keeps it (and all linked history).
func (s *Store) Archive(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE server_profiles SET archived=1, active=0, updated_at=? WHERE id=?`, store.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.changed()
	return nil
}

// RotateIngest issues new MetricZ and bridge credentials (old ones stop working immediately).
func (s *Store) RotateIngest(ctx context.Context, id string) error {
	_, sec, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if sec == nil {
		return errors.New("stored secrets cannot be decrypted")
	}
	sec.MetricZPassword, sec.BridgeToken = secret.Token(24), secret.Token(32)
	_, err = s.db.ExecContext(ctx, `UPDATE server_profiles SET secrets=?, metricz_pass_hash=?, bridge_token_hash=?, updated_at=? WHERE id=?`,
		s.seal(id, sec), s.box.Hash(sec.MetricZPassword), s.box.Hash(sec.BridgeToken), store.Now(), id)
	if err == nil {
		s.changed()
	}
	return err
}

// AuthMetricZ resolves a server from MetricZ Basic-auth credentials.
func (s *Store) AuthMetricZ(ctx context.Context, user, pass string) (string, bool) {
	var id, h string
	if err := s.db.QueryRowContext(ctx, `SELECT id, metricz_pass_hash FROM server_profiles WHERE metricz_user=? AND archived=0`, user).Scan(&id, &h); err != nil {
		return "", false
	}
	return id, secret.Equal(h, s.box.Hash(pass))
}

// AuthBridge resolves a server from a bridge token.
func (s *Store) AuthBridge(ctx context.Context, token string) (string, bool) {
	if token == "" {
		return "", false
	}
	var id string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM server_profiles WHERE bridge_token_hash=? AND archived=0`, s.box.Hash(token)).Scan(&id); err != nil {
		return "", false
	}
	return id, true
}

func (s *Store) changed() {
	if s.OnChange != nil {
		s.OnChange()
	}
}
