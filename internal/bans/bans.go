// Package bans keeps the owner's ban list (#10), enforced by BattlEye through
// RCON so bans hold even when DZ Manager is offline, plus external banlists
// imported for manual review only. External matches never trigger an action.
package bans

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/woozymasta/bercon/parser/rv"
	"github.com/woozymasta/dzid"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/hooks"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/players"
	"github.com/kproios/dz-manager/internal/store"
)

// Tag marks bans owned by DZ Manager in BattlEye's list.
func Tag(id int64) string { return fmt.Sprintf("[DZM#%d]", id) }

var (
	reGUID    = regexp.MustCompile(`^[0-9a-f]{32}$`)
	reSteam   = regexp.MustCompile(`^7656119\d{10}$`)
	reTag     = regexp.MustCompile(`\[DZM#(\d+)\]`)
	identKind = map[string]*regexp.Regexp{"steam64": reSteam, "be_guid": reGUID}
)

// RCON is what the ban service needs from the connector.
type RCON interface {
	Exec(ctx context.Context, cmd string) (string, []byte, error)
}

// Roster resolves online players for the kick-after-ban.
type Roster interface {
	OnlineNumber(ctx context.Context, beGUID string) (int, bool)
}

// Service implements bans.
type Service struct {
	DB      *store.DB
	Audit   *audit.Log
	Players *players.Service
	RCON    RCON
	Roster  Roster
	Now     func() time.Time
	// Hooks receives public ban/unban events (Discord, bot API); optional.
	Hooks *hooks.Bus
}

func (s *Service) publishBan(kind string, b *Ban) {
	if s.Hooks == nil || b == nil {
		return
	}
	who := b.Name
	if who == "" {
		who = "A player"
	}
	if kind == hooks.Unban {
		s.Hooks.Publish(hooks.Event{Kind: kind, Title: who + " was unbanned"})
		return
	}
	until := "permanent"
	if b.ExpiresAt != nil {
		until = "until " + *b.ExpiresAt
	}
	s.Hooks.Publish(hooks.Event{Kind: kind, Title: who + " was banned", Fields: []hooks.Field{{Name: "Reason", Value: b.Reason}, {Name: "Duration", Value: until}}})
}

// Ban row.
type Ban struct {
	ID         int64   `json:"id"`
	Steam64    string  `json:"steam64,omitempty"`
	BEGUID     string  `json:"be_guid"`
	Name       string  `json:"name,omitempty"`
	Reason     string  `json:"reason"`
	CreatedAt  string  `json:"created_at"`
	ExpiresAt  *string `json:"expires_at,omitempty"`
	IssuerName string  `json:"issuer_name"`
	RevokedAt  *string `json:"revoked_at,omitempty"`
	RevokedBy  string  `json:"revoked_by,omitempty"`
	Applied    string  `json:"applied"` // ok | error: … | pending
	Status     string  `json:"status"`  // active | expired | revoked
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func actor(r *http.Request) audit.Actor {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.Actor()
	}
	return audit.System
}

// Register adds routes.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/bans", s.list)
	mux.HandleFunc("POST /api/bans", s.create)
	mux.HandleFunc("POST /api/bans/{id}/revoke", s.revoke)
	mux.HandleFunc("GET /api/bans/reconcile", s.reconcileAPI)
	mux.HandleFunc("GET /api/bans/export", s.export)
	mux.HandleFunc("GET /api/banlists", s.lists)
	mux.HandleFunc("POST /api/banlists/import", s.importList)
	mux.HandleFunc("GET /api/players/{id}/flags", s.flags)
	mux.HandleFunc("POST /api/players/{id}/review", s.review)
	mux.HandleFunc("GET /api/review-queue", s.queue)
}

func clean(v string, max int) string {
	v = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, v)
	v = strings.TrimSpace(v)
	if len([]rune(v)) > max {
		v = string([]rune(v)[:max])
	}
	return v
}

type createReq struct {
	Steam64 string `json:"steam64"`
	BEGUID  string `json:"be_guid"`
	Reason  string `json:"reason"`
	Minutes int    `json:"minutes"` // 0 = permanent
}

