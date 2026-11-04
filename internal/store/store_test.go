package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestMigrateAndReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := db.SchemaVersion(ctx)
	if v != LatestVersion() || v == 0 {
		t.Fatalf("version %d, latest %d", v, LatestVersion())
	}
	if _, err := db.Exec(`INSERT INTO settings(key,value) VALUES('k','v')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	var val string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key='k'`).Scan(&val); err != nil || val != "v" {
		t.Fatalf("data lost across reopen: %v %q", err, val)
	}
	var mode string
	db.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
	if mode != "wal" {
		t.Fatalf("journal_mode = %s", mode)
	}
	db.Close()
}

func TestRefuseNewerSchema(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations(version,name,applied_at) VALUES(9999,'future','x')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := OpenPath(ctx, filepath.Join(dir, "dzm.db")); !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("want ErrSchemaNewer, got %v", err)
	}
}
