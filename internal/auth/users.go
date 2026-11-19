package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/perm"
	"github.com/kproios/dz-manager/internal/secret"
	"github.com/kproios/dz-manager/internal/store"
)

// RegisterAPI adds the account and user management routes to the API mux
// (signed-in; permissions come from perm.Routes).
func (a *Auth) RegisterAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/account", a.account)
	mux.HandleFunc("PUT /api/account/password", a.setPassword)
	mux.HandleFunc("POST /api/account/confirm", a.confirm)
	mux.HandleFunc("POST /api/account/recovery-codes", a.newRecoveryCodes)
	mux.HandleFunc("POST /api/account/2fa/start", a.reenrollStart)
	mux.HandleFunc("POST /api/account/2fa/finish", a.reenrollFinish)
	mux.HandleFunc("DELETE /api/account/sessions/{id}", a.endSession)
	mux.HandleFunc("POST /api/account/sessions/sign-out-all", a.endOtherSessions)
	mux.HandleFunc("DELETE /api/account/steam", a.unlink("steam64", "steam"))
	mux.HandleFunc("DELETE /api/account/oidc", a.unlink("oidc_sub", "oidc"))

	mux.HandleFunc("GET /api/users", a.listUsers)
	mux.HandleFunc("POST /api/users/invites", a.createInviteAPI)
	mux.HandleFunc("DELETE /api/users/invites/{id}", a.deleteInvite)
	mux.HandleFunc("PUT /api/users/{id}/role", a.userAction("role"))
	mux.HandleFunc("POST /api/users/{id}/disable", a.userAction("disable"))
	mux.HandleFunc("POST /api/users/{id}/enable", a.userAction("enable"))
	mux.HandleFunc("POST /api/users/{id}/reset-2fa", a.userAction("reset-2fa"))
	mux.HandleFunc("POST /api/users/{id}/sign-out", a.userAction("sign-out"))
	mux.HandleFunc("DELETE /api/users/{id}", a.userAction("delete"))
}

func roleLabel(r string) string {
	for _, x := range perm.Roles {
		if string(x.Name) == r {
			return x.Label
		}
	}
	return r
}

// ---- own account ----

type sessionView struct {
	ID         string `json:"id"`
	Method     string `json:"method"`
	IP         string `json:"ip"`
	UserAgent  string `json:"user_agent"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at"`
	Current    bool   `json:"current"`
}

func (a *Auth) sessionsOf(ctx context.Context, uid int64, current string) []sessionView {
	out := []sessionView{}
	rows, err := a.db.QueryContext(ctx, `SELECT id_hash, method, ip, user_agent, created_at, last_seen_at FROM sessions WHERE user_id=? AND expires_at > ? ORDER BY last_seen_at DESC`, uid, a.now().Unix())
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s sessionView
		var c, l int64
		if rows.Scan(&s.ID, &s.Method, &s.IP, &s.UserAgent, &c, &l) == nil {
			s.CreatedAt, s.LastSeenAt = time.Unix(c, 0).UTC().Format(time.RFC3339), time.Unix(l, 0).UTC().Format(time.RFC3339)
			s.Current = s.ID == current
			s.ID = s.ID[:16]
			out = append(out, s)
		}
	}
	return out
}

