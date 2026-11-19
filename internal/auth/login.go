package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/httpx"
	"github.com/kproios/dz-manager/internal/perm"
	"github.com/kproios/dz-manager/internal/secret"
	"github.com/kproios/dz-manager/internal/store"
)

// Issuer is the name authenticator apps show next to the code.
const Issuer = "DZ Manager"

// pending is the half-finished sign-in between the first factor (password,
// Steam or OIDC) and the second (TOTP). It lives only in a sealed cookie.
type pending struct {
	UID    int64  `json:"u"`
	Method string `json:"m"`
	Kind   string `json:"k"`           // "totp" (enter a code) or "enroll" (set up 2FA first)
	Secret string `json:"s,omitempty"` // enroll: the TOTP secret being set up
	Return string `json:"r,omitempty"`
	Exp    int64  `json:"e"`
}

func (a *Auth) setPending(w http.ResponseWriter, p pending) {
	p.Exp = a.now().Add(pendingTTL).Unix()
	b, _ := json.Marshal(p)
	a.setCookie(w, a.pendingCookieName(), a.box.SealString(string(b), "pending"), "/", pendingTTL)
}

func (a *Auth) getPending(r *http.Request) (pending, bool) {
	var p pending
	c, err := r.Cookie(a.pendingCookieName())
	if err != nil {
		return p, false
	}
	raw, err := a.box.OpenString(c.Value, "pending")
	if err != nil || json.Unmarshal([]byte(raw), &p) != nil || a.now().Unix() > p.Exp || p.UID == 0 {
		return pending{}, false
	}
	return p, true
}

// afterFirstFactor asks for a code only when the account has enabled 2FA.
func (a *Auth) afterFirstFactor(ctx context.Context, w http.ResponseWriter, r *http.Request, uid int64, method, ret string) string {
	var enc string
	if err := a.db.QueryRowContext(ctx, `SELECT totp_secret FROM users WHERE id=? AND disabled_at IS NULL`, uid).Scan(&enc); err != nil {
		httpx.Error(w, 500, "could not read account")
		return ""
	}
	if enc == "" {
		if err := a.createSession(ctx, w, r, uid, method, false); err != nil {
			httpx.Error(w, 500, "could not create session")
			return ""
		}
		_ = a.audit.Write(ctx, a.userActor(ctx, uid), "auth.login", "", map[string]any{"method": method, "second_factor": "not enabled", "ip": ClientIP(r)}, "ok", "")
		return "signed_in"
	}
	p := pending{UID: uid, Method: method, Kind: "totp", Return: safeReturn(ret)}
	a.setPending(w, p)
	return p.Kind
}

// Register adds the sign-in routes (all under /auth/) to the root mux.
func (a *Auth) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/state", a.state)
	mux.HandleFunc("POST /auth/password", a.post(a.passwordLogin))
	mux.HandleFunc("POST /auth/totp", a.post(a.totpStep))
	mux.HandleFunc("POST /auth/enroll/start", a.post(a.enrollStart))
	mux.HandleFunc("POST /auth/enroll/finish", a.post(a.enrollFinish))
	mux.HandleFunc("POST /auth/enroll/skip", a.post(a.enrollSkip))
	mux.HandleFunc("POST /auth/cancel", a.post(func(w http.ResponseWriter, r *http.Request) {
		a.setCookie(w, a.pendingCookieName(), "", "/", 0)
		writeJSON(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /auth/invite/{token}", a.inviteInfo)
	mux.HandleFunc("POST /auth/invite/{token}/password", a.post(a.invitePassword))
	mux.HandleFunc("POST /auth/logout", a.Logout)
	// The old entry point: single sign-on when configured, else the login page.
	mux.HandleFunc("GET /auth/login", func(w http.ResponseWriter, r *http.Request) {
		if a.oidc != nil {
			a.oidc.login(w, r)
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
	})
	if a.steam != nil {
		mux.HandleFunc("GET /auth/steam/login", a.steam.login)
		mux.HandleFunc("GET /auth/steam/callback", a.steam.callback)
	}
	if a.oidc != nil {
		mux.HandleFunc("GET /auth/oidc/login", a.oidc.login)
		mux.HandleFunc("GET /auth/callback", a.oidc.callback)
	}
}

// post wraps a JSON POST handler of the sign-in flow with the Origin check.
func (a *Auth) post(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.sameOrigin(r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request refused"})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		h(w, r)
	}
}

func (a *Auth) state(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"methods": a.Methods(), "signed_in": false}
	if u, err := a.Session(r); err == nil {
		out["signed_in"] = true
		out["username"] = u.Username
	} else if p, ok := a.getPending(r); ok {
		out["pending"] = p.Kind
	}
	var n int
	_ = a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&n)
	out["setup_needed"] = n == 0
	writeJSON(w, 200, out)
}

func (a *Auth) tooManyFromIP(w http.ResponseWriter, r *http.Request) bool {
	if a.ipFails.blocked(ClientIP(r), a.now()) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "Too many failed sign-ins from your network. Wait 15 minutes and try again."})
		return true
	}
	return false
}

