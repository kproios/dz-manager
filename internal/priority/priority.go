// Package priority keeps the owner's priority-queue list (#13, phase 1).
// DayZ has no RCON command for queue priority and the provider owns it, so DZ
// Manager never writes anywhere: it exports the list and tracks "applied?".
package priority

import (
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/store"
)

var reSteam = regexp.MustCompile(`^7656119\d{10}$`)

// Entry is one priority player.
type Entry struct {
	Steam64   string  `json:"steam64"`
	Name      string  `json:"name"`
	Reason    string  `json:"reason"`
	ExpiresAt *string `json:"expires_at,omitempty"`
	Expired   bool    `json:"expired"`
	Applied   bool    `json:"applied"`
	AddedBy   string  `json:"added_by"`
	CreatedAt string  `json:"created_at"`
}

// API serves /api/priority.
type API struct {
	DB    *store.DB
	Audit *audit.Log
}

// Register adds routes.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/priority", a.list)
	mux.HandleFunc("POST /api/priority", a.add)
	mux.HandleFunc("POST /api/priority/{steam64}/remove", a.remove)
	mux.HandleFunc("POST /api/priority/{steam64}/applied", a.applied)
	mux.HandleFunc("GET /api/priority/export", a.export)
}

func actor(r *http.Request) audit.Actor {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.Actor()
	}
	return audit.System
}

func (a *API) entries(r *http.Request) ([]Entry, error) {
	rows, err := a.DB.QueryContext(r.Context(), `SELECT q.steam64, COALESCE(p.name,''), q.reason, q.expires_at, q.applied, q.added_by, q.created_at
		FROM priority q LEFT JOIN players p ON p.steam64=q.steam64 ORDER BY q.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var exp sql.NullString
		var applied int
		if err := rows.Scan(&e.Steam64, &e.Name, &e.Reason, &exp, &applied, &e.AddedBy, &e.CreatedAt); err != nil {
			return nil, err
		}
		if exp.Valid {
			e.ExpiresAt = &exp.String
			e.Expired = exp.String <= now
		}
		e.Applied = applied == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	es, err := a.entries(r)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"entries": es,
		"note": "DZ Manager does not apply priority: copy the export into the provider (Architect / host panel) and tick 'applied'."})
}

func (a *API) add(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Steam64 string `json:"steam64"`
		Reason  string `json:"reason"`
		Days    int    `json:"days"` // 0 = no expiry
	}
	if err := httpx.Decode(r, 4<<10, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	in.Steam64, in.Reason = strings.TrimSpace(in.Steam64), strings.TrimSpace(in.Reason)
	if !reSteam.MatchString(in.Steam64) {
		httpx.Error(w, 400, "steam64 is invalid")
		return
	}
	if len(in.Reason) > 200 || in.Days < 0 || in.Days > 3650 {
		httpx.Error(w, 400, "reason max 200 chars, days 0..3650")
		return
	}
	var exp any
	if in.Days > 0 {
		exp = time.Now().UTC().Add(time.Duration(in.Days) * 24 * time.Hour).Format(time.RFC3339)
	}
	a2 := actor(r)
	now := store.Now()
	res, err := a.DB.ExecContext(r.Context(), `INSERT OR IGNORE INTO priority(steam64,reason,expires_at,applied,added_by,created_at,updated_at) VALUES(?,?,?,0,?,?,?)`,
		in.Steam64, in.Reason, exp, a2.Name, now, now)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, 409, "already on the priority list")
		return
	}
	_ = a.Audit.Write(r.Context(), a2, "priority.add", in.Steam64, map[string]any{"reason": in.Reason, "days": in.Days}, "ok", "")
	httpx.JSON(w, 201, map[string]bool{"ok": true})
}

func (a *API) remove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("steam64")
	res, err := a.DB.ExecContext(r.Context(), `DELETE FROM priority WHERE steam64=?`, id)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, 404, "not on the list")
		return
	}
	_ = a.Audit.Write(r.Context(), actor(r), "priority.remove", id, nil, "ok", "remove it from the provider too")
	httpx.JSON(w, 200, map[string]bool{"ok": true})
}

func (a *API) applied(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Applied bool `json:"applied"`
	}
	if err := httpx.Decode(r, 1<<10, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	id := r.PathValue("steam64")
	v := 0
	if in.Applied {
		v = 1
	}
	res, err := a.DB.ExecContext(r.Context(), `UPDATE priority SET applied=?, updated_at=? WHERE steam64=?`, v, store.Now(), id)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, 404, "not on the list")
		return
	}
	_ = a.Audit.Write(r.Context(), actor(r), "priority.applied", id, map[string]bool{"applied": in.Applied}, "ok", "")
	httpx.JSON(w, 200, map[string]bool{"ok": true})
}

// export returns active (non-expired) Steam64 IDs, one per line (format=lines)
// or as CSV with reasons (format=csv).
func (a *API) export(w http.ResponseWriter, r *http.Request) {
	es, err := a.entries(r)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	csv := r.URL.Query().Get("format") == "csv"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if csv {
		fmt.Fprintln(w, "steam64,name,reason,expires_at")
	}
	for _, e := range es {
		if e.Expired {
			continue
		}
		if csv {
			exp := ""
			if e.ExpiresAt != nil {
				exp = *e.ExpiresAt
			}
			fmt.Fprintf(w, "%s,%q,%q,%s\n", e.Steam64, e.Name, e.Reason, exp)
		} else {
			fmt.Fprintln(w, e.Steam64)
		}
	}
}