func (a *Auth) account(w http.ResponseWriter, r *http.Request) {
	u := FromContext(r.Context())
	ctx := r.Context()
	var login, steam, oidcSub sql.NullString
	var pw, totp string
	_ = a.db.QueryRowContext(ctx, `SELECT login, password_hash, totp_secret, steam64, oidc_sub FROM users WHERE id=?`, u.ID).Scan(&login, &pw, &totp, &steam, &oidcSub)
	var left int
	_ = a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM recovery_codes WHERE user_id=? AND used_at IS NULL`, u.ID).Scan(&left)
	httpx.JSON(w, 200, map[string]any{
		"user": u, "role_label": roleLabel(string(u.Role)),
		"login": login.String, "has_password": pw != "", "steam64": steam.String, "oidc_linked": oidcSub.Valid,
		"totp": totp != "", "recovery_left": left, "sessions": a.sessionsOf(ctx, u.ID, u.sessionHash),
		"methods": a.Methods(), "step_up_until": u.StepUpUntil, "permissions": perm.All,
	})
}

func (a *Auth) setPassword(w http.ResponseWriter, r *http.Request) {
	u := FromContext(r.Context())
	var in struct {
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, 4096, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	if err := CheckPasswordPolicy(in.Password, u.Username); err != nil {
		httpx.Error(w, 400, "Password: "+err.Error())
		return
	}
	// Signing in with a password uses the display name (in lower case).
	_, err := a.db.ExecContext(r.Context(), `UPDATE users SET password_hash=?, login=COALESCE(login, lower(username)) WHERE id=?`, HashPassword(in.Password), u.ID)
	if err != nil {
		httpx.Error(w, 400, "Another account already signs in with that name; ask an owner to rename one of them.")
		return
	}
	_ = a.audit.Write(r.Context(), u.Actor(), "account.password", "", nil, "ok", "")
	httpx.JSON(w, 200, map[string]bool{"ok": true})
}

// confirm re-checks the second factor and unlocks sensitive routes for StepUpTTL.
func (a *Auth) confirm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := FromContext(ctx)
	if a.tooManyFromIP(w, r) {
		return
	}
	if d, locked := a.accountLocked(ctx, u.ID); locked {
		a.lockedReply(w, d)
		return
	}
	var in struct {
		Code     string `json:"code"`
		Recovery string `json:"recovery"`
	}
	if err := httpx.Decode(r, 1024, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	if _, ok := a.checkSecondFactor(ctx, u.ID, in.Code, in.Recovery); !ok {
		a.failLogin(ctx, r, u.ID, u.Actor(), "wrong 2FA code (confirm)")
		httpx.Error(w, 401, "That code is not right (or was already used).")
		return
	}
	now := a.now().Unix()
	_, _ = a.db.ExecContext(ctx, `UPDATE sessions SET mfa_at=? WHERE id_hash=?`, now, u.sessionHash)
	_, _ = a.db.ExecContext(ctx, `UPDATE users SET failed_logins=0 WHERE id=?`, u.ID)
	httpx.JSON(w, 200, map[string]any{"ok": true, "step_up_until": time.Unix(now, 0).Add(StepUpTTL).UTC()})
}

func (a *Auth) newRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	u := FromContext(r.Context())
	codes := NewRecoveryCodes(10)
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	if err := a.replaceRecovery(r.Context(), tx, u.ID, codes); err != nil || tx.Commit() != nil {
		httpx.Error(w, 500, "could not save recovery codes")
		return
	}
	_ = a.audit.Write(r.Context(), u.Actor(), "account.recovery_codes", "", nil, "ok", "")
	httpx.JSON(w, 200, map[string]any{"recovery_codes": codes})
}

func (a *Auth) reenrollStart(w http.ResponseWriter, r *http.Request) {
	u := FromContext(r.Context())
	sec := NewTOTPSecret()
	out, err := a.enrollPayload(r.Context(), u.ID, sec)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	out["ticket"] = a.box.SealString(fmt.Sprintf("%d|%s", a.now().Add(pendingTTL).Unix(), sec), fmt.Sprintf("reenroll:%d", u.ID))
	httpx.JSON(w, 200, out)
}

func (a *Auth) reenrollFinish(w http.ResponseWriter, r *http.Request) {
	u := FromContext(r.Context())
	var in struct {
		Ticket string `json:"ticket"`
		Code   string `json:"code"`
	}
	if err := httpx.Decode(r, 4096, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	raw, err := a.box.OpenString(in.Ticket, fmt.Sprintf("reenroll:%d", u.ID))
	exp, sec, _ := strings.Cut(raw, "|")
	if n, _ := strconv.ParseInt(exp, 10, 64); err != nil || a.now().Unix() > n {
		httpx.Error(w, 400, "This setup expired. Start again.")
		return
	}
	step, ok := VerifyTOTP(sec, in.Code, a.now(), 0)
	if !ok {
		httpx.Error(w, 400, "That code is not right. Check the time on your phone and try the next code.")
		return
	}
	codes, err := a.saveTOTP(r.Context(), u.ID, sec, step)
	if err != nil {
		httpx.Error(w, 500, "could not save 2FA")
		return
	}
	_ = a.audit.Write(r.Context(), u.Actor(), "auth.2fa_setup", "", map[string]any{"replaced": true}, "ok", "")
	httpx.JSON(w, 200, map[string]any{"recovery_codes": codes})
}

func (a *Auth) endSession(w http.ResponseWriter, r *http.Request) {
	u := FromContext(r.Context())
	id := r.PathValue("id")
	if len(id) != 16 {
		httpx.Error(w, 404, "no such session")
		return
	}
	res, _ := a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id=? AND substr(id_hash,1,16)=?`, u.ID, id)
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, 404, "no such session")
		return
	}
	_ = a.audit.Write(r.Context(), u.Actor(), "account.sign_out_session", "", nil, "ok", "")
	httpx.JSON(w, 200, map[string]bool{"ok": true})
}

