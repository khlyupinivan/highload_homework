package store

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

func Register(r interface {
	Handle(method, path, desc string, h http.HandlerFunc)
}, s *Store) {
	r.Handle(http.MethodGet, "/healthz", "liveness/readiness", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Handle(http.MethodGet, "/users", "list users", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.List())
	})
	r.Handle(http.MethodPost, "/users", "create user", func(w http.ResponseWriter, req *http.Request) {
		var it Item
		if err := json.NewDecoder(req.Body).Decode(&it); err != nil || it.ID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad body: id required"})
			return
		}
		if err := s.Create(it); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, it)
	})
	r.Handle(http.MethodGet, "/users/{id}", "get user", func(w http.ResponseWriter, req *http.Request) {
		it, err := s.Get(req.PathValue("id"))
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, it)
	})
	r.Handle(http.MethodPut, "/users/{id}", "replace user", func(w http.ResponseWriter, req *http.Request) {
		var it Item
		if err := json.NewDecoder(req.Body).Decode(&it); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad body"})
			return
		}
		it.ID = req.PathValue("id")
		upd, err := s.Update(it)
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, upd)
	})

	// Чтобы на дашборде были 4xx/5xx
	r.Handle(http.MethodGet, "/debug/boom", "simulated internal error (5xx)", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "injected 5xx"})
	})
	r.Handle(http.MethodGet, "/debug/slow", "simulated slow dependency (latency)", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(40 * time.Millisecond)
		writeJSON(w, http.StatusOK, map[string]string{"status": "slow-ok"})
	})

	r.Handle(http.MethodDelete, "/users/{id}", "delete user", func(w http.ResponseWriter, req *http.Request) {
		if err := s.Delete(req.PathValue("id")); errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		} else if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
