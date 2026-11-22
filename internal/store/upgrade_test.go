package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A database created before migration 10 had a different self-hosted provider
// value and a CHECK constraint that allowed only it. Migration 10 must turn
// every such row into 'selfhosted' and leave a constraint that allows only the
// generic names, with foreign keys still on.
func TestProviderKindUpgrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dzm.db")
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	ms, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.version >= 10 {
			break
		}
		sqlText := m.sql
		if m.version == 1 {
			sqlText = strings.Replace(sqlText, "('selfhosted','managed','generic')", "('selfhosted_old','managed','generic')", 1)
		}
		if _, err := raw.Exec(sqlText); err != nil {
			t.Fatalf("old migration %s: %v", m.name, err)
		}
		if _, err := raw.Exec(`INSERT INTO schema_migrations VALUES(?,?,?)`, m.version, m.name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.Exec(`INSERT INTO server_profiles(id,name,provider_kind,active,metricz_user,metricz_pass_hash,bridge_token_hash,created_at,updated_at)
		VALUES('a','Old','selfhosted_old',1,'ua','h','ta','x','x'),('b','Panel','managed',0,'ub','h','tb','x','x')`); err != nil {
		t.Fatalf("insert old rows: %v", err)
	}
	raw.Close()

	db, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got := map[string]string{}
	rows, err := db.Query(`SELECT id, provider_kind FROM server_profiles`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, kind string
		rows.Scan(&id, &kind)
		got[id] = kind
	}
	rows.Close()
	if got["a"] != "selfhosted" || got["b"] != "managed" {
		t.Fatalf("after upgrade: %v", got)
	}
	if _, err := db.Exec(`UPDATE server_profiles SET provider_kind='selfhosted_old' WHERE id='a'`); err == nil {
		t.Fatal("the old value is still allowed")
	}
	var fk int
	db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	if fk != 1 {
		t.Fatal("foreign keys are off after the upgrade")
	}
}

// A database from an older release stored the managed-panel kind under a
// host-specific name. Migration 13 must turn it into 'managed' and stop
// accepting the old name.
func TestManagedProviderKindUpgrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dzm.db")
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	ms, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.version >= 13 {
			break
		}
		sqlText := strings.ReplaceAll(m.sql, "'managed'", "'legacy_panel'")
		if _, err := raw.Exec(sqlText); err != nil {
			t.Fatalf("old migration %s: %v", m.name, err)
		}
		if _, err := raw.Exec(`INSERT INTO schema_migrations VALUES(?,?,?)`, m.version, m.name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.Exec(`INSERT INTO server_profiles(id,name,provider_kind,active,metricz_user,metricz_pass_hash,bridge_token_hash,created_at,updated_at)
		VALUES('a','Panel','legacy_panel',1,'ua','h','ta','x','x'),('b','Own','selfhosted',0,'ub','h','tb','x','x'),('c','Other','generic',0,'uc','h','tc','x','x')`); err != nil {
		t.Fatalf("insert old rows: %v", err)
	}
	raw.Close()

	db, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got := map[string]string{}
	rows, err := db.Query(`SELECT id, provider_kind FROM server_profiles`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, kind string
		rows.Scan(&id, &kind)
		got[id] = kind
	}
	rows.Close()
	if got["a"] != "managed" || got["b"] != "selfhosted" || got["c"] != "generic" {
		t.Fatalf("after upgrade: %v", got)
	}
	if _, err := db.Exec(`UPDATE server_profiles SET provider_kind='legacy_panel' WHERE id='a'`); err == nil {
		t.Fatal("the old name is still allowed")
	}
	var fk int
	db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	if fk != 1 {
		t.Fatal("foreign keys are off after the upgrade")
	}
}
