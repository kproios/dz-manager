// Package audit writes the append-only admin action log.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/kproios/dz-manager/internal/store"
)

// Actor identifies who performed an action.
type Actor struct {
	Sub  string `json:"sub"`
	Name string `json:"name"`
}

// System is the actor for automatic actions (schedulers, ingest).
var System = Actor{Sub: "system", Name: "system"}

// Entry is one audit row.
type Entry struct {
	ID        int64           `json:"id"`
	TS        string          `json:"ts"`
	ActorSub  string          `json:"actor_sub"`
	ActorName string          `json:"actor_name"`
	Action    string          `json:"action"`
	Target    string          `json:"target"`
	Params    json.RawMessage `json:"params"`
	Result    string          `json:"result"`
	Detail    string          `json:"detail"`
}

// Log writes audit entries.
type Log struct{ db *store.DB }

// New returns a Log.
func New(db *store.DB) *Log { return &Log{db: db} }

// Write appends an entry. params may be nil. Secrets must never be passed in params.
func (l *Log) Write(ctx context.Context, a Actor, action, target string, params any, result, detail string) error {
	p := []byte("{}")
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		p = b
	}
	_, err := l.db.ExecContext(ctx,
		`INSERT INTO audit_log(ts,actor_sub,actor_name,action,target,params,result,detail) VALUES(?,?,?,?,?,?,?,?)`,
		store.Now(), a.Sub, a.Name, action, target, string(p), result, detail)
	return err
}

// List returns the newest entries first; a non-empty actorSub limits it to that actor.
func (l *Log) List(ctx context.Context, limit int, beforeID int64, actorSub string) ([]Entry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows *sql.Rows
	var err error
	if beforeID > 0 {
		rows, err = l.db.QueryContext(ctx, `SELECT id,ts,actor_sub,actor_name,action,target,params,result,detail FROM audit_log WHERE id < ? AND (?='' OR actor_sub=?) ORDER BY id DESC LIMIT ?`, beforeID, actorSub, actorSub, limit)
	} else {
		rows, err = l.db.QueryContext(ctx, `SELECT id,ts,actor_sub,actor_name,action,target,params,result,detail FROM audit_log WHERE (?='' OR actor_sub=?) ORDER BY id DESC LIMIT ?`, actorSub, actorSub, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var p string
		if err := rows.Scan(&e.ID, &e.TS, &e.ActorSub, &e.ActorName, &e.Action, &e.Target, &p, &e.Result, &e.Detail); err != nil {
			return nil, err
		}
		e.Params = json.RawMessage(p)
		out = append(out, e)
	}
	return out, rows.Err()
}
