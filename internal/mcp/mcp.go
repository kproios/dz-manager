// Package mcp is a Model Context Protocol server (Streamable HTTP transport,
// stateless, JSON responses) at /mcp, so an MCP client can read the server
// and, when allowed, act on it.
//
// Safety model:
//   - Every request needs a DZ Manager API key. The tools an assistant sees are
//     exactly those the key's scopes allow; a read-only key gets no write tools.
//   - Each tool calls the matching /public/v1 route in-process with that key,
//     so validation, scope checks, confirmation words and the audit log (under
//     the key's name) are the same as for the REST API.
//   - Destructive tools need their confirmation word (KILL, STRIP, DELETE) as
//     an argument, and their descriptions tell the assistant to ask the human
//     first. Tools carry MCP annotations (readOnlyHint, destructiveHint).
//   - Browser requests (any Origin header) are refused, which blocks DNS
//     rebinding and drive-by calls from web pages.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/kproios/dz-manager/internal/apikeys"
	"github.com/kproios/dz-manager/internal/httpx"
)

// Protocol revisions this server speaks, newest first.
var versions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// Server handles /mcp.
type Server struct {
	Keys    *apikeys.Store
	API     http.Handler // the /public/v1 mux
	Version string
	Log     *slog.Logger
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

const instructions = `You are connected to DZ Manager, the admin panel of a DayZ game server.
Read tools show the server, players, bans, the priority queue and the live map. Write tools change the running game for real players: before any write tool, tell the human exactly what you will do and to whom, and wait for their go-ahead. Destructive tools (kill, strip, delete a vehicle) also need their confirmation word as the "confirm" argument; never fill it in without the human's explicit approval. Every write is recorded in the server's audit log under this API key's name.
Positions are game metres: x east, z north, 0 to the world size reported by live_map.`

// ServeHTTP implements the Streamable HTTP transport (POST only, JSON replies).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" {
		httpx.Error(w, http.StatusForbidden, "browser requests are not allowed on /mcp")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		httpx.Error(w, http.StatusMethodNotAllowed, "this MCP server is stateless: POST JSON-RPC messages to /mcp")
		return
	}
	key, err := s.Keys.Authenticate(r.Context(), apikeys.Token(r))
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="dz-manager"`)
		httpx.Error(w, http.StatusUnauthorized, "missing or invalid DZ Manager API key (Authorization: Bearer dzm_…)")
		return
	}
	ctx := apikeys.WithKey(r.Context(), key)
	var raw json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&raw); err != nil {
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{-32700, "parse error"}})
		return
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '[' { // batches (2025-03-26)
		var batch []rpcReq
		if err := json.Unmarshal(raw, &batch); err != nil || len(batch) == 0 || len(batch) > 20 {
			writeJSON(w, rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{-32600, "invalid batch"}})
			return
		}
		var out []rpcResp
		for _, q := range batch {
			if resp, ok := s.handle(ctx, key, q); ok {
				out = append(out, resp)
			}
		}
		if len(out) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, out)
		return
	}
	var q rpcReq
	if err := json.Unmarshal(raw, &q); err != nil {
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{-32600, "invalid request"}})
		return
	}
	resp, ok := s.handle(ctx, key, q)
	if !ok {
		w.WriteHeader(http.StatusAccepted) // notification or response from the client
		return
	}
	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

// handle returns false for notifications (no id), which get no reply.
func (s *Server) handle(ctx context.Context, key *apikeys.Key, q rpcReq) (rpcResp, bool) {
	if len(q.ID) == 0 || string(q.ID) == "null" {
		return rpcResp{}, false
	}
	resp := rpcResp{JSONRPC: "2.0", ID: q.ID}
	if q.JSONRPC != "2.0" {
		resp.Error = &rpcErr{-32600, "jsonrpc must be 2.0"}
		return resp, true
	}
	switch q.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(q.Params, &p)
		v := versions[0]
		for _, x := range versions {
			if x == p.ProtocolVersion {
				v = x
			}
		}
		resp.Result = map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "dz-manager", "title": "DZ Manager", "version": s.Version},
			"instructions":    instructions + "\nThis key (" + key.Name + ") has the scopes: " + strings.Join(key.Scopes, ", ") + ".",
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		list := []map[string]any{}
		for _, t := range Tools {
			if key.Has(t.Scope) {
				list = append(list, t.describe())
			}
		}
		resp.Result = map[string]any{"tools": list}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(q.Params, &p); err != nil {
			resp.Error = &rpcErr{-32602, "invalid params"}
			return resp, true
		}
		t := find(p.Name)
		if t == nil || !key.Has(t.Scope) {
			resp.Error = &rpcErr{-32602, "unknown tool " + p.Name}
			return resp, true
		}
		resp.Result = s.call(ctx, t, p.Arguments)
	case "resources/list":
		resp.Result = map[string]any{"resources": []any{}}
	case "prompts/list":
		resp.Result = map[string]any{"prompts": []any{}}
	default:
		resp.Error = &rpcErr{-32601, "method not found: " + q.Method}
	}
	return resp, true
}

// call runs a tool against the public API in-process and wraps the answer.
func (s *Server) call(ctx context.Context, t *Tool, args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	fail := func(msg string) map[string]any {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": msg}}, "isError": true}
	}
	method, path, query, body, err := t.Build(args)
	if err != nil {
		return fail(err.Error())
	}
	u := "/public/v1" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, u, rd).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.API.ServeHTTP(rec, req)
	text := strings.TrimSpace(rec.Body.String())
	if rec.Code >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(rec.Body.Bytes(), &e) == nil && e.Error != "" {
			text = e.Error
		}
		return fail(fmt.Sprintf("HTTP %d: %s", rec.Code, text))
	}
	out := map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
	var structured any
	if json.Unmarshal(rec.Body.Bytes(), &structured) == nil {
		if obj, ok := structured.(map[string]any); ok {
			out["structuredContent"] = obj
		} else {
			out["structuredContent"] = map[string]any{"items": structured}
		}
	}
	return out
}

// Tool is one MCP tool backed by a /public/v1 route.
type Tool struct {
	Name, Title, Description string
	Scope                    string
	ReadOnly, Destructive    bool
	Props                    map[string]any
	Required                 []string
	Build                    func(args map[string]any) (method, path string, query url.Values, body any, err error)
}

func (t *Tool) describe() map[string]any {
	props := t.Props
	if props == nil {
		props = map[string]any{}
	}
	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(t.Required) > 0 {
		schema["required"] = t.Required
	}
	return map[string]any{
		"name": t.Name, "title": t.Title, "description": t.Description, "inputSchema": schema,
		"annotations": map[string]any{"title": t.Title, "readOnlyHint": t.ReadOnly, "destructiveHint": t.Destructive, "idempotentHint": t.ReadOnly, "openWorldHint": false},
	}
}

func find(name string) *Tool {
	for i := range Tools {
		if Tools[i].Name == name {
			return &Tools[i]
		}
	}
	return nil
}
