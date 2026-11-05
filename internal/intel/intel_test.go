package intel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kproios/dz-manager/internal/store"
)

func TestSteamProfileAndIPLookup(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenPath(ctx, filepath.Join(t.TempDir(), "dzm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const steam = "76561198000000001"
	if _, err := db.Exec(`INSERT INTO players(steam64,be_guid,first_seen,last_seen) VALUES(?,?,?,?)`, steam, "abcdef", "x", "x"); err != nil {
		t.Fatal(err)
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/profiles/"):
			_, _ = w.Write([]byte(`<?xml version="1.0"?><profile><steamID><![CDATA[Alice]]></steamID><privacyState>public</privacyState><vacBanned>1</vacBanned><tradeBanState>None</tradeBanState><isLimitedAccount>0</isLimitedAccount><avatarMedium><![CDATA[https://avatars.steamstatic.com/abc_medium.jpg]]></avatarMedium></profile>`))
		case r.URL.Path == "/batch":
			_, _ = w.Write([]byte(`[{"status":"success","country":"Greece","countryCode":"gr","isp":"Example ISP","proxy":true,"hosting":false,"query":"203.0.113.9"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	s := &Service{DB: db, SteamCommunity: fake.URL, IPAPI: fake.URL, Now: func() time.Time { return time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC) }}
	s.Init()
	s.Observe(ctx, []Seen{{BEGUID: "ABCDEF", IP: "203.0.113.9"}})
	if !s.flushIP(ctx) || !s.flushSteam(ctx, 10) {
		t.Fatal("nothing was queued")
	}
	in := s.Get(ctx, steam)
	if in.Persona != "Alice" || !in.VACBanned || in.Avatar == "" || in.CountryCode != "GR" || !in.Proxy || in.IP != "203.0.113.9" {
		t.Fatalf("intel: %+v", in)
	}
	if safeURL("javascript:alert(1)") != "" || safeURL("https://evil.example/x.jpg") != "" {
		t.Fatal("unsafe avatar URL accepted")
	}
}
