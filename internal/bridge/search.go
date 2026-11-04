package bridge

import (
	"encoding/json"
	"github.com/kproios/dz-manager/internal/httpx"
	"net/http"
	"strings"
)

func (b *Bridge) search(w http.ResponseWriter, r *http.Request) {
	out := struct {
		Items      []map[string]any `json:"items"`
		NextBefore int64            `json:"next_before"`
	}{Items: []map[string]any{}}
	p, _, err := b.Profiles.Active(r.Context())
	if err != nil {
		httpx.JSON(w, 200, out)
		return
	}
	limit := httpx.QueryInt(r, "limit", 25)
	if limit < 1 || limit > 100 {
		limit = 25
	}
	before := httpx.QueryInt(r, "before", 0)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 200 {
		query = query[:200]
	}
	pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query) + "%"
	typ := r.URL.Query().Get("type")
	heartbeat := r.URL.Query().Get("heartbeat") == "true"
	rows, err := b.DB.QueryContext(r.Context(), `SELECT e.id,e.event_id,e.type,e.ts,e.actor,COALESCE(p.name,e.actor),e.data,e.known FROM events e
 LEFT JOIN players p ON p.steam64=e.actor WHERE e.server_id=? AND (?=0 OR e.id<?)
 AND (? OR e.type NOT IN ('heartbeat','event.seen')) AND (?='' OR e.type=?)
 AND (?='' OR (e.actor||' '||COALESCE(p.name,'')||' '||e.type||' '||replace(replace(e.type,'.',' '),'_',' ')||' '||e.data) LIKE ? ESCAPE '\')
 ORDER BY e.id DESC LIMIT ?`, p.ID, before, before, heartbeat, typ, typ, query, pattern, limit+1)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	defer rows.Close()
	var last int64
	for rows.Next() {
		var id int64
		var eventID, kind, ts, actor, name, data string
		var known bool
		if err := rows.Scan(&id, &eventID, &kind, &ts, &actor, &name, &data, &known); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		if len(out.Items) == limit {
			out.NextBefore = last
			break
		}
		out.Items = append(out.Items, map[string]any{"id": id, "event_id": eventID, "type": kind, "ts": ts, "actor": actor, "actor_name": name, "data": json.RawMessage(data), "known": known})
		last = id
	}
	if err := rows.Err(); err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	httpx.JSON(w, 200, out)
}
