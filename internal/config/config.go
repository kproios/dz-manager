// Package config loads and validates the DZ Manager runtime configuration
// from environment variables (see docs/contracts/host-and-release.md §3).
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config is the validated runtime configuration.
type Config struct {
	Listen          string
	DataDir         string
	PublicURL       *url.URL
	IngestPublicURL *url.URL
	Location        *time.Location
	LogLevel        string
	AlertWebhook    string
	SecretKey       []byte
	// MapMaxPx is the largest map image (px per side) the server will tile.
	MapMaxPx int
	// Container is set by the official Docker image: the process listens on
	// all interfaces inside the container and the host publishes the port.
	Container bool

	// Optional OpenID Connect provider, offered as one more sign-in method.
	// Who may sign in and with which role is decided in DZ Manager.
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCLabel        string
	// SteamLogin offers "Sign in with Steam" (on unless DZM_STEAM_LOGIN=0).
	SteamLogin bool
}

// SecureCookies reports whether cookies must carry the Secure attribute.
// Always true in production (https); only plain-http loopback URLs used by
// tests and local development disable it.
func (c *Config) SecureCookies() bool { return c.PublicURL.Scheme == "https" }

// PublicOrigin returns scheme://host of the UI URL, used for Origin checks.
func (c *Config) PublicOrigin() string { return c.PublicURL.Scheme + "://" + c.PublicURL.Host }

// FromEnv builds a Config using getenv (normally os.Getenv). All problems are
// collected so the operator sees every misconfiguration at once.
func FromEnv(getenv func(string) string) (*Config, error) {
	var errs []error
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	c := &Config{
		Listen:           get("DZM_LISTEN", "127.0.0.1:8080"),
		DataDir:          get("DZM_DATA_DIR", "/var/lib/dzm"),
		LogLevel:         strings.ToLower(get("DZM_LOG_LEVEL", "info")),
		AlertWebhook:     get("DZM_ALERT_WEBHOOK", ""),
		OIDCIssuer:       get("DZM_OIDC_ISSUER", ""),
		OIDCClientID:     get("DZM_OIDC_CLIENT_ID", ""),
		OIDCClientSecret: get("DZM_OIDC_CLIENT_SECRET", ""),
		OIDCLabel:        get("DZM_OIDC_LABEL", "Single sign-on"),
		SteamLogin:       get("DZM_STEAM_LOGIN", "1") != "0",
	}

	c.Container = get("DZM_CONTAINER", "") == "1"
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		errs = append(errs, fmt.Errorf("DZM_LISTEN: %w", err))
	} else if ip := net.ParseIP(host); !c.Container && host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		errs = append(errs, errors.New("DZM_LISTEN must be a loopback address (put a reverse proxy or tunnel in front; the Docker image sets DZM_CONTAINER=1)"))
	}

	c.MapMaxPx = 10000
	if v := get("DZM_MAP_MAX_PX", ""); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n < 1024 || n > 20000 {
			errs = append(errs, errors.New("DZM_MAP_MAX_PX must be a number of pixels between 1024 and 20000"))
		} else {
			c.MapMaxPx = n
		}
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("DZM_LOG_LEVEL %q invalid (debug|info|warn|error)", c.LogLevel))
	}

	keyRaw := get("DZM_SECRET_KEY", "")
	if keyRaw == "" {
		errs = append(errs, errors.New("DZM_SECRET_KEY is required (32 random bytes, base64)"))
	} else if k, err := base64.StdEncoding.DecodeString(keyRaw); err != nil || len(k) != 32 {
		errs = append(errs, errors.New("DZM_SECRET_KEY must be exactly 32 bytes encoded as standard base64"))
	} else {
		c.SecretKey = k
	}

	c.PublicURL = parseURL("DZM_PUBLIC_URL", get("DZM_PUBLIC_URL", ""), &errs)
	c.IngestPublicURL = parseURL("DZM_INGEST_PUBLIC_URL", get("DZM_INGEST_PUBLIC_URL", ""), &errs)

	// OpenID Connect is optional: all three values or none.
	switch n := countSet(c.OIDCIssuer, c.OIDCClientID, c.OIDCClientSecret); {
	case n == 0:
	case n < 3:
		errs = append(errs, errors.New("DZM_OIDC_ISSUER, DZM_OIDC_CLIENT_ID and DZM_OIDC_CLIENT_SECRET go together: set all three to offer single sign-on, or none"))
	default:
		if u, err := url.Parse(c.OIDCIssuer); err != nil || u.Host == "" || (u.Scheme != "https" && !isLoopbackHost(u.Hostname())) {
			errs = append(errs, errors.New("DZM_OIDC_ISSUER must be an https URL"))
		}
	}
	if len(c.OIDCLabel) > 40 {
		errs = append(errs, errors.New("DZM_OIDC_LABEL must be at most 40 characters"))
	}

	tz := get("DZM_TZ", "UTC")
	if loc, err := time.LoadLocation(tz); err != nil {
		errs = append(errs, fmt.Errorf("DZM_TZ %q: %w", tz, err))
	} else {
		c.Location = loc
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return c, nil
}

func parseURL(name, raw string, errs *[]error) *url.URL {
	if raw == "" {
		*errs = append(*errs, fmt.Errorf("%s is required", name))
		return nil
	}
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Host == "" {
		*errs = append(*errs, fmt.Errorf("%s %q is not a valid absolute URL", name, raw))
		return nil
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		*errs = append(*errs, fmt.Errorf("%s must use https (plain http is allowed only for loopback testing)", name))
		return nil
	}
	return u
}

func countSet(v ...string) int {
	n := 0
	for _, s := range v {
		if s != "" {
			n++
		}
	}
	return n
}

func isLoopbackHost(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
