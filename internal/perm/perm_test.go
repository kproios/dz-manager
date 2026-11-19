package perm

import (
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every "METHOD /api/..." route registered anywhere in the code must have a
// rule, and every rule must belong to a registered route. A new route without
// a permission fails here; at run time it would be refused for everyone.
func TestEveryAPIRouteHasARule(t *testing.T) {
	re := regexp.MustCompile(`Handle(?:Func)?\("((?:GET|POST|PUT|PATCH|DELETE) /api/[^"]*|/api/)"`)
	found := map[string]string{}
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			found[m[1]] = p
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) < 50 {
		t.Fatalf("found only %d routes; the scan is broken", len(found))
	}
	for pat, file := range found {
		if _, ok := Routes[pat]; !ok {
			t.Errorf("%s (%s) has no permission rule in perm.Routes", pat, file)
		}
	}
	for pat := range Routes {
		if _, ok := found[pat]; !ok {
			t.Errorf("perm.Routes has %q, but no such route is registered", pat)
		}
	}
}

func TestRoles(t *testing.T) {
	if !Has(Owner, UsersManage) || Has(Admin, UsersManage) || Has(Admin, ServersManage) {
		t.Fatal("owner/admin split wrong")
	}
	if Has(Moderator, ActionsStrong) || !Has(Moderator, ActionsSafe) || Has(Viewer, PlayersPrivate) {
		t.Fatal("moderator/viewer split wrong")
	}
	if !Has(Viewer, Self) || Has("nobody", Self) || Has("nobody", ViewLive) {
		t.Fatal("unknown roles must have nothing")
	}
	for _, r := range Roles {
		for _, p := range r.Perms {
			found := false
			for _, i := range All {
				found = found || i.Name == p
			}
			if !found {
				t.Errorf("role %s has unlisted permission %s", r.Name, p)
			}
		}
	}
}

func TestDynamicRules(t *testing.T) {
	cases := []struct {
		pattern, url string
		want         Permission
		stepUp       bool
	}{
		{"POST /api/players/{id}/actions/{action}", "/api/players/7656/actions/heal", ActionsSafe, false},
		{"POST /api/players/{id}/actions/{action}", "/api/players/7656/actions/kill", ActionsStrong, true},
		{"POST /api/players/{id}/actions/{action}", "/api/players/7656/actions/give_item", ActionsStrong, true},
		{"POST /api/vehicles/{id}/actions/{action}", "/api/vehicles/1:2/actions/vehicle_delete", ActionsStrong, true},
		{"POST /api/vehicles/{id}/actions/{action}", "/api/vehicles/1:2/actions/vehicle_refuel", ActionsSafe, false},
		{"GET /api/server/players", "/api/server/players", ViewLive, false},
		{"GET /api/server/players", "/api/server/players?reveal_ip=1", PlayersPrivate, false},
		{"GET /api/players/{id}/intel", "/api/players/7656/intel?reveal_ip=1", PlayersPrivate, false},
	}
	for _, c := range cases {
		p, s, ok := Resolve(c.pattern, httptest.NewRequest("GET", c.url, nil))
		if !ok || p != c.want || s != c.stepUp {
			t.Errorf("%s: got %s step-up %v ok %v, want %s %v", c.url, p, s, ok, c.want, c.stepUp)
		}
	}
	if _, _, ok := Resolve("GET /api/nope", httptest.NewRequest("GET", "/api/nope", nil)); ok {
		t.Fatal("unknown route resolved")
	}
}
