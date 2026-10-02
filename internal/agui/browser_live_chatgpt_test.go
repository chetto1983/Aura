package agui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatGPTBrowserMetadataOmitsOAuthQueriesAndFragments(t *testing.T) {
	lines := make(chan []byte, 4)
	sink := browserLineSink{ctx: context.Background(), out: lines, privateLogin: true}
	input := `{"type":"url","url":"https://auth.openai.com/api/accounts/authorize?id_token_hint=private-jwt&state=private-state"}` + "\n" +
		`{"type":"tabs","tabs":[{"url":"http://127.0.0.1:1455/auth/callback?code=private-code&state=private-state#private-fragment"}]}` + "\n" +
		`{"type":"frame","data":"base64frame","metadata":{"deviceWidth":1280,"deviceHeight":720}}` + "\n"
	if _, err := sink.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		output := string(<-lines)
		if strings.Contains(output, "private-") || strings.Contains(output, "id_token_hint") || strings.Contains(output, "?code=") {
			t.Fatal("OAuth secrets reached browser metadata")
		}
	}
	for _, raw := range []string{`{"url":"https://aura.local/#token"}`, `{"url":"https://user:password@auth.openai.com/path?token=private-token"}`, "{"} {
		if strings.Contains(string(browserLoginMetadata([]byte(raw))), "token") || strings.Contains(string(browserLoginMetadata([]byte(raw))), "password") {
			t.Fatal("credential metadata retained")
		}
	}
}

func TestChatGPTBrowserStreamDoesNotCacheUnauthorizedMetadata(t *testing.T) {
	server := &Server{}
	mux := http.NewServeMux()
	server.registerBrowserLiveRoutes(mux)
	request := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/browser/sessions/chatgpt-0123456789abcdef01234567/stream", nil), "owner")
	got := httptest.NewRecorder()
	mux.ServeHTTP(got, request)
	if got.Code != http.StatusNotFound || got.Header().Get("Cache-Control") != "no-store" || got.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("login metadata boundary lost")
	}
}
