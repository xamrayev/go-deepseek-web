package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xamrayev/go-deepseek-web/deepseek"
)

// ServerOptions contains configuration for the local API proxy server.
type ServerOptions struct {
	Host          string
	Port          int
	ProxyAPIKey   string
	MaxConcurrent int
	AccountsDir   string
	Client        *deepseek.Client
	Pool          *deepseek.AccountPool

	// ServiceName labels this proxy in the /health payload and in the default
	// startup log lines. Defaults to DefaultServiceName.
	ServiceName string

	// Stateless runs every request in a fresh upstream chat instead of
	// continuing one chat per agent. Clients can override it per request with
	// the x-session-mode header.
	Stateless bool

	// OnListen, when set, is called once the listener is bound instead of the
	// default English log lines, so an embedding application prints its own.
	OnListen func(info ListenInfo)
}

// DefaultServiceName labels a proxy whose ServerOptions left ServiceName empty.
const DefaultServiceName = "go-deepseek-web"

// ListenInfo describes a bound proxy, passed to ServerOptions.OnListen.
type ListenInfo struct {
	// Addr is the resolved host:port the listener is bound to.
	Addr string
	// Port is the resolved port, repeated for callers building URLs.
	Port int
	// ReadyAccounts counts the pooled accounts usable right now; 0 means the
	// proxy runs on the single default credential instead of a pool.
	ReadyAccounts int
}

// Server provides the local OpenAI & Anthropic compatible API proxy.
type Server struct {
	host          string
	port          int
	proxyAPIKey   string
	maxConcurrent int
	client        *deepseek.Client
	sessionMgr    *SessionManager
	stateless     bool
	pool          *deepseek.AccountPool
	serviceName   string
	onListen      func(info ListenInfo)
	sem           chan struct{}
	httpServer    *http.Server
	mu            sync.Mutex
}

// NewServer creates a new API proxy server.
func NewServer(opts ServerOptions) *Server {
	if opts.Port <= 0 {
		opts.Port = 9655
	}
	if opts.Host == "" {
		opts.Host = "0.0.0.0"
	}
	if opts.ServiceName == "" {
		opts.ServiceName = DefaultServiceName
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 10
	}
	if opts.Client == nil {
		opts.Client = deepseek.NewClient(deepseek.ClientOptions{})
	}

	pool := opts.Pool
	if pool == nil {
		pool = deepseek.NewAccountPool(opts.AccountsDir)
	}
	if pool != nil && pool.Count() > 0 {
		opts.Client.SetPool(pool)
	}

	return &Server{
		host:          opts.Host,
		port:          opts.Port,
		proxyAPIKey:   opts.ProxyAPIKey,
		maxConcurrent: opts.MaxConcurrent,
		client:        opts.Client,
		sessionMgr:    NewSessionManager(30 * time.Minute),
		stateless:     opts.Stateless,
		pool:          pool,
		serviceName:   opts.ServiceName,
		onListen:      opts.OnListen,
		sem:           make(chan struct{}, opts.MaxConcurrent),
	}
}

// Handler builds the full HTTP handler (routes + CORS middleware).
// It is used by Start and lets tests exercise the real routing table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/readyz", s.handleHealth)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/model-capabilities", s.handleModelCapabilities)
	mux.HandleFunc("/v1/sessions", s.handleSessions)
	mux.HandleFunc("/v1/sessions/reset", s.handleResetSession)
	mux.HandleFunc("/reset-session", s.handleResetSession)
	mux.HandleFunc("/v1/chat/completions", s.handleDispatch)
	mux.HandleFunc("/v1/messages", s.handleDispatch)
	mux.HandleFunc("/v1/responses", s.handleDispatch)

	// Ollama API Emulation
	mux.HandleFunc("/api/tags", s.handleOllamaTags)
	mux.HandleFunc("/api/version", s.handleOllamaVersion)
	mux.HandleFunc("/api/ps", s.handleOllamaPs)
	mux.HandleFunc("/api/show", s.handleOllamaShow)
	mux.HandleFunc("/api/chat", s.handleOllamaChat)
	mux.HandleFunc("/api/generate", s.handleOllamaGenerate)

	return s.corsMiddleware(mux)
}

