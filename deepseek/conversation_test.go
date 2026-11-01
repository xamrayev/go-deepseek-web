package deepseek

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fakeDeepSeek is a minimal stand-in for chat.deepseek.com: it hands out chat
// sessions, a known-solvable PoW challenge and a fixed streamed reply, and
// records what each completion asked for.
type fakeDeepSeek struct {
	mu          sync.Mutex
	sessions    int
	completions []map[string]any
	// completionStatus, when non-zero, is returned instead of a reply.
	completionStatus int
	nextMessageID    int64
}

func (f *fakeDeepSeek) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v0/chat_session/create", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.sessions++
		id := fmt.Sprintf("sess-%d", f.sessions)
		f.mu.Unlock()
		fmt.Fprintf(w, `{"code":0,"data":{"biz_data":{"id":%q}}}`, id)
	})
	mux.HandleFunc("/api/v0/chat/create_pow_challenge", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"biz_data":{"challenge":{"algorithm":"DeepSeekHashV1","challenge":"e3100e4b312dff68c1558950048401049b6a5af6977854a0e70182e0fe82150f","salt":"f440d547486aa66b435e","signature":"sig","difficulty":144000,"expire_at":1791982051472,"target_path":"/api/v0/chat/completion"}}}}`)
	})
	mux.HandleFunc("/api/v0/chat/completion", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.completions = append(f.completions, body)
		status := f.completionStatus
		f.nextMessageID += 2
		msgID := f.nextMessageID
		f.mu.Unlock()

		if status != 0 {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":"session gone"}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: ready\ndata: {\"response_message_id\": %d}\n\n", msgID)
		fmt.Fprint(w, "data: {\"p\": \"response/fragments\", \"v\": [{\"type\": \"RESPONSE\", \"content\": \"ok\"}]}\n\n")
		fmt.Fprint(w, "data: {\"p\": \"response/status\", \"v\": \"FINISHED\"}\n\n")
	})
	return mux
}

func (f *fakeDeepSeek) last() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.completions[len(f.completions)-1]
}

func drain(t *testing.T, c *Client, prompt string) error {
	t.Helper()
	chunks, errs := c.ChatStream(context.Background(), ChatStreamRequest{Prompt: prompt, Model: ModelChat})
	for range chunks {
	}
	return <-errs
}

func newFakeClient(t *testing.T, f *fakeDeepSeek, keep bool) *Client {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return NewClient(ClientOptions{Token: "test-token", BaseURL: srv.URL, KeepSession: keep})
}

// A conversation saved after one turn and restored into a brand-new client
// must thread the next message after the saved reply, in the same session.
func TestConversationSurvivesANewClient(t *testing.T) {
	f := &fakeDeepSeek{}
	first := newFakeClient(t, f, false)

	if err := drain(t, first, "hello"); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if got := f.last()["parent_message_id"]; got != nil {
		t.Fatalf("first turn parent = %v, want nil", got)
	}
	sessionID, parent := first.Conversation()
	if sessionID != "sess-1" || parent == nil || *parent != 2 {
		t.Fatalf("Conversation() = %q, %v; want sess-1, 2", sessionID, parent)
	}

	// A later process: fresh client, same fake server, restored state.
	second := NewClient(ClientOptions{Token: "test-token", BaseURL: first.baseURL})
	second.SetConversation(sessionID, parent)
	if err := drain(t, second, "and then?"); err != nil {
		t.Fatalf("second turn: %v", err)
	}

	req := f.last()
	if req["chat_session_id"] != "sess-1" {
		t.Errorf("chat_session_id = %v, want sess-1", req["chat_session_id"])
	}
	if req["parent_message_id"] != float64(2) {
		t.Errorf("parent_message_id = %v, want 2", req["parent_message_id"])
	}
	if f.sessions != 1 {
		t.Errorf("sessions created = %d, want 1", f.sessions)
	}
	if _, p := second.Conversation(); p == nil || *p != 4 {
		t.Errorf("parent after second turn = %v, want 4", p)
	}
}

// SetConversation must copy the parent, not alias the caller's variable.
func TestSetConversationCopiesParent(t *testing.T) {
	c := NewClient(ClientOptions{Token: "t"})
	id := int64(7)
	c.SetConversation("s", &id)
	id = 99
	if _, p := c.Conversation(); p == nil || *p != 7 {
		t.Fatalf("parent = %v, want 7", p)
	}
}

// By default a rejected session is replaced and the prompt resent on a new
// one — the existing behaviour callers like nexus-cli rely on.
func TestRejectedSessionIsReplacedByDefault(t *testing.T) {
	f := &fakeDeepSeek{completionStatus: http.StatusNotFound}
	c := newFakeClient(t, f, false)
	c.SetConversation("stale", nil)

	_ = drain(t, c, "hi")
	if f.sessions != 1 {
		t.Fatalf("sessions created = %d, want 1 (the silent replacement)", f.sessions)
	}
}

// With KeepSession the rejection surfaces instead, typed, and no
// replacement session is created behind the caller's back.
func TestKeepSessionReportsRejection(t *testing.T) {
	f := &fakeDeepSeek{completionStatus: http.StatusNotFound}
	c := newFakeClient(t, f, true)
	c.SetConversation("stale", nil)

	err := drain(t, c, "hi")
	if StatusCode(err) != http.StatusNotFound {
		t.Fatalf("StatusCode(%v) = %d, want 404", err, StatusCode(err))
	}
	if f.sessions != 0 {
		t.Fatalf("sessions created = %d, want 0", f.sessions)
	}
	if sid, _ := c.Conversation(); sid != "stale" {
		t.Fatalf("session = %q, want it left as stale", sid)
	}
}
