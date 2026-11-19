package audit

import (
	"context"
	"strings"
)

type Page struct {
	Items      []Entry `json:"items"`
	NextBefore int64   `json:"next_before"`
}

// Search filters in SQLite before applying the cursor and page size. A
// non-empty actorSub limits the page to that actor's own entries.
func (l *Log) Search(ctx context.Context, limit int, before int64, query, result, actorSub string) (Page, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	query = strings.TrimSpace(query)
	if len(query) > 200 {
		query = query[:200]
	}
	pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query) + "%"
	rows, err := l.db.QueryContext(ctx, `SELECT a.id,a.ts,a.actor_sub,a.actor_name,a.action,
 COALESCE(sp.name,pl.name,a.target),a.params,a.result,a.detail
 FROM audit_log a LEFT JOIN server_profiles sp ON sp.id=a.target LEFT JOIN players pl ON pl.steam64=a.target
 WHERE (?=0 OR a.id < ?) AND (?='' OR a.result=?) AND (?='' OR a.actor_sub=?) AND (?='' OR
 (a.actor_name||' '||replace(replace(a.action,'.',' '),'_',' ')||' '||a.action||' '||a.target||' '||COALESCE(sp.name,pl.name,'')||' '||a.params||' '||a.detail) LIKE ? ESCAPE '\')
 ORDER BY a.id DESC LIMIT ?`, before, before, result, result, actorSub, actorSub, query, pattern, limit+1)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	out := Page{Items: []Entry{}}
	for rows.Next() {
		var e Entry
		var params string
		if err := rows.Scan(&e.ID, &e.TS, &e.ActorSub, &e.ActorName, &e.Action, &e.Target, &params, &e.Result, &e.Detail); err != nil {
			return Page{}, err
		}
		e.Params = []byte(params)
		out.Items = append(out.Items, e)
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.NextBefore = out.Items[limit-1].ID
	}
	return out, rows.Err()
}
