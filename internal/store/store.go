// Package store opens the SQLite database and applies embedded, forward-only
// migrations. The dzm serve process is the single writer.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// ErrSchemaNewer means the database was migrated by a newer dzm release.
var ErrSchemaNewer = errors.New("store: database schema is newer than this binary; refusing to start")

// DB wraps *sql.DB.
type DB struct {
	*sql.DB
	Path string
}

// Open opens (creating if needed) dataDir/dzm.db and migrates it.
func Open(ctx context.Context, dataDir string) (*DB, error) {
	path := filepath.Join(dataDir, "dzm.db")
	return OpenPath(ctx, path)
}

// OpenPath opens a database at an explicit path and migrates it.
func OpenPath(ctx context.Context, path string) (*DB, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection serialises writes and avoids SQLITE_BUSY; the workload is tiny.
	sqlDB.SetMaxOpenConns(1)
	db := &DB{DB: sqlDB, Path: path}
	if err := db.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := db.migrate(ctx); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return db, nil
}

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		n := e.Name()
		v, err := strconv.Atoi(strings.SplitN(n, "_", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("store: bad migration name %q", n)
		}
		b, err := migrationFS.ReadFile("migrations/" + n)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{v, n, string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("store: migrations must be numbered 1..n without gaps (got %s)", m.name)
		}
	}
	return out, nil
}

// LatestVersion is the schema version this binary knows.
func LatestVersion() int {
	ms, err := loadMigrations()
	if err != nil {
		return 0
	}
	return len(ms)
}

func (db *DB) migrate(ctx context.Context) error {
	ms, err := loadMigrations()
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	cur, err := db.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if cur > len(ms) {
		return fmt.Errorf("%w (db=%d, binary=%d)", ErrSchemaNewer, cur, len(ms))
	}
	for _, m := range ms[cur:] {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: migration %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, m.version, m.name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion returns the highest applied migration.
func (db *DB) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	return int(v.Int64), err
}

// Now returns the canonical UTC timestamp string used in the database.
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