func (a *Auth) lockedReply(w http.ResponseWriter, d time.Duration) {
	writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": fmt.Sprintf("This account is locked for %s after failed sign-ins.", d.Round(time.Second))})
}

func (a *Auth) passwordLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if a.tooManyFromIP(w, r) {
		return
	}
	var in struct {
		Login    string `json:"login"`
		Password string `json:"password"`
		Return   string `json:"return"`
	}
	if err := httpx.Decode(r, 4096, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	login := strings.ToLower(strings.TrimSpace(in.Login))
	var uid int64
	var name, hash string
	var disabled sql.NullString
	err := a.db.QueryRowContext(ctx, `SELECT id, username, password_hash, disabled_at FROM users WHERE login=?`, login).Scan(&uid, &name, &hash, &disabled)
	if err != nil {
		hash, uid = "", 0
	}
	if uid > 0 {
		if d, locked := a.accountLocked(ctx, uid); locked {
			a.lockedReply(w, d)
			return
		}
	}
	ok := VerifyPassword(in.Password, hash) // runs the hash even for unknown users
	if !ok || uid == 0 || disabled.Valid {
		actor := audit.Actor{Sub: "anonymous", Name: firstNonEmpty(name, "unknown user")}
		why := "wrong user name or password"
		if ok && disabled.Valid {
			why = "account disabled"
		}
		a.failLogin(ctx, r, uid, actor, why)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Wrong user name or password."})
		return
	}
	if next := a.afterFirstFactor(ctx, w, r, uid, "password", in.Return); next != "" {
		writeJSON(w, 200, map[string]string{"next": next})
	}
}

// checkSecondFactor verifies a TOTP code (refusing reuse) or a recovery code.
func (a *Auth) checkSecondFactor(ctx context.Context, uid int64, code, recovery string) (string, bool) {
	if recovery != "" {
		h := a.box.Hash("recovery:" + normRecovery(recovery))
		res, err := a.db.ExecContext(ctx, `UPDATE recovery_codes SET used_at=? WHERE user_id=? AND hash=? AND used_at IS NULL`, store.Now(), uid, h)
		if err != nil {
			return "", false
		}
		n, _ := res.RowsAffected()
		return "recovery code", n == 1
	}
	var enc string
	var last int64
	if a.db.QueryRowContext(ctx, `SELECT totp_secret, totp_last_step FROM users WHERE id=?`, uid).Scan(&enc, &last) != nil || enc == "" {
		return "", false
	}
	sec, err := a.box.OpenString(enc, fmt.Sprintf("totp:%d", uid))
	if err != nil {
		return "", false
	}
	step, ok := VerifyTOTP(sec, code, a.now(), last)
	if !ok {
		return "", false
	}
	// Conditional update: two requests racing with the same code can't both win.
	res, err := a.db.ExecContext(ctx, `UPDATE users SET totp_last_step=? WHERE id=? AND totp_last_step < ?`, step, uid, step)
	if err != nil {
		return "", false
	}
	n, _ := res.RowsAffected()
	return "authenticator code", n == 1
}

func (a *Auth) userActor(ctx context.Context, uid int64) audit.Actor {
	var sub, name string
	_ = a.db.QueryRowContext(ctx, `SELECT sub, username FROM users WHERE id=?`, uid).Scan(&sub, &name)
	return audit.Actor{Sub: sub, Name: name}
}

func (a *Auth) totpStep(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := a.getPending(r)
	if !ok || p.Kind != "totp" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Your sign-in expired. Start again."})
		return
	}
	if a.tooManyFromIP(w, r) {
		return
	}
	if d, locked := a.accountLocked(ctx, p.UID); locked {
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
	actor := a.userActor(ctx, p.UID)
	via, ok := a.checkSecondFactor(ctx, p.UID, in.Code, in.Recovery)
	if !ok {
		a.failLogin(ctx, r, p.UID, actor, "wrong 2FA code")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "That code is not right (or was already used)."})
		return
	}
	if err := a.createSession(ctx, w, r, p.UID, p.Method, true); err != nil {
		httpx.Error(w, 500, "could not create session")
		return
	}
	_ = a.audit.Write(ctx, actor, "auth.login", "", map[string]any{"method": p.Method, "second_factor": via, "ip": ClientIP(r)}, "ok", "")
	writeJSON(w, 200, map[string]any{"ok": true, "return": p.Return, "recovery_used": via == "recovery code"})
}

