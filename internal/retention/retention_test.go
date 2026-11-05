package retention

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kproios/dz-manager/internal/store"
)

func TestArchiveMovesOldRowsIntoGzip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.OpenPath(ctx, filepath.Join(dir, "dzm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	ins := func(id, ts string) {
		if _, err := db.Exec(`INSERT INTO events(server_id,event_id,type,ts,received_at,source,data) VALUES('s',?,'player.connect',?,?,'test','{}')`, id, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	ins("old1", now.AddDate(0, 0, -40).Format(time.RFC3339))
	ins("old2", now.AddDate(0, 0, -20).Format(time.RFC3339))
	ins("new", now.AddDate(0, 0, -1).Format(time.RFC3339))
	s := &Service{DB: db, DataDir: dir, Now: func() time.Time { return now }}
	res, err := s.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Archived["events"] != 2 {
		t.Fatalf("archived %v", res.Archived)
	}
	var left int
	db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&left)
	if left != 1 {
		t.Fatalf("rows left %d", left)
	}
	f, err := os.Open(filepath.Join(dir, "archive", "events-2026-11-05.jsonl.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(zr)
	lines := 0
	for sc.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil || rec["type"] != "player.connect" {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		lines++
	}
	if lines != 2 {
		t.Fatalf("archive lines %d", lines)
	}
	if got := s.list(); len(got) != 1 || got[0].Table != "events" {
		t.Fatalf("list %+v", got)
	}
}
