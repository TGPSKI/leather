// Package httpx provides shared HTTP response helpers for leather's API handlers.
package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteJSON encodes v as JSON and writes it with the given HTTP status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes a JSON {"error":"<msg>"} response with the given status code.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}