// Start runs the HTTP server.
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	handler := s.Handler()

	s.mu.Lock()
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: handler,
	}
	s.mu.Unlock()

	accounts := 0
	if s.pool != nil {
		accounts = s.pool.Count()
	}
	if s.onListen != nil {
		s.onListen(ListenInfo{Addr: addr, Port: s.port, ReadyAccounts: accounts})
	} else {
		log.Printf("[%s] listening on http://%s", s.serviceName, addr)
		if accounts > 0 {
			log.Printf("[%s] account pool: %d loaded (round-robin, 429 failover)", s.serviceName, accounts)
		}
		log.Printf("[%s] OpenAI chat:        POST http://localhost:%d/v1/chat/completions", s.serviceName, s.port)
		log.Printf("[%s] Anthropic messages: POST http://localhost:%d/v1/messages", s.serviceName, s.port)
		log.Printf("[%s] models:             GET  http://localhost:%d/v1/models", s.serviceName, s.port)
	}

	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.httpServer
	s.mu.Unlock()
	if srv != nil {
		return srv.Shutdown(ctx)
	}
	return nil
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, x-agent-session, x-session-mode, x-api-key, anthropic-version")
		w.Header().Set("Access-Control-Expose-Headers", "*")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) isAuthorized(r *http.Request) bool {
	if s.proxyAPIKey == "" {
		return true
	}

	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		token := strings.TrimSpace(auth[7:])
		if token == s.proxyAPIKey {
			return true
		}
	}

	if xKey := r.Header.Get("x-api-key"); xKey == s.proxyAPIKey {
		return true
	}

	return false
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	accountsCount := 1
	if s.pool != nil {
		accountsCount = s.pool.Count()
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "ok",
		"service":        s.serviceName,
		"accounts_count": accountsCount,
		"time":           time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.handleHealth(w, r)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, `{"error":{"message":"Unauthorized","type":"authentication_error"}}`, http.StatusUnauthorized)
		return
	}

	var data []map[string]interface{}
	for id := range deepseek.ModelConfigs {
		data = append(data, map[string]interface{}{
			"id":       id,
			"object":   "model",
			"created":  1700000000,
			"owned_by": "deepseek",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   data,
	})
}

func (s *Server) handleModelCapabilities(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, `{"error":{"message":"Unauthorized","type":"authentication_error"}}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(deepseek.ModelConfigs)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, `{"error":{"message":"Unauthorized","type":"authentication_error"}}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.sessionMgr.List())
}

func (s *Server) handleResetSession(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		http.Error(w, `{"error":{"message":"Unauthorized","type":"authentication_error"}}`, http.StatusUnauthorized)
		return
	}

	agentID := r.URL.Query().Get("agent")
	if agentID == "" {
		agentID = r.Header.Get("x-agent-session")
	}

	count := s.sessionMgr.Reset(agentID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"cleared": count,
		"agent":   agentID,
	})
}

func (s *Server) handleDispatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.isAuthorized(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"message": "Invalid proxy API key", "type": "authentication_error"},
		})
		return
	}

	// Concurrency limiting / backpressure
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"message": "Server busy. Retry shortly.", "type": "overloaded"},
		})
		return
	}

	// Read body (limit to 10MB)
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Determine agentID for session isolation
	agentID := r.Header.Get("x-agent-session")
	if agentID == "" {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if host == "127.0.0.1" || host == "::1" {
			agentID = "dev-agent"
		} else {
			agentID = host
		}
	}

	path := r.URL.Path
	switch path {
	case "/v1/chat/completions":
		var req OpenAIRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, `{"error":{"message":"Invalid JSON payload","type":"invalid_request_error"}}`, http.StatusBadRequest)
			return
		}
		if req.User != "" && agentID == "dev-agent" {
			agentID = req.User
		}
		s.handleOpenAIChatCompletions(w, r, req, agentID)

	case "/v1/messages":
		s.handleAnthropicMessages(w, r, body, agentID)

	case "/v1/responses":
		// OpenAI Responses API shim: map into OpenAI completions format
		var req OpenAIRequest
		_ = json.Unmarshal(body, &req)
		s.handleOpenAIChatCompletions(w, r, req, agentID)

	default:
		http.NotFound(w, r)
	}
}
