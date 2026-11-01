package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServerEndpoints(t *testing.T) {
	srv := NewServer(ServerOptions{
		Port: 9888,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/v1/models", srv.handleModels)
	mux.HandleFunc("/v1/model-capabilities", srv.handleModelCapabilities)
	mux.HandleFunc("/v1/sessions", srv.handleSessions)
	mux.HandleFunc("/v1/sessions/reset", srv.handleResetSession)

	// Test 1: /health
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /health status = %d; want 200", w.Code)
	}

	var healthRes map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&healthRes); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if healthRes["status"] != "ok" {
		t.Errorf("health.status = %v; want 'ok'", healthRes["status"])
	}

	// Test 2: /v1/models
	req = httptest.NewRequest("GET", "/v1/models", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /v1/models status = %d; want 200", w.Code)
	}

	var modelsRes struct {
		Object string                   `json:"object"`
		Data   []map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&modelsRes); err != nil {
		t.Fatalf("decode models error: %v", err)
	}
	if len(modelsRes.Data) == 0 {
		t.Errorf("expected models in list, got 0")
	}

	// Test 3: /v1/sessions/reset
	req = httptest.NewRequest("POST", "/v1/sessions/reset?agent=all", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST /v1/sessions/reset status = %d; want 200", w.Code)
	}
}
