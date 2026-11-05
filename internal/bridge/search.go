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
	heartbeat := r.URL.Query().Get("heartbeat") == "true"
	order, cursor := "DESC", "e.id<?"
	if r.URL.Query().Get("order") == "asc" {
		order, cursor = "ASC", "e.id>?"
	}
	// type (single, legacy) or types (comma list) filter.
	types := []string{}
	for _, t := range strings.Split(r.URL.Query().Get("type")+","+r.URL.Query().Get("types"), ",") {
		if t = strings.TrimSpace(t); reType.MatchString(t) && len(types) < 20 {
			types = append(types, t)
		}
	}
	typeSQL := "1"
	args := []any{p.ID, before, before, heartbeat, query, pattern}
	if len(types) > 0 {
		typeSQL = "e.type IN (?" + strings.Repeat(",?", len(types)-1) + ")"
		for _, t := range types {
			args = append(args, t)
		}
	}
	args = append(args, limit+1)
	// event.start/event.end only carry a key: join the marker so the log can
	// say which event it was and where.
	rows, err := b.DB.QueryContext(r.Context(), `SELECT e.id,e.event_id,e.type,e.ts,e.actor,COALESCE(p.name,e.actor),e.data,e.known,
 COALESCE(m.category,''),COALESCE(m.class,''),COALESCE(json_extract(m.data,'$.name'),''),COALESCE(m.x,0),COALESCE(m.z,0) FROM events e
 LEFT JOIN players p ON p.steam64=e.actor
 LEFT JOIN map_events m ON e.type IN ('event.start','event.end') AND m.server_id=e.server_id AND m.key=json_extract(e.data,'$.key')
 WHERE e.server_id=? AND (?=0 OR `+cursor+`)
 AND (? OR e.type NOT IN ('heartbeat','event.seen'))
 AND (?='' OR (e.actor||' '||COALESCE(p.name,'')||' '||e.type||' '||replace(replace(e.type,'.',' '),'_',' ')||' '||e.data) LIKE ? ESCAPE '\')
 AND `+typeSQL+`
 ORDER BY e.id `+order+` LIMIT ?`, args...)
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	defer rows.Close()
	var last int64
	for rows.Next() {
		var id int64
		var eventID, kind, ts, actor, name, data, mCat, mClass, mName string
		var mx, mz float64
		var known bool
		if err := rows.Scan(&id, &eventID, &kind, &ts, &actor, &name, &data, &known, &mCat, &mClass, &mName, &mx, &mz); err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		if len(out.Items) == limit {
			out.NextBefore = last
			break
		}
		item := map[string]any{"id": id, "event_id": eventID, "type": kind, "ts": ts, "actor": actor, "actor_name": name, "data": json.RawMessage(data), "known": known}
		if mCat != "" {
			item["subject"] = map[string]any{"category": mCat, "class": mClass, "name": mName, "x": mx, "z": mz}
		}
		out.Items = append(out.Items, item)
		last = id
	}
	if err := rows.Err(); err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	httpx.JSON(w, 200, out)
}