func (a *Auth) endOtherSessions(w http.ResponseWriter, r *http.Request) {
	u := FromContext(r.Context())
	res, _ := a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id=? AND id_hash<>?`, u.ID, u.sessionHash)
	n, _ := res.RowsAffected()
	_ = a.audit.Write(r.Context(), u.Actor(), "account.sign_out_others", "", map[string]any{"sessions": n}, "ok", "")
	httpx.JSON(w, 200, map[string]any{"ended": n})
}

// unlink removes Steam or OIDC sign-in, keeping at least one way in.
func (a *Auth) unlink(col, method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := FromContext(r.Context())
		var pw string
		var steam, oidcSub sql.NullString
		_ = a.db.QueryRowContext(r.Context(), `SELECT password_hash, steam64, oidc_sub FROM users WHERE id=?`, u.ID).Scan(&pw, &steam, &oidcSub)
		ways := 0
		for _, ok := range []bool{pw != "", steam.Valid && col != "steam64", oidcSub.Valid && col != "oidc_sub"} {
			if ok {
				ways++
			}
		}
		if ways == 0 {
			httpx.Error(w, 400, "Set a password or link another sign-in first, or you could not sign in any more.")
			return
		}
		_, _ = a.db.ExecContext(r.Context(), `UPDATE users SET `+col+`=NULL WHERE id=?`, u.ID)
		_ = a.audit.Write(r.Context(), u.Actor(), "account.unlink", method, nil, "ok", "")
		httpx.JSON(w, 200, map[string]bool{"ok": true})
	}
}

// ---- user management (owners) ----

type userView struct {
	ID          int64   `json:"id"`
	Username    string  `json:"username"`
	Role        string  `json:"role"`
	Password    bool    `json:"password"`
	Steam64     string  `json:"steam64,omitempty"`
	OIDC        bool    `json:"oidc"`
	TOTP        bool    `json:"totp"`
	Disabled    bool    `json:"disabled"`
	CreatedAt   string  `json:"created_at"`
	LastLoginAt string  `json:"last_login_at"`
	Sessions    int     `json:"sessions"`
	LockedUntil *string `json:"locked_until,omitempty"`
	Self        bool    `json:"self"`
}

type inviteView struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Note      string `json:"note"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

func (a *Auth) listUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := FromContext(ctx)
	users := []userView{}
	rows, err := a.db.QueryContext(ctx, `SELECT u.id,u.username,u.role,u.password_hash<>'',COALESCE(u.steam64,''),u.oidc_sub IS NOT NULL,u.totp_secret<>'',u.disabled_at IS NOT NULL,u.created_at,u.last_login_at,u.locked_until,
		(SELECT COUNT(*) FROM sessions s WHERE s.user_id=u.id AND s.expires_at > ?) FROM users u ORDER BY u.disabled_at IS NOT NULL, u.username COLLATE NOCASE`, a.now().Unix())
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var v userView
		var locked int64
		if err := rows.Scan(&v.ID, &v.Username, &v.Role, &v.Password, &v.Steam64, &v.OIDC, &v.TOTP, &v.Disabled, &v.CreatedAt, &v.LastLoginAt, &locked, &v.Sessions); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		if t := time.Unix(locked, 0); t.After(a.now()) {
			s := t.UTC().Format(time.RFC3339)
			v.LockedUntil = &s
		}
		v.Self = v.ID == me.ID
		users = append(users, v)
	}
	invites := []inviteView{}
	irows, err := a.db.QueryContext(ctx, `SELECT id, role, note, created_by, created_at, expires_at FROM invites WHERE used_at IS NULL AND expires_at > ? ORDER BY created_at DESC`, a.now().Unix())
	if err == nil {
		defer irows.Close()
		for irows.Next() {
			var v inviteView
			var exp int64
			if irows.Scan(&v.ID, &v.Role, &v.Note, &v.CreatedBy, &v.CreatedAt, &exp) == nil {
				v.ExpiresAt = time.Unix(exp, 0).UTC().Format(time.RFC3339)
				invites = append(invites, v)
			}
		}
	}
	httpx.JSON(w, 200, map[string]any{"users": users, "invites": invites, "roles": perm.Roles, "permissions": perm.All, "methods": a.Methods()})
}

// CreateInvite makes a one-time invite link for a role (also used by the
// "dzm admin invite-owner" command).
func (a *Auth) CreateInvite(ctx context.Context, role, note, by string, ttl time.Duration) (id, link string, err error) {
	if !perm.ValidRole(role) {
		return "", "", errors.New("unknown role")
	}
	note = strings.TrimSpace(note)
	if len(note) > 80 {
		return "", "", errors.New("note too long (max 80 characters)")
	}
	token := secret.Token(24)
	id = secret.Token(6)
	_, err = a.db.ExecContext(ctx, `INSERT INTO invites(id,hash,role,note,created_by,created_at,expires_at) VALUES(?,?,?,?,?,?,?)`,
		id, a.box.Hash("invite:"+token), role, note, by, store.Now(), a.now().Add(ttl).Unix())
	if err != nil {
		return "", "", err
	}
	return id, a.cfg.PublicURL.String() + "/#/invite/" + token, nil
}