func (s *Service) create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in createReq
	if err := httpx.Decode(r, 4<<10, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	reason := clean(in.Reason, 100)
	if reason == "" {
		httpx.Error(w, 400, "reason is required")
		return
	}
	if in.Minutes < 0 || in.Minutes > 60*24*365*5 {
		httpx.Error(w, 400, "minutes must be 0 (permanent) .. 5 years")
		return
	}
	steam, guid := strings.TrimSpace(in.Steam64), strings.ToLower(strings.TrimSpace(in.BEGUID))
	if steam != "" {
		if !reSteam.MatchString(steam) {
			httpx.Error(w, 400, "steam64 is invalid")
			return
		}
		n, _ := strconv.ParseUint(steam, 10, 64)
		g := dzid.BattlEyeString(n)
		if guid != "" && guid != g {
			httpx.Error(w, 400, "be_guid does not belong to that steam64")
			return
		}
		guid = g
	}
	if !reGUID.MatchString(guid) {
		httpx.Error(w, 400, "a valid steam64 or 32-character be_guid is required")
		return
	}
	var active int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM bans WHERE be_guid=? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`, guid, s.now().UTC().Format(time.RFC3339)).Scan(&active)
	if active > 0 {
		httpx.Error(w, 409, "this player already has an active ban")
		return
	}
	a := actor(r)
	var exp any
	if in.Minutes > 0 {
		exp = s.now().UTC().Add(time.Duration(in.Minutes) * time.Minute).Format(time.RFC3339)
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO bans(steam64,be_guid,reason,created_at,expires_at,issuer_sub,issuer_name,applied) VALUES(?,?,?,?,?,?,?, 'pending')`,
		steam, guid, reason, s.now().UTC().Format(time.RFC3339), exp, a.Sub, a.Name)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	applied := "ok"
	_, _, err = s.RCON.Exec(ctx, fmt.Sprintf("addBan %s %d %s %s", guid, in.Minutes, Tag(id), reason))
	if err != nil {
		applied = "error: " + err.Error()
	} else if n, online := s.Roster.OnlineNumber(ctx, guid); online {
		_, _, _ = s.RCON.Exec(ctx, fmt.Sprintf("kick %d Banned: %s", n, reason))
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE bans SET applied=? WHERE id=?`, applied, id)
	target := steam
	if target == "" {
		target = guid
	}
	result := "ok"
	if applied != "ok" {
		result = "error"
	}
	_ = s.Audit.Write(ctx, a, "ban.create", target, map[string]any{"ban_id": id, "be_guid": guid, "reason": reason, "minutes": in.Minutes}, result, applied)
	b, _ := s.get(ctx, id)
	code := 201
	if applied != "ok" {
		code = 502
	} else {
		s.publishBan(hooks.Ban, b)
	}
	httpx.JSON(w, code, b)
}

func (s *Service) revoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	b, err := s.get(ctx, id)
	if err != nil {
		httpx.Error(w, 404, "ban not found")
		return
	}
	if b.RevokedAt != nil {
		httpx.Error(w, 409, "already revoked")
		return
	}
	a := actor(r)
	detail := "ok"
	if n, found, err := s.findInBE(ctx, id); err != nil {
		detail = "error: " + err.Error()
	} else if found {
		if _, _, err := s.RCON.Exec(ctx, fmt.Sprintf("removeBan %d", n)); err != nil {
			detail = "error: " + err.Error()
		} else {
			_, _, _ = s.RCON.Exec(ctx, "writeBans")
		}
	} else {
		detail = "ok (not present in BattlEye list: expired or removed elsewhere)"
	}
	if strings.HasPrefix(detail, "error") {
		_ = s.Audit.Write(ctx, a, "ban.revoke", b.BEGUID, map[string]any{"ban_id": id}, "error", detail)
		httpx.JSON(w, 502, map[string]any{"ok": false, "error": detail})
		return
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE bans SET revoked_at=?, revoked_by=? WHERE id=?`, s.now().UTC().Format(time.RFC3339), a.Name, id)
	_ = s.Audit.Write(ctx, a, "ban.revoke", targetOf(b), map[string]any{"ban_id": id}, "ok", detail)
	b, _ = s.get(ctx, id)
	s.publishBan(hooks.Unban, b)
	httpx.JSON(w, 200, b)
}

