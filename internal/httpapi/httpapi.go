package httpapi

import (
	"encoding/json"
	"net/http"

	"task034-snapdiff/internal/snapdiff"
)

// API exposes the snapshot diff service over HTTP.
type API struct {
	store *snapdiff.Store
}

// New creates an API backed by a fresh in-memory store.
func New() *API {
	return &API{store: snapdiff.NewStore()}
}

// Handler returns the HTTP handler serving all routes.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.handleHealth)
	mux.HandleFunc("POST /snapshots/{id}", a.handlePutSnapshot)
	mux.HandleFunc("PUT /snapshots/{id}", a.handlePutSnapshot)
	mux.HandleFunc("GET /diff", a.handleDiff)
	return mux
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) handlePutSnapshot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errBody("missing id"))
		return
	}
	var req struct {
		Files []snapdiff.FileInput `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid json: "+err.Error()))
		return
	}
	snap, err := snapdiff.NewSnapshot(req.Files)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	a.store.Put(id, snap)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "count": snap.Len()})
}

func (a *API) handleDiff(w http.ResponseWriter, r *http.Request) {
	aID := r.URL.Query().Get("a")
	bID := r.URL.Query().Get("b")
	before, ok := a.store.Get(aID)
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("snapshot not found: "+aID))
		return
	}
	after, ok := a.store.Get(bID)
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("snapshot not found: "+bID))
		return
	}
	writeJSON(w, http.StatusOK, snapdiff.Diff(before, after))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errBody(msg string) map[string]any {
	return map[string]any{"error": msg}
}
