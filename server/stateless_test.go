package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionFor_ModeHeaderAndOption(t *testing.T) {
	cases := []struct {
		name      string
		stateless bool
		header    string
		wantKept  bool
	}{
		{"default keeps the agent chat", false, "", true},
		{"header asks for stateless", false, "stateless", false},
		{"header is case-insensitive", false, " Stateless ", false},
		{"option makes every request stateless", true, "", false},
		{"header restores the session over the option", true, "session", true},
		{"unknown header falls back to the option", true, "other", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := NewServer(ServerOptions{Port: 9999, AccountsDir: t.TempDir(), Stateless: c.stateless})
			req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
			if c.header != "" {
				req.Header.Set(SessionModeHeader, c.header)
			}
			first := srv.sessionFor(req, "agent")
			first.ID = "upstream-chat"
			second := srv.sessionFor(req, "agent")

			if kept := second == first && second.ID == "upstream-chat"; kept != c.wantKept {
				t.Fatalf("second request continued the first chat = %v, want %v", kept, c.wantKept)
			}
			if stored := len(srv.sessionMgr.List()); (stored == 1) != c.wantKept {
				t.Fatalf("stored sessions = %d, want stored only when the chat is kept", stored)
			}
		})
	}
}

func TestCORS_AllowsSessionModeHeader(t *testing.T) {
	h := newTestServer(t, "")
	w := do(h, "OPTIONS", "/v1/chat/completions", map[string]string{"Origin": "chrome-extension://x"})
	if got := w.Header().Get("Access-Control-Allow-Headers"); !containsHeader(got, SessionModeHeader) {
		t.Fatalf("Access-Control-Allow-Headers = %q, want it to include %s", got, SessionModeHeader)
	}
}

func containsHeader(list, name string) bool {
	for _, h := range strings.Split(list, ",") {
		if strings.TrimSpace(h) == name {
			return true
		}
	}
	return false
}