func targetOf(b *Ban) string {
	if b.Steam64 != "" {
		return b.Steam64
	}
	return b.BEGUID
}

func (s *Service) beBans(ctx context.Context) ([]rv.Ban, error) {
	_, out, err := s.RCON.Exec(ctx, "bans")
	if err != nil {
		return nil, err
	}
	parsed, perr := rv.ParseBans(out)
	if perr != nil && len(parsed.GUID) == 0 && len(bytes.TrimSpace(out)) > 0 && !bytes.Contains(out, []byte("GUID Bans")) {
		return nil, fmt.Errorf("cannot parse bans: %w", perr)
	}
	return parsed.GUID, nil
}

func (s *Service) findInBE(ctx context.Context, id int64) (int, bool, error) {
	list, err := s.beBans(ctx)
	if err != nil {
		return 0, false, err
	}
	for _, b := range list {
		if strings.Contains(b.Reason, Tag(id)) && b.IDValid {
			return b.ID, true, nil
		}
	}
	return 0, false, nil
}

// Drift is the reconciliation report.
type Drift struct {
	Missing []Ban    `json:"missing"` // active in DZ Manager, absent from BattlEye
	Stale   []rv.Ban `json:"stale"`   // tagged ours but revoked/expired here
	Foreign []rv.Ban `json:"foreign"` // created by someone else: never touched
	InSync  int      `json:"in_sync"`
}

// Reconcile compares desired (DB) and applied (BattlEye) state. It reports only.
func (s *Service) Reconcile(ctx context.Context) (*Drift, error) {
	list, err := s.beBans(ctx)
	if err != nil {
		return nil, err
	}
	all, err := s.query(ctx, "all")
	if err != nil {
		return nil, err
	}
	d := &Drift{Missing: []Ban{}, Stale: []rv.Ban{}, Foreign: []rv.Ban{}}
	present := map[int64]bool{}
	byID := map[int64]Ban{}
	for _, b := range all {
		byID[b.ID] = b
	}
	for _, e := range list {
		m := reTag.FindStringSubmatch(e.Reason)
		if m == nil {
			d.Foreign = append(d.Foreign, e)
			continue
		}
		id, _ := strconv.ParseInt(m[1], 10, 64)
		present[id] = true
		if b, ok := byID[id]; !ok || b.Status != "active" {
			d.Stale = append(d.Stale, e)
		} else {
			d.InSync++
		}
	}
	for _, b := range all {
		if b.Status == "active" && !present[b.ID] {
			d.Missing = append(d.Missing, b)
		}
	}
	return d, nil
}

func (s *Service) reconcileAPI(w http.ResponseWriter, r *http.Request) {
	d, err := s.Reconcile(r.Context())
	if err != nil {
		httpx.Error(w, 502, err.Error())
		return
	}
	httpx.JSON(w, 200, d)
}

const banCols = `b.id,b.steam64,b.be_guid,COALESCE(p.name,''),b.reason,b.created_at,b.expires_at,b.issuer_name,b.revoked_at,b.revoked_by,b.applied`

func (s *Service) scan(row interface{ Scan(...any) error }) (Ban, error) {
	var b Ban
	var exp, rev sql.NullString
	err := row.Scan(&b.ID, &b.Steam64, &b.BEGUID, &b.Name, &b.Reason, &b.CreatedAt, &exp, &b.IssuerName, &rev, &b.RevokedBy, &b.Applied)
	if exp.Valid {
		b.ExpiresAt = &exp.String
	}
	if rev.Valid {
		b.RevokedAt = &rev.String
	}
	switch {
	case b.RevokedAt != nil:
		b.Status = "revoked"
	case b.ExpiresAt != nil && *b.ExpiresAt <= s.now().UTC().Format(time.RFC3339):
		b.Status = "expired"
	default:
		b.Status = "active"
	}
	return b, err
}

