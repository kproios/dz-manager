package profiles_test

import (
	"testing"

	"github.com/kproios/dz-manager/internal/apptest"
	"github.com/kproios/dz-manager/internal/profiles"
)

func TestProfileLifecycle(t *testing.T) {
	e := apptest.New(t, "")
	e.Login()
	var a, b profiles.Profile
	if code := e.JSON("POST", "/api/profiles", profiles.Input{Name: "TestServer", ProviderKind: profiles.SelfhostedArchitect, GameHost: "192.0.2.10", RconPassword: "pw1"}, &a); code != 201 {
		t.Fatalf("create = %d", code)
	}
	if !a.Active || !a.HasRconPassword || a.RconPort != 2305 || a.QueryPort != 27016 {
		t.Fatalf("first profile: %+v", a)
	}
	e.JSON("POST", "/api/profiles", profiles.Input{Name: "VYKIX", ProviderKind: profiles.VYKIX, GameHost: "203.0.113.5"}, &b)
	if b.Active {
		t.Fatal("second profile must not auto-activate")
	}
	// Endpoint change keeps server_id and the stored password.
	var upd profiles.Profile
	e.JSON("PUT", "/api/profiles/"+a.ID, profiles.Input{Name: "TestServer", ProviderKind: profiles.SelfhostedArchitect, GameHost: "192.0.2.20"}, &upd)
	if upd.ID != a.ID || upd.GameHost != "192.0.2.20" || !upd.HasRconPassword {
		t.Fatalf("update: %+v", upd)
	}
	_, sec, _ := e.App.Profiles.Get(e.Ctx, a.ID)
	if sec.RconPassword != "pw1" {
		t.Fatal("password lost on update")
	}
	// Switch active.
	if code := e.JSON("POST", "/api/profiles/"+b.ID+"/activate", nil, nil); code != 200 {
		t.Fatalf("activate = %d", code)
	}
	act, _, _ := e.App.Profiles.Active(e.Ctx)
	if act.ID != b.ID {
		t.Fatal("activate did not switch")
	}
	// Wizard + ingest credentials; rotation invalidates old ones.
	var wz profiles.Wizard
	e.JSON("GET", "/api/profiles/"+a.ID+"/wizard", nil, &wz)
	http := wz.MetricZConfig["http"].(map[string]any)
	user, pass := http["user"].(string), http["password"].(string)
	if id, ok := e.App.Profiles.AuthMetricZ(e.Ctx, user, pass); !ok || id != a.ID {
		t.Fatal("metricz auth failed")
	}
	if _, ok := e.App.Profiles.AuthMetricZ(e.Ctx, user, "wrong"); ok {
		t.Fatal("metricz wrong password accepted")
	}
	tok := wz.BridgeConfig["server_token"].(string)
	e.JSON("POST", "/api/profiles/"+a.ID+"/rotate-ingest", nil, nil)
	if _, ok := e.App.Profiles.AuthBridge(e.Ctx, tok); ok {
		t.Fatal("old bridge token still valid after rotation")
	}
	// Archive keeps the row.
	e.JSON("POST", "/api/profiles/"+a.ID+"/archive", nil, nil)
	var list []profiles.Profile
	e.JSON("GET", "/api/profiles", nil, &list)
	if len(list) != 2 || !list[1].Archived {
		t.Fatalf("archive: %+v", list)
	}
	// Validation.
	if code := e.JSON("POST", "/api/profiles", profiles.Input{Name: ""}, nil); code != 400 {
		t.Fatalf("empty name = %d", code)
	}
	if code := e.JSON("POST", "/api/profiles", profiles.Input{Name: "x", RconPort: 70000}, nil); code != 400 {
		t.Fatalf("bad port = %d", code)
	}
	if code := e.JSON("POST", "/api/profiles", profiles.Input{Name: "x", ProviderKind: "aws"}, nil); code != 400 {
		t.Fatalf("bad provider = %d", code)
	}
}