func (a *Auth) createInviteAPI(w http.ResponseWriter, r *http.Request) {
	u := FromContext(r.Context())
	var in struct {
		Role string `json:"role"`
		Note string `json:"note"`
		Days int    `json:"days"`
	}
	if err := httpx.Decode(r, 4096, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	if in.Days == 0 {
		in.Days = 2
	}
	if in.Days < 1 || in.Days > 7 {
		httpx.Error(w, 400, "an invite lasts 1 to 7 days")
		return
	}
	id, link, err := a.CreateInvite(r.Context(), in.Role, in.Note, u.Username, time.Duration(in.Days)*24*time.Hour)
	if err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	_ = a.audit.Write(r.Context(), u.Actor(), "users.invite", id, map[string]any{"role": in.Role, "note": in.Note, "days": in.Days}, "ok", "")
	httpx.JSON(w, 201, map[string]any{"id": id, "link": link})
}

func (a *Auth) deleteInvite(w http.ResponseWriter, r *http.Request) {
	u := FromContext(r.Context())
	id := r.PathValue("id")
	res, _ := a.db.ExecContext(r.Context(), `DELETE FROM invites WHERE id=? AND used_at IS NULL`, id)
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, 404, "no open invite with that id")
		return
	}
	_ = a.audit.Write(r.Context(), u.Actor(), "users.invite_delete", id, nil, "ok", "")
	httpx.JSON(w, 200, map[string]bool{"ok": true})
}

// activeOwners counts owners who can still sign in, other than exclude.
func (a *Auth) activeOwners(ctx context.Context, exclude int64) int {
	var n int
	_ = a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='owner' AND disabled_at IS NULL AND id<>?`, exclude).Scan(&n)
	return n
}

func (a *Auth) userAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		me := FromContext(ctx)
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var name, role string
		if err == nil {
			err = a.db.QueryRowContext(ctx, `SELECT username, role FROM users WHERE id=?`, id).Scan(&name, &role)
		}
		if err != nil {
			httpx.Error(w, 404, "no such user")
			return
		}
		if id == me.ID && action != "sign-out" {
			httpx.Error(w, 400, "You can't do this to your own account. Ask another owner.")
			return
		}
		lastOwner := role == string(perm.Owner) && a.activeOwners(ctx, id) == 0
		params := map[string]any{"user": name}
		var q string
		var args []any
		switch action {
		case "role":
			var in struct {
				Role string `json:"role"`
			}
			if err := httpx.Decode(r, 1024, &in); err != nil || !perm.ValidRole(in.Role) {
				httpx.Error(w, 400, "pick one of the roles")
				return
			}
			if lastOwner && in.Role != string(perm.Owner) {
				httpx.Error(w, 400, "There must always be an owner. Make someone else owner first.")
				return
			}
			params["from"], params["to"] = role, in.Role
			q, args = `UPDATE users SET role=? WHERE id=?`, []any{in.Role, id}
		case "disable":
			if lastOwner {
				httpx.Error(w, 400, "There must always be an owner who can sign in.")
				return
			}
			q, args = `UPDATE users SET disabled_at=? WHERE id=?`, []any{store.Now(), id}
		case "enable":
			q, args = `UPDATE users SET disabled_at=NULL, failed_logins=0, locked_until=0 WHERE id=?`, []any{id}
		case "reset-2fa":
			// They set up a new authenticator at their next sign-in.
			q, args = `UPDATE users SET totp_secret='', totp_last_step=0 WHERE id=?`, []any{id}
		case "sign-out":
			q, args = `DELETE FROM sessions WHERE user_id=?`, []any{id}
		case "delete":
			if lastOwner {
				httpx.Error(w, 400, "There must always be an owner.")
				return
			}
			q, args = `DELETE FROM users WHERE id=?`, []any{id}
		}
		if _, err := a.db.ExecContext(ctx, q, args...); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		// Anything that changes what a user may do ends their sessions now.
		switch action {
		case "role", "disable", "reset-2fa":
			_, _ = a.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, id)
		}
		if action == "reset-2fa" {
			_, _ = a.db.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=?`, id)
		}
		_ = a.audit.Write(ctx, me.Actor(), "users."+strings.ReplaceAll(action, "-", "_"), strconv.FormatInt(id, 10), params, "ok", "")
		httpx.JSON(w, 200, map[string]bool{"ok": true})
	}
}