func (a *Auth) enrollPayload(ctx context.Context, uid int64, sec string) (map[string]string, error) {
	var name string
	_ = a.db.QueryRowContext(ctx, `SELECT username FROM users WHERE id=?`, uid).Scan(&name)
	uri := TOTPURI(Issuer, name, sec)
	qr, err := QRDataURI(uri)
	if err != nil {
		return nil, err
	}
	return map[string]string{"secret": sec, "uri": uri, "qr": qr, "account": name, "issuer": Issuer}, nil
}

func (a *Auth) enrollStart(w http.ResponseWriter, r *http.Request) {
	p, ok := a.getPending(r)
	if !ok || p.Kind != "enroll" || p.Secret == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Your sign-in expired. Start again."})
		return
	}
	out, err := a.enrollPayload(r.Context(), p.UID, p.Secret)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, out)
}

// saveTOTP stores a verified secret and returns fresh recovery codes.
func (a *Auth) saveTOTP(ctx context.Context, uid int64, sec string, step int64, sessionHash string) ([]string, error) {
	codes := NewRecoveryCodes(10)
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	query := `UPDATE users SET totp_secret=?, totp_last_step=? WHERE id=? AND disabled_at IS NULL`
	if sessionHash == "" {
		query += ` AND totp_secret=''`
	}
	res, err := tx.ExecContext(ctx, query, a.box.SealString(sec, fmt.Sprintf("totp:%d", uid)), step, uid)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return nil, errors.New("account is unavailable or 2FA is already enabled")
	}
	if err := a.replaceRecovery(ctx, tx, uid, codes); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=? AND id_hash<>?`, uid, sessionHash); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET mfa_at=? WHERE user_id=? AND id_hash=?`, a.now().Unix(), uid, sessionHash); err != nil {
		return nil, err
	}
	return codes, tx.Commit()
}

// enrollSkip lets a pending enrollment from an older release continue without
// enabling 2FA. A pending code challenge can never take this path.
func (a *Auth) enrollSkip(w http.ResponseWriter, r *http.Request) {
	p, ok := a.getPending(r)
	if !ok || p.Kind != "enroll" {
		httpx.Error(w, 401, "Your sign-in expired. Start again.")
		return
	}
	var enabled bool
	if err := a.db.QueryRowContext(r.Context(), `SELECT totp_secret<>'' FROM users WHERE id=? AND disabled_at IS NULL`, p.UID).Scan(&enabled); err != nil || enabled {
		httpx.Error(w, 401, "Sign in again to continue.")
		return
	}
	if next := a.afterFirstFactor(r.Context(), w, r, p.UID, p.Method, p.Return); next != "" {
		writeJSON(w, 200, map[string]string{"next": next})
	}
}

func (a *Auth) replaceRecovery(ctx context.Context, tx *sql.Tx, uid int64, codes []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=?`, uid); err != nil {
		return err
	}
	for _, c := range codes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes(user_id,hash) VALUES(?,?)`, uid, a.box.Hash("recovery:"+normRecovery(c))); err != nil {
			return err
		}
	}
	return nil
}

func (a *Auth) enrollFinish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := a.getPending(r)
	if !ok || p.Kind != "enroll" || p.Secret == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Your sign-in expired. Start again."})
		return
	}
	if a.tooManyFromIP(w, r) {
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if err := httpx.Decode(r, 1024, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	step, ok := VerifyTOTP(p.Secret, in.Code, a.now(), 0)
	if !ok {
		a.ipFails.add(ClientIP(r), a.now())
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "That code is not right. Check the time on your phone and try the next code."})
		return
	}
	codes, err := a.saveTOTP(ctx, p.UID, p.Secret, step, "")
	if err != nil {
		httpx.Error(w, 500, "could not save 2FA")
		return
	}
	if err := a.createSession(ctx, w, r, p.UID, p.Method, true); err != nil {
		httpx.Error(w, 500, "could not create session")
		return
	}
	actor := a.userActor(ctx, p.UID)
	_ = a.audit.Write(ctx, actor, "auth.2fa_setup", "", nil, "ok", "")
	_ = a.audit.Write(ctx, actor, "auth.login", "", map[string]any{"method": p.Method, "second_factor": "new authenticator", "ip": ClientIP(r)}, "ok", "")
	writeJSON(w, 200, map[string]any{"ok": true, "return": p.Return, "recovery_codes": codes})
}

// ---- invites (the public side; managing them is in users.go) ----

