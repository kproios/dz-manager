package profiles

import (
	"errors"
	"net/http"

	"github.com/kproios/dz-manager/internal/audit"
	"github.com/kproios/dz-manager/internal/auth"
	"github.com/kproios/dz-manager/internal/httpx"
)

// API exposes profile CRUD under /api/profiles.
type API struct {
	Store     *Store
	Audit     *audit.Log
	IngestURL string
}

// Register adds routes to an authenticated mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/profiles", a.list)
	mux.HandleFunc("POST /api/profiles", a.create)
	mux.HandleFunc("PUT /api/profiles/{id}", a.update)
	mux.HandleFunc("POST /api/profiles/{id}/activate", a.activate)
	mux.HandleFunc("POST /api/profiles/{id}/archive", a.archive)
	mux.HandleFunc("POST /api/profiles/{id}/rotate-ingest", a.rotate)
	mux.HandleFunc("GET /api/profiles/{id}/wizard", a.wizard)
}

func actor(r *http.Request) audit.Actor {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.Actor()
	}
	return audit.System
}

func (a *API) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, err.Error())
		return
	}
	httpx.Error(w, http.StatusBadRequest, err.Error())
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	ps, err := a.Store.List(r.Context())
	if err != nil {
		httpx.Error(w, 500, err.Error())
		return
	}
	httpx.JSON(w, 200, ps)
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var in Input
	if err := httpx.Decode(r, 16<<10, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	p, err := a.Store.Create(r.Context(), in)
	if err != nil {
		a.fail(w, err)
		return
	}
	_ = a.Audit.Write(r.Context(), actor(r), "profile.create", p.ID, map[string]any{"name": p.Name, "provider": p.ProviderKind}, "ok", "")
	httpx.JSON(w, 201, p)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	var in Input
	if err := httpx.Decode(r, 16<<10, &in); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	id := r.PathValue("id")
	p, err := a.Store.Update(r.Context(), id, in)
	if err != nil {
		a.fail(w, err)
		return
	}
	_ = a.Audit.Write(r.Context(), actor(r), "profile.update", id, map[string]any{"name": p.Name, "rcon_password_changed": in.RconPassword != ""}, "ok", "")
	httpx.JSON(w, 200, p)
}

func (a *API) simple(action string, fn func(*http.Request, string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := fn(r, id); err != nil {
			_ = a.Audit.Write(r.Context(), actor(r), action, id, nil, "error", err.Error())
			a.fail(w, err)
			return
		}
		_ = a.Audit.Write(r.Context(), actor(r), action, id, nil, "ok", "")
		httpx.JSON(w, 200, map[string]bool{"ok": true})
	}
}

func (a *API) activate(w http.ResponseWriter, r *http.Request) {
	a.simple("profile.activate", func(r *http.Request, id string) error { return a.Store.Activate(r.Context(), id) })(w, r)
}

func (a *API) archive(w http.ResponseWriter, r *http.Request) {
	a.simple("profile.archive", func(r *http.Request, id string) error { return a.Store.Archive(r.Context(), id) })(w, r)
}

func (a *API) rotate(w http.ResponseWriter, r *http.Request) {
	a.simple("profile.rotate_ingest", func(r *http.Request, id string) error { return a.Store.RotateIngest(r.Context(), id) })(w, r)
}

func (a *API) wizard(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, sec, err := a.Store.Get(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if sec == nil {
		httpx.Error(w, 500, "stored secrets cannot be decrypted")
		return
	}
	// Revealing ingest credentials is security-relevant: audit it.
	_ = a.Audit.Write(r.Context(), actor(r), "profile.reveal_wizard", id, nil, "ok", "")
	httpx.JSON(w, 200, BuildWizard(p, sec, a.IngestURL))
}
