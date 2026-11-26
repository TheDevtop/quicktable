package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dgraph-io/badger/v4"

	"github.com/TheDevtop/quicktable/internal/mustang"
)

// writeJSON writes a JSON response with status code and Content-Type header.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, status(err), map[string]string{"error": err.Error()})
}

// status converts engine and request errors to HTTP status codes.
func status(err error) int {
	switch {
	case errors.Is(err, mustang.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, errRequest), errors.Is(err, mustang.ErrName), errors.Is(err, mustang.ErrType):
		return http.StatusBadRequest
	case errors.Is(err, mustang.ErrCondition), errors.Is(err, badger.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, badger.ErrTxnTooBig):
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusInternalServerError
}

// requireToken lets a request through only if it carries "Authorization: Bearer <token>".
// GET /health stays open for liveness probes. The response never echoes what the client sent.
func requireToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		// subtle hides where a wrong token differs; only its length can leak.
		scheme, got, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		if !strings.EqualFold(scheme, "Bearer") || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="quicktable"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or invalid bearer token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RegisterRESTRoutes serves every endpoint as POST /api/<name>, plus GET /health.
func RegisterRESTRoutes(mux *http.ServeMux, s *mustang.Store) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	for _, e := range endpoints {
		mux.HandleFunc("POST /api/"+e.name, func(w http.ResponseWriter, r *http.Request) {
			v, err := e.call(s, r.Body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, v)
		})
	}
}