func (s *Service) get(ctx context.Context, id int64) (*Ban, error) {
	b, err := s.scan(s.DB.QueryRowContext(ctx, `SELECT `+banCols+` FROM bans b LEFT JOIN players p ON p.be_guid=b.be_guid WHERE b.id=?`, id))
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Service) query(ctx context.Context, status string) ([]Ban, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+banCols+` FROM bans b LEFT JOIN players p ON p.be_guid=b.be_guid ORDER BY b.id DESC LIMIT 2000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ban{}
	for rows.Next() {
		b, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		if status == "all" || b.Status == status {
			out = append(out, b)
		}
	}
	return out, rows.Err()
}

func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	st := r.URL.Query().Get("status")
	if st == "" {
		st = "active"
	}
	bs, err := s.query(r.Context(), st)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	httpx.JSON(w, 200, bs)
}

func (s *Service) export(w http.ResponseWriter, r *http.Request) {
	bs, err := s.query(r.Context(), "all")
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="dzm-bans.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "steam64", "be_guid", "name", "reason", "created_at", "expires_at", "issuer", "status"})
	for _, b := range bs {
		exp := ""
		if b.ExpiresAt != nil {
			exp = *b.ExpiresAt
		}
		_ = cw.Write([]string{fmt.Sprint(b.ID), b.Steam64, b.BEGUID, b.Name, b.Reason, b.CreatedAt, exp, b.IssuerName, b.Status})
	}
	cw.Flush()
}

// ---- external lists (manual import, intelligence only) ----

// Entry is one imported external listing.
type Entry struct {
	Identifier string `json:"identifier"`
	Type       string `json:"identifier_type"`
	Reason     string `json:"reason,omitempty"`
	ListedAt   string `json:"listed_at,omitempty"`
}

// ParseImport parses CSV (identifier,identifier_type[,reason[,listed_at]]) or a
// JSON array of Entry. Any invalid line rejects the whole import.
func ParseImport(format string, content []byte) ([]Entry, []string) {
	var entries []Entry
	var errs []string
	switch format {
	case "json":
		if err := json.Unmarshal(content, &entries); err != nil {
			return nil, []string{"invalid JSON: " + err.Error()}
		}
	case "csv":
		rd := csv.NewReader(bytes.NewReader(content))
		rd.FieldsPerRecord = -1
		line := 0
		for {
			rec, err := rd.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			line++
			if err != nil {
				errs = append(errs, fmt.Sprintf("line %d: %v", line, err))
				continue
			}
			if line == 1 && strings.EqualFold(strings.TrimSpace(rec[0]), "identifier") {
				continue
			}
			if len(rec) < 2 {
				errs = append(errs, fmt.Sprintf("line %d: need at least identifier,identifier_type", line))
				continue
			}
			e := Entry{Identifier: rec[0], Type: rec[1]}
			if len(rec) > 2 {
				e.Reason = rec[2]
			}
			if len(rec) > 3 {
				e.ListedAt = rec[3]
			}
			entries = append(entries, e)
		}
	default:
		return nil, []string{"format must be csv or json"}
	}
	seen := map[string]bool{}
	out := make([]Entry, 0, len(entries))
	for i, e := range entries {
		e.Identifier, e.Type = strings.ToLower(strings.TrimSpace(e.Identifier)), strings.ToLower(strings.TrimSpace(e.Type))
		re, ok := identKind[e.Type]
		if !ok {
			errs = append(errs, fmt.Sprintf("entry %d: identifier_type must be steam64 or be_guid", i+1))
			continue
		}
		if !re.MatchString(e.Identifier) {
			errs = append(errs, fmt.Sprintf("entry %d: %q is not a valid %s", i+1, e.Identifier, e.Type))
			continue
		}
		if seen[e.Type+e.Identifier] {
			continue // duplicate rows are collapsed
		}
		seen[e.Type+e.Identifier] = true
		e.Reason = clean(e.Reason, 200)
		out = append(out, e)
	}
	if len(out) > 100000 {
		errs = append(errs, "too many entries (max 100000)")
	}
	return out, errs
}

type importReq struct {
	Name    string `json:"name"`
	Source  string `json:"source"` // free-text provenance (URL or description)
	Format  string `json:"format"`
	Content string `json:"content"`
}

func (s *Service) importList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in importReq
	if err := httpx.Decode(r, 8<<20, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	in.Name = clean(in.Name, 80)
	if in.Name == "" {
		httpx.Error(w, 400, "name is required")
		return
	}
	entries, errs := ParseImport(in.Format, []byte(in.Content))
	if len(errs) > 0 {
		httpx.JSON(w, 400, map[string]any{"error": "import rejected", "problems": errs})
		return
	}
	listID, err := s.ReplaceList(ctx, in.Name, in.Source, entries)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	_ = s.Audit.Write(ctx, actor(r), "banlist.import", in.Name, map[string]any{"entries": len(entries), "source": in.Source}, "ok", "")
	httpx.JSON(w, 200, map[string]any{"list_id": listID, "entries": len(entries)})
}

// ReplaceList stores a new revision of an external list (all entries replaced).
// Entries are evidence for review only; nothing is enforced from them.
func (s *Service) ReplaceList(ctx context.Context, name, source string, entries []Entry) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := s.now().UTC().Format(time.RFC3339)
	var listID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM external_lists WHERE name=?`, name).Scan(&listID)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := tx.ExecContext(ctx, `INSERT INTO external_lists(name,source,imported_at,entries) VALUES(?,?,?,0)`, name, clean(source, 300), now)
		if err != nil {
			return 0, err
		}
		listID, _ = res.LastInsertId()
	} else if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM external_entries WHERE list_id=?`, listID); err != nil {
		return 0, err
	}
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx, `INSERT INTO external_entries(list_id,identifier,identifier_type,reason,listed_at) VALUES(?,?,?,?,?)`, listID, e.Identifier, e.Type, e.Reason, e.ListedAt); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE external_lists SET imported_at=?, entries=?, source=? WHERE id=?`, now, len(entries), clean(source, 300), listID); err != nil {
		return 0, err
	}
	return listID, tx.Commit()
}

