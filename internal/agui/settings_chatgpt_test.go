package agui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/chatgptplan"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

type chatGPTPlanFixture struct {
	owner, cancelled string
	err              error
}

func (f *chatGPTPlanFixture) Start(ctx context.Context, owner, _ string) (chatgptplan.Flow, error) {
	f.owner = identityctx.IdentityID(ctx)
	return chatgptplan.Flow{AuthURL: "/browser/chatgpt-0123456789abcdef01234567", Status: "authorization_required"}, f.err
}

func (f *chatGPTPlanFixture) Status(ctx context.Context, owner string) (chatgptplan.Status, error) {
	f.owner = identityctx.IdentityID(ctx)
	return chatgptplan.Status{Connected: true, PlanEnabled: true, Email: "owner@example.test", Status: "connected"}, f.err
}

func (f *chatGPTPlanFixture) Cancel(ctx context.Context, owner, authURL string) error {
	f.owner, f.cancelled = identityctx.IdentityID(ctx), authURL
	return f.err
}

func (f *chatGPTPlanFixture) Owns(ctx context.Context, owner, session string) bool {
	return identityctx.IdentityID(ctx) == owner && f.owner == owner && session == "chatgpt-0123456789abcdef01234567"
}

func TestChatGPTCancelRequiresExactBrowserRoute(t *testing.T) {
	fixture := &chatGPTPlanFixture{}
	server := &Server{}
	server.SetChatGPTPlan(fixture)
	server.SetChatGPTBrowserLogin(fixture)
	mux := http.NewServeMux()
	server.registerChatGPTPlanRoutes(mux)
	for _, body := range []string{`{`, `{}`, `{"auth_url":"https://auth.openai.com/"}`, `{"auth_url":"/browser/chatgpt-0123456789abcdef01234567?code=secret"}`, strings.Repeat("x", 2048)} {
		request := withPrincipal(httptest.NewRequest(http.MethodDelete, "/api/settings/chatgpt/login", strings.NewReader(body)), "owner")
		got := httptest.NewRecorder()
		mux.ServeHTTP(got, request)
		if got.Code != http.StatusBadRequest || fixture.cancelled != "" {
			t.Fatal("invalid cancellation reached flow manager")
		}
	}
}

func TestChatGPTLoginViewCannotReachAnotherOwnerOrClosedFlow(t *testing.T) {
	const session = "chatgpt-0123456789abcdef01234567"
	server := &Server{}
	server.SetChatGPTBrowserLogin(&chatGPTPlanFixture{owner: "alice"})
	for _, owner := range []string{"alice", "bob"} {
		request := withPrincipal(httptest.NewRequest(http.MethodGet, "/", nil), owner)
		if allowed := server.browserSessionAllowed(request.Context(), session); allowed != (owner == "alice") {
			t.Fatal("browser crossed owner boundary")
		}
		if server.browserSessionAllowed(request.Context(), session+"x") {
			t.Fatal("unissued session accepted")
		}
	}
	server.SetChatGPTBrowserLogin(nil)
	if server.browserSessionAllowed(withPrincipal(httptest.NewRequest(http.MethodGet, "/", nil), "alice").Context(), session) {
		t.Fatal("closed login browser accepted")
	}
}

func (f *chatGPTPlanFixture) AccessToken(context.Context) (string, error) {
	return "fixture-secret", f.err
}

func (f *chatGPTPlanFixture) Disconnect(ctx context.Context, owner string) error {
	f.owner = identityctx.IdentityID(ctx)
	return f.err
}

func TestChatGPTSettingsRoutesScopeOwnersAndSanitizeFailures(t *testing.T) {
	for _, route := range ChatGPTPlanRoutes() {
		for _, mode := range []string{"ready", "unwired", "failure"} {
			t.Run(route+"/"+mode, func(t *testing.T) {
				fixture := &chatGPTPlanFixture{}
				server := &Server{}
				if mode != "unwired" {
					server.SetChatGPTPlan(fixture)
					server.SetChatGPTBrowserLogin(fixture)
				}
				if mode == "failure" {
					fixture.err = errors.New("secret-access-token")
				}
				mux := http.NewServeMux()
				server.registerChatGPTPlanRoutes(mux)
				parts := strings.SplitN(route, " ", 2)
				request := httptest.NewRequest(parts[0], parts[1], strings.NewReader(`{"auth_url":"/browser/chatgpt-0123456789abcdef01234567"}`))
				request = withPrincipal(request, "00000000-0000-0000-0000-000000000007")
				recorder := httptest.NewRecorder()
				mux.ServeHTTP(recorder, request)
				if recorder.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("connection response may cache account credentials")
				}
				if mode == "ready" && recorder.Code != http.StatusOK {
					t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
				}
				if mode != "ready" && recorder.Code < 400 {
					t.Fatal("failed connection reported success")
				}
				if strings.Contains(recorder.Body.String(), "secret-") {
					t.Fatal("credentials leaked into response")
				}
				if mode == "ready" && fixture.owner != principalFrom(request.Context()) {
					t.Fatal("wrong owner scope")
				}
			})
		}
	}
}

func TestChatGPTCatalogRequiresFixedEndpointAndPreservesAccountOrder(t *testing.T) {
	server, calls := catalogServerWith(t, []llm.ModelCatalogEntry{
		{ID: "model-z", DisplayName: "Model Z"}, {ID: "model-a", DisplayName: "Model A"},
	}, nil, nil)
	query := url.Values{"provider": {llm.ChatGPTProvider}, "base_url": {llm.ChatGPTBaseURL}}
	if got := getModels(t, server, query.Encode()); got.Code != 503 {
		t.Fatal("unconnected catalog was exposed")
	}
	server.SetChatGPTPlan(&chatGPTPlanFixture{})
	query.Set("base_url", "https://attacker.example/v1")
	if got := getModels(t, server, query.Encode()); got.Code != 400 || len(*calls) != 0 {
		t.Fatal("ChatGPT credentials could be sent to another endpoint")
	}
	query.Set("base_url", llm.ChatGPTBaseURL)
	got := getModels(t, server, query.Encode())
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"display_name":"Model Z"`) || strings.Index(got.Body.String(), "model-z") > strings.Index(got.Body.String(), "model-a") {
		t.Fatalf("catalog: %s", got.Body.String())
	}
	if len(*calls) != 1 || (*calls)[0].apiKey != "" {
		t.Fatal("account catalog borrowed an OpenRouter credential")
	}
}
