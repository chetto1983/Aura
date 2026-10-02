package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

type loginBrowserHandle struct {
	done chan struct{}
	stop func()
	once sync.Once
}

func (h *loginBrowserHandle) Kill()              { h.once.Do(h.stop) }
func (h *loginBrowserHandle) Wait() (int, error) { <-h.done; return 0, nil }

func browserProtocolFixture(t *testing.T, query string, cleanupError bool) *chatGPTBrowserSession {
	t.Helper()
	in, commands := io.Pipe()
	out, output := io.Pipe()
	handle := &loginBrowserHandle{done: make(chan struct{}), stop: func() { _ = in.Close(); _ = output.Close() }}
	s := newChatGPTBrowserSession(handle, commands, out, output)
	go func() {
		defer close(handle.done)
		defer in.Close()
		_, _ = io.WriteString(output, "untrusted stderr\n"+`{"type":"listening","redirect_uri":"http://127.0.0.1:43210/auth/callback"}`+"\n")
		scanner := bufio.NewScanner(in)
		if scanner.Scan() {
			var command struct{ Type, URL string }
			if json.Unmarshal(scanner.Bytes(), &command) != nil || command.Type != "navigate" || command.URL != "https://auth.openai.com/api/accounts/authorize?state=private-state" {
				_, _ = io.WriteString(output, `{"type":"error"}`+"\n")
			} else {
				_, _ = io.WriteString(output, `{"type":"navigated"}`+"\n")
				data, _ := json.Marshal(chatGPTBrowserEvent{Type: "callback", Query: query})
				_, _ = output.Write(append(data, '\n'))
			}
		}
		for scanner.Scan() {
		}
		if cleanupError {
			_, _ = io.WriteString(output, `{"type":"cleanup_error"}`+"\n")
		}
	}()
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestChatGPTBrowserProtocolAndGracefulCleanup(t *testing.T) {
	s := browserProtocolFixture(t, "state=private-state&code=private-code&client_id=oaiapp_owner", false)
	ready, err := s.next(context.Background(), "listening")
	if err != nil {
		t.Fatal(err)
	}
	s.redirectURI = ready.RedirectURI
	if s.RedirectURI() != "http://127.0.0.1:43210/auth/callback" {
		t.Fatal("lost callback URI")
	}
	if err = s.Navigate(context.Background(), "https://auth.openai.com/api/accounts/authorize?state=private-state"); err != nil {
		t.Fatal(err)
	}
	query, err := s.Callback(context.Background())
	if err != nil || query.Get("code") != "private-code" || query.Get("state") != "private-state" {
		t.Fatal("lost callback")
	}
	for range 2 {
		if s.Close() != nil {
			t.Fatal("graceful cleanup failed")
		}
	}
	if _, err = s.Callback(context.Background()); err == nil {
		t.Fatal("closed browser supplied a callback")
	}
}

func TestChatGPTBrowserRejectsMalformedCallbackAndReportsCleanup(t *testing.T) {
	s := browserProtocolFixture(t, "state=private-state&code=%xx", true)
	if _, err := s.next(context.Background(), "listening"); err != nil {
		t.Fatal(err)
	}
	if err := s.Navigate(context.Background(), "https://auth.openai.com/api/accounts/authorize?state=private-state"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Callback(context.Background()); err == nil {
		t.Fatal("invalid percent encoding accepted")
	}
	if s.Close() == nil {
		t.Fatal("cleanup failure was hidden")
	}
}

func TestChatGPTBrowserReadAndWriteRespectCancellation(t *testing.T) {
	in, commands := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &chatGPTBrowserSession{commands: commands, events: make(chan chatGPTBrowserEvent), callbacks: make(chan string)}
	if s.Navigate(ctx, "https://auth.openai.com/api/accounts/authorize") == nil {
		t.Fatal("cancelled navigation succeeded")
	}
	if _, err := s.next(ctx, "listening"); err == nil {
		t.Fatal("cancelled listener read succeeded")
	}
	if _, err := s.Callback(ctx); err == nil {
		t.Fatal("cancelled callback succeeded")
	}
	_ = in.Close()
	_ = commands.Close()
	for _, session := range []string{"../escape", "chatgpt-0123456789abcdef01234567"} {
		if _, err := (sandboxChatGPTBrowser{}).Open(context.Background(), session); err == nil {
			t.Fatal("missing sandbox accepted")
		}
	}
}

func TestChatGPTLoginBrowserRoutesRequireAuthentication(t *testing.T) {
	const session = "chatgpt-0123456789abcdef01234567"
	for _, authenticated := range []bool{false, true} {
		handler, err := newServeHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if identityctx.IdentityID(r.Context()) != restartTestIdentity {
				t.Fatal("login browser lost owner")
			}
			w.WriteHeader(http.StatusNoContent)
		}), authulaTestDeps(restartTestIdentity, uncapableIdentities{id: restartTestIdentity}), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, route := range []struct{ method, suffix string }{{http.MethodGet, "stream"}, {http.MethodPost, "input"}} {
			request := httptest.NewRequest(route.method, "/api/browser/sessions/"+session+"/"+route.suffix, strings.NewReader(`{}`))
			if authenticated {
				addAuthulaSession(request)
			}
			got := httptest.NewRecorder()
			handler.ServeHTTP(got, request)
			want := http.StatusUnauthorized
			if authenticated {
				want = http.StatusNoContent
			}
			if got.Code != want {
				t.Fatalf("status %d want %d", got.Code, want)
			}
		}
	}
}