// List is an external list summary.
type List struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Source     string `json:"source"`
	ImportedAt string `json:"imported_at"`
	Entries    int    `json:"entries"`
	Status     string `json:"status"` // fresh | stale (> 30 days since import)
}

func (s *Service) allLists(ctx context.Context) ([]List, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,source,imported_at,entries FROM external_lists ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []List{}
	for rows.Next() {
		var l List
		if err := rows.Scan(&l.ID, &l.Name, &l.Source, &l.ImportedAt, &l.Entries); err != nil {
			return nil, err
		}
		l.Status = "fresh"
		if t, err := time.Parse(time.RFC3339, l.ImportedAt); err == nil && s.now().Sub(t) > 30*24*time.Hour {
			l.Status = "stale"
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Service) lists(w http.ResponseWriter, r *http.Request) {
	ls, err := s.allLists(r.Context())
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	httpx.JSON(w, 200, ls)
}

// Match is an external listing that matches a player exactly.
type Match struct {
	List       string `json:"list"`
	ListStatus string `json:"list_status"`
	ImportedAt string `json:"imported_at"`
	Type       string `json:"identifier_type"`
	Identifier string `json:"identifier"`
	Reason     string `json:"reason,omitempty"`
	ListedAt   string `json:"listed_at,omitempty"`
}

// Review is the admin's decision on external matches.
type Review struct {
	Decision string `json:"decision"` // ignored | watch | banned_manually
	Note     string `json:"note"`
	Reviewer string `json:"reviewer"`
	At       string `json:"at"`
}

// Flags summarises a player's ban state.
type Flags struct {
	Steam64   string  `json:"steam64"`
	LocalBan  *Ban    `json:"local_ban,omitempty"`
	External  []Match `json:"external"`
	Review    *Review `json:"review,omitempty"`
	Disclaim  string  `json:"disclaimer"`
	BanEffect string  `json:"enforcement"`
}

// FlagsFor computes flags for a player (exact identifier-type matches only).
func (s *Service) FlagsFor(ctx context.Context, p *players.Player) (*Flags, error) {
	f := &Flags{Steam64: p.Steam64, External: []Match{},
		Disclaim:  "External listings are evidence for review, not proof. They never kick or ban automatically.",
		BanEffect: "Local bans are enforced by BattlEye (RCON addBan) and survive DZ Manager downtime."}
	if b, err := s.scan(s.DB.QueryRowContext(ctx, `SELECT `+banCols+` FROM bans b LEFT JOIN players p ON p.be_guid=b.be_guid WHERE b.be_guid=? ORDER BY b.id DESC LIMIT 1`, p.BEGUID)); err == nil && b.Status == "active" {
		f.LocalBan = &b
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT l.name,l.imported_at,e.identifier_type,e.identifier,e.reason,e.listed_at FROM external_entries e JOIN external_lists l ON l.id=e.list_id
		WHERE (e.identifier_type='steam64' AND e.identifier=?) OR (e.identifier_type='be_guid' AND e.identifier=?)`, p.Steam64, p.BEGUID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m Match
		if rows.Scan(&m.List, &m.ImportedAt, &m.Type, &m.Identifier, &m.Reason, &m.ListedAt) == nil {
			m.ListStatus = "fresh"
			if t, err := time.Parse(time.RFC3339, m.ImportedAt); err == nil && s.now().Sub(t) > 30*24*time.Hour {
				m.ListStatus = "stale"
			}
			f.External = append(f.External, m)
		}
	}
	rows.Close()
	var rv Review
	if err := s.DB.QueryRowContext(ctx, `SELECT decision,note,reviewer,at FROM external_reviews WHERE steam64=?`, p.Steam64).Scan(&rv.Decision, &rv.Note, &rv.Reviewer, &rv.At); err == nil {
		f.Review = &rv
	}
	return f, nil
}

func (s *Service) flags(w http.ResponseWriter, r *http.Request) {
	p, err := s.Players.Find(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, 404, "unknown player")
		return
	}
	f, err := s.FlagsFor(r.Context(), p)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	httpx.JSON(w, 200, f)
}

func (s *Service) review(w http.ResponseWriter, r *http.Request) {
	p, err := s.Players.Find(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, 404, "unknown player")
		return
	}
	var in struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if err := httpx.Decode(r, 4<<10, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	switch in.Decision {
	case "ignored", "watch", "banned_manually":
	default:
		httpx.Error(w, 400, "decision must be ignored, watch or banned_manually")
		return
	}
	a := actor(r)
	note := clean(in.Note, 500)
	_, err = s.DB.ExecContext(r.Context(), `INSERT INTO external_reviews(steam64,decision,note,reviewer,at) VALUES(?,?,?,?,?)
		ON CONFLICT(steam64) DO UPDATE SET decision=excluded.decision, note=excluded.note, reviewer=excluded.reviewer, at=excluded.at`,
		p.Steam64, in.Decision, note, a.Name, s.now().UTC().Format(time.RFC3339))
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	_ = s.Audit.Write(r.Context(), a, "banlist.review", p.Steam64, map[string]any{"decision": in.Decision, "note": note}, "ok", "")
	httpx.JSON(w, 200, map[string]bool{"ok": true})
}

func (s *Service) queue(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.QueryContext(r.Context(), `SELECT DISTINCT p.steam64, p.name, l.name, e.identifier_type, e.reason FROM players p
		JOIN external_entries e ON (e.identifier_type='steam64' AND e.identifier=p.steam64) OR (e.identifier_type='be_guid' AND e.identifier=p.be_guid)
		JOIN external_lists l ON l.id=e.list_id
		LEFT JOIN external_reviews rv ON rv.steam64=p.steam64
		WHERE rv.steam64 IS NULL ORDER BY p.last_seen DESC LIMIT 200`)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	defer rows.Close()
	type item struct {
		Steam64 string `json:"steam64"`
		Name    string `json:"name"`
		List    string `json:"list"`
		Type    string `json:"identifier_type"`
		Reason  string `json:"reason"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		if rows.Scan(&it.Steam64, &it.Name, &it.List, &it.Type, &it.Reason) == nil {
			out = append(out, it)
		}
	}
	httpx.JSON(w, 200, out)
}
