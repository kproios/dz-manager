// Package oidctest is a minimal in-process OIDC issuer for tests. It is never
// linked into the production binary.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// User is the identity the issuer will vouch for.
type User struct {
	Sub, Username, Email string
	Groups               []string
}

// Issuer is a fake SSO.
type Issuer struct {
	Server   *httptest.Server
	ClientID string
	Secret   string

	mu    sync.Mutex
	key   *rsa.PrivateKey
	User  User
	codes map[string]pending
	// Tamper hooks for negative tests.
	WrongAudience bool
	WrongNonce    bool
	Expired       bool
	WrongIssuer   bool
}

type pending struct {
	nonce, challenge, redirect string
}

// New starts an issuer.
func New() *Issuer {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	is := &Issuer{ClientID: "dzm-test", Secret: "s3cret", key: k, codes: map[string]pending{},
		User: User{Sub: "user-1", Username: "owner", Email: "owner@example.test", Groups: []string{"dzm-admins"}}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", is.discovery)
	mux.HandleFunc("/jwks", is.jwks)
	mux.HandleFunc("/authorize", is.authorize)
	mux.HandleFunc("/token", is.token)
	mux.HandleFunc("/end-session", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	is.Server = httptest.NewServer(mux)
	return is
}

// URL is the issuer URL.
func (is *Issuer) URL() string { return is.Server.URL }

// Close stops the server.
func (is *Issuer) Close() { is.Server.Close() }

func (is *Issuer) discovery(w http.ResponseWriter, _ *http.Request) {
	u := is.URL()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer": u, "authorization_endpoint": u + "/authorize", "token_endpoint": u + "/token",
		"jwks_uri": u + "/jwks", "end_session_endpoint": u + "/end-session",
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (is *Issuer) jwks(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &is.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
}

func (is *Issuer) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != is.ClientID || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "bad request", 400)
		return
	}
	code := randStr()
	is.mu.Lock()
	is.codes[code] = pending{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
	is.mu.Unlock()
	http.Redirect(w, r, q.Get("redirect_uri")+"?"+url.Values{"code": {code}, "state": {q.Get("state")}}.Encode(), http.StatusFound)
}

func (is *Issuer) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, sec, ok := r.BasicAuth()
	if !ok {
		id, sec = r.Form.Get("client_id"), r.Form.Get("client_secret")
	}
	if id != is.ClientID || sec != is.Secret {
		http.Error(w, `{"error":"invalid_client"}`, 401)
		return
	}
	is.mu.Lock()
	p, ok := is.codes[r.Form.Get("code")]
	delete(is.codes, r.Form.Get("code"))
	user := is.User
	is.mu.Unlock()
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != p.challenge || r.Form.Get("redirect_uri") != p.redirect {
		http.Error(w, `{"error":"invalid_grant"}`, 400)
		return
	}
	now := time.Now()
	claims := map[string]any{
		"iss": is.URL(), "sub": user.Sub, "aud": is.ClientID, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"nonce": p.nonce, "preferred_username": user.Username, "email": user.Email, "groups": user.Groups,
	}
	if is.WrongAudience {
		claims["aud"] = "someone-else"
	}
	if is.WrongNonce {
		claims["nonce"] = "wrong"
	}
	if is.Expired {
		claims["exp"] = now.Add(-time.Hour).Unix()
	}
	if is.WrongIssuer {
		claims["iss"] = "https://evil.example"
	}
	sig, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: is.key}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
	payload, _ := json.Marshal(claims)
	obj, _ := sig.Sign(payload)
	idt, _ := obj.CompactSerialize()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": randStr(), "token_type": "Bearer", "expires_in": 300, "id_token": idt})
}

func randStr() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
