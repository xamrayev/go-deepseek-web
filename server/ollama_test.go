package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaEndpoints(t *testing.T) {
	srv := NewServer(ServerOptions{
		Port: 9655,
	})

	// 1. Test /api/version
	t.Run("GET /api/version", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
		rec := httptest.NewRecorder()
		srv.handleOllamaVersion(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var vResp OllamaVersionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &vResp); err != nil {
			t.Fatalf("failed to decode version json: %v", err)
		}
		if vResp.Version == "" {
			t.Errorf("version string should not be empty")
		}
	})

	// 2. Test /api/tags
	t.Run("GET /api/tags", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
		rec := httptest.NewRecorder()
		srv.handleOllamaTags(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var tagsResp OllamaTagsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &tagsResp); err != nil {
			t.Fatalf("failed to decode tags json: %v", err)
		}
		if len(tagsResp.Models) == 0 {
			t.Fatalf("expected models in /api/tags, got 0")
		}

		foundReasoner := false
		for _, m := range tagsResp.Models {
			if m.Name == "deepseek-reasoner:latest" {
				foundReasoner = true
				if m.Details.Family != "deepseek" {
					t.Errorf("expected family deepseek, got %s", m.Details.Family)
				}
			}
		}
		if !foundReasoner {
			t.Errorf("deepseek-reasoner:latest not found in /api/tags")
		}
	})

	// 3. Test /api/ps
	t.Run("GET /api/ps", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ps", nil)
		rec := httptest.NewRecorder()
		srv.handleOllamaPs(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var psResp OllamaTagsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &psResp); err != nil {
			t.Fatalf("failed to decode ps json: %v", err)
		}
		if len(psResp.Models) == 0 {
			t.Fatalf("expected running models in /api/ps, got 0")
		}
	})

	// 4. Test /api/show
	t.Run("POST /api/show", func(t *testing.T) {
		payload := `{"name": "deepseek-reasoner"}`
		req := httptest.NewRequest(http.MethodPost, "/api/show", bytes.NewBufferString(payload))
		rec := httptest.NewRecorder()
		srv.handleOllamaShow(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var showResp OllamaShowResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &showResp); err != nil {
			t.Fatalf("failed to decode show json: %v", err)
		}
		if showResp.Template == "" {
			t.Errorf("template should not be empty")
		}
		if showResp.Details.ParameterSize != "671B" {
			t.Errorf("expected 671B, got %s", showResp.Details.ParameterSize)
		}
	})
}
