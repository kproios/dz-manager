package priority_test

import (
	"io"
	"strings"
	"testing"

	"github.com/kproios/dz-manager/internal/apptest"
)

func TestPriority(t *testing.T) {
	e := apptest.New(t, "")
	e.Login()
	if code := e.JSON("POST", "/api/priority", map[string]any{"steam64": "76561198000000001", "reason": "supporter"}, nil); code != 201 {
		t.Fatalf("add = %d", code)
	}
	e.JSON("POST", "/api/priority", map[string]any{"steam64": "76561198000000002", "reason": "trial", "days": 7}, nil)
	if code := e.JSON("POST", "/api/priority", map[string]any{"steam64": "76561198000000001"}, nil); code != 409 {
		t.Fatalf("duplicate = %d", code)
	}
	if code := e.JSON("POST", "/api/priority", map[string]any{"steam64": "123"}, nil); code != 400 {
		t.Fatalf("invalid id = %d", code)
	}
	e.JSON("POST", "/api/priority/76561198000000001/applied", map[string]any{"applied": true}, nil)
	var out struct {
		Entries []struct {
			Steam64 string
			Applied bool
			Expired bool
		}
	}
	e.JSON("GET", "/api/priority", nil, &out)
	if len(out.Entries) != 2 || !out.Entries[0].Applied || out.Entries[1].Applied {
		t.Fatalf("list: %+v", out)
	}
	// Expired entries are excluded from the export.
	e.App.DB.Exec(`UPDATE priority SET expires_at='2000-01-01T00:00:00Z' WHERE steam64='76561198000000002'`)
	resp := e.Do("GET", "/api/priority/export", nil)
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.TrimSpace(string(b)) != "76561198000000001" {
		t.Fatalf("export: %q", b)
	}
	if code := e.JSON("POST", "/api/priority/76561198000000001/remove", nil, nil); code != 200 {
		t.Fatalf("remove = %d", code)
	}
	if code := e.JSON("POST", "/api/priority/76561198000000001/remove", nil, nil); code != 404 {
		t.Fatalf("remove missing = %d", code)
	}
	var n int
	e.App.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action LIKE 'priority.%'`).Scan(&n)
	if n != 4 {
		t.Fatalf("audit rows = %d", n)
	}
}
