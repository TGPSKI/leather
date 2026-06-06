package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	t.Run("status and content-type", func(t *testing.T) {
		rr := httptest.NewRecorder()
		WriteJSON(rr, http.StatusCreated, map[string]string{"k": "v"})
		if rr.Code != http.StatusCreated {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusCreated)
		}
		if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
	})

	t.Run("body round-trips", func(t *testing.T) {
		type payload struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		}
		rr := httptest.NewRecorder()
		WriteJSON(rr, http.StatusOK, payload{Name: "leather", Count: 42})
		var got payload
		if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Name != "leather" || got.Count != 42 {
			t.Errorf("got %+v, want {leather 42}", got)
		}
	})
}

func TestWriteError(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteError(rr, http.StatusNotFound, "not found")
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
	var got map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["error"] != "not found" {
		t.Errorf("error = %q, want %q", got["error"], "not found")
	}
}
