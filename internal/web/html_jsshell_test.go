//go:build !web_integration

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
)

func extract(t *testing.T, body string) (string, string, error) {
	t.Helper()
	_, md, _, warning, err := ExtractMarkdown([]byte(body), mustParse(t, "https://shop.test/p"))
	return md, warning, err
}

// The SiePortal product page of 2026-10-07, captured from the lab VM with the
// navigation headers (its inline CSS trimmed): 31,526 B whose body is an Angular
// <app-root> spinner and a <noscript> plea. Without JavaScript nothing ever renders.
func TestExtractMarkdown_AJavaScriptShellSaysSo(t *testing.T) {
	shell, err := os.ReadFile("testdata/js_shell_sieportal.html")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = extract(t, string(shell))
	assertWebErr(t, err, CodeJavaScriptRequired, "")
}

func TestExtractMarkdown_AThinPageWithoutScriptStaysLowContent(t *testing.T) {
	md, warning, err := extract(t, "<html><body><p>Closed on Sundays.</p></body></html>")
	if err != nil || warning != WarningLowContent || !strings.Contains(md, "Closed on Sundays.") {
		t.Errorf("md = %q, warning = %q, err = %v, want the text with low_content", md, warning, err)
	}
}

// Nearly every real page carries a script; a short page that still SAYS something
// is a short page, not a shell, and its text must reach the model.
func TestExtractMarkdown_AThinPageWithScriptsAndTextStaysLowContent(t *testing.T) {
	body := `<html><head><script src="/analytics.js"></script></head><body>
<h1>Opening hours</h1><p>The showroom in Via Roma 12 opens Monday to Friday, 9 to 18.</p>
<script>window.dataLayer = [];</script></body></html>`
	md, warning, err := extract(t, body)
	if err != nil || warning != WarningLowContent || !strings.Contains(md, "Via Roma 12") {
		t.Errorf("md = %q, warning = %q, err = %v, want the text with low_content", md, warning, err)
	}
}

func TestFetch_AJavaScriptShellIsAnErrorTheModelCanActOn(t *testing.T) {
	shell, err := os.ReadFile("testdata/js_shell_sieportal.html")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(shell)
	}))
	defer srv.Close()
	_, port := hostPort(t, srv.URL)
	c := fetchClient(t, map[string][]netip.Addr{"portal.test": {publicIP}})
	_, err = c.Fetch(context.Background(), "c", "http://portal.test:"+port+"/detail")
	assertWebErr(t, err, CodeJavaScriptRequired, "")
	if we, _ := AsWebError(err); !strings.Contains(we.Message, "browser") {
		t.Errorf("message = %q, want it to point the model at a browser", we.Message)
	}
}