type inviteRow struct {
	ID      string
	Role    string
	Expires int64
}

var errInvite = errors.New("This invite link is not valid any more. Ask for a new one.")

func (a *Auth) findInvite(ctx context.Context, token string) (inviteRow, error) {
	var iv inviteRow
	if token == "" || len(token) > 100 {
		return iv, errInvite
	}
	err := a.db.QueryRowContext(ctx, `SELECT id, role, expires_at FROM invites WHERE hash=? AND used_at IS NULL`, a.box.Hash("invite:"+token)).Scan(&iv.ID, &iv.Role, &iv.Expires)
	if err != nil || a.now().Unix() > iv.Expires || !perm.ValidRole(iv.Role) {
		return iv, errInvite
	}
	return iv, nil
}

func (a *Auth) inviteInfo(w http.ResponseWriter, r *http.Request) {
	if a.tooManyFromIP(w, r) {
		return
	}
	iv, err := a.findInvite(r.Context(), r.PathValue("token"))
	if err != nil {
		a.ipFails.add(ClientIP(r), a.now())
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	label := iv.Role
	for _, ri := range perm.Roles {
		if string(ri.Name) == iv.Role {
			label = ri.Label
		}
	}
	writeJSON(w, 200, map[string]any{"role": iv.Role, "role_label": label, "expires_at": time.Unix(iv.Expires, 0).UTC(), "methods": a.Methods()})
}

var reUsername = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._-]{1,31}$`)

// checkUsername validates a display name and that nobody else has it.
func (a *Auth) checkUsername(ctx context.Context, name string) error {
	if !reUsername.MatchString(name) {
		return errors.New("Pick a name of 2 to 32 letters, digits, spaces, dots, dashes or underscores.")
	}
	var n int
	_ = a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE lower(username)=lower(?) OR login=lower(?)`, name, name).Scan(&n)
	if n > 0 {
		return errors.New("That name is taken. Pick another.")
	}
	return nil
}

// acceptInvite uses an invite once and creates the user. set holds the
// sign-in method columns (login+password_hash, steam64 or oidc_sub).
func (a *Auth) acceptInvite(ctx context.Context, r *http.Request, token, name string, set map[string]string, method string) (int64, error) {
	iv, err := a.findInvite(ctx, token)
	if err != nil {
		return 0, err
	}
	name = strings.TrimSpace(name)
	if err := a.checkUsername(ctx, name); err != nil {
		return 0, err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE invites SET used_at=? WHERE id=? AND used_at IS NULL`, store.Now(), iv.ID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, errInvite
	}
	ts := store.Now()
	var uid int64
	err = tx.QueryRowContext(ctx, `INSERT INTO users(sub,username,email,created_at,last_login_at,role,login,password_hash,steam64,oidc_sub) VALUES(?,?,'',?,?,?,?,?,?,?) RETURNING id`,
		"dzm:"+secret.UUID(), name, ts, ts, iv.Role, nullIfEmpty(set["login"]), set["password_hash"], nullIfEmpty(set["steam64"]), nullIfEmpty(set["oidc_sub"])).Scan(&uid)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, errors.New("That sign-in is already linked to another DZ Manager account.")
		}
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE invites SET used_by=? WHERE id=?`, uid, iv.ID); err != nil {
		return 0, err
	}
	// The first owner also takes over API keys made before any user existed.
	if iv.Role == string(perm.Owner) {
		_, _ = tx.ExecContext(ctx, `UPDATE api_keys SET user_id=? WHERE user_id IS NULL`, uid)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	_ = a.audit.Write(ctx, a.userActor(ctx, uid), "users.invite_accepted", iv.ID, map[string]any{"role": iv.Role, "method": method, "ip": ClientIP(r)}, "ok", "")
	return uid, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (a *Auth) invitePassword(w http.ResponseWriter, r *http.Request) {
	if a.tooManyFromIP(w, r) {
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, 4096, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	name := strings.TrimSpace(in.Username)
	if err := CheckPasswordPolicy(in.Password, name); err != nil {
		writeJSON(w, 400, map[string]string{"error": "Password: " + err.Error()})
		return
	}
	uid, err := a.acceptInvite(r.Context(), r, r.PathValue("token"), name, map[string]string{"login": strings.ToLower(name), "password_hash": HashPassword(in.Password)}, "password")
	if err != nil {
		if errors.Is(err, errInvite) {
			a.ipFails.add(ClientIP(r), a.now())
		}
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if next := a.afterFirstFactor(r.Context(), w, r, uid, "password", "/"); next != "" {
		writeJSON(w, 200, map[string]string{"next": next})
	}
}
