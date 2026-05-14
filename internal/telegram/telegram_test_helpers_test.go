package telegram

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
)

// telegramAPICall records a single Telegram Bot API request for assertion in tests.
type telegramAPICall struct {
	Method string
	Body   map[string]any
}

// fakeTelegramPDFBytes is a minimal valid-looking PDF header used as the
// synthetic document body returned by the test API server.
var fakeTelegramPDFBytes = []byte("%PDF-1.7\n% aura telegram document test\n")

// newTelegramAPIServer returns an httptest.Server that records every Telegram
// Bot API method call into *calls and returns synthetic OK responses.
// /file/* requests return fakeTelegramPDFBytes as application/pdf.
func newTelegramAPIServer(t *testing.T, calls *[]telegramAPICall) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/file/") {
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write(fakeTelegramPDFBytes)
			return
		}

		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		method := path.Base(r.URL.Path)
		*calls = append(*calls, telegramAPICall{Method: method, Body: body})
		w.Header().Set("Content-Type", "application/json")
		if method == "getFile" {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"doc-1","file_path":"documents/test.pdf","file_size":36}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":123},"date":1760000000,"text":"ok"}}`))
	}))
}

// countTelegramMethods returns how many recorded calls used the given method name.
func countTelegramMethods(calls []telegramAPICall, method string) int {
	var count int
	for _, call := range calls {
		if call.Method == method {
			count++
		}
	}
	return count
}
