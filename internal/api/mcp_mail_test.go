package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/aura/aura/internal/mcp"
)

func TestMCPProviderProbeMailMCPFindsSearchReadAndFlagsBlockedTools(t *testing.T) {
	srv, state := newFakeMCPServer(t, []mcp.Tool{
		{Name: "imap_search_messages"},
		{Name: "imap_get_message"},
		{Name: "smtp_send_message"},
	})
	router := newInvokeRouter(t, "mail-mcp", state, srv)

	rr := postRaw(t, router, "/mcp/providers/mail-mcp/actions/probe", `{}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}

	var got ConnectorProbeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK {
		t.Fatalf("probe failed: %+v", got)
	}
	if !containsString(got.CapabilitiesReady, "mail.search") || !containsString(got.CapabilitiesReady, "mail.read") {
		t.Fatalf("probe did not find mail search/read: %+v", got)
	}
	if !containsString(got.BlockedToolsAdvertised, "smtp_send_message") {
		t.Fatalf("probe did not flag blocked send tool: %+v", got.BlockedToolsAdvertised)
	}
}

func TestMCPProviderMailSearchMapsCanonicalRequestToIMAPTool(t *testing.T) {
	srv, state := newFakeMCPServer(t, []mcp.Tool{
		{Name: "imap_search_messages"},
		{Name: "imap_get_message"},
	})
	state.resultText = `{"messages":[{"id":"m1","thread_id":"t1","subject":"Fattura maggio","from":"cliente@example.com","to":["davide@example.com"],"date":"2026-05-08T08:30:00Z","snippet":"Pagamento entro venerdi"}]}`
	router := newInvokeRouter(t, "mail-mcp", state, srv)

	rr := postRaw(t, router, "/mcp/providers/mail-mcp/mail/search", `{"account":"work","mailbox":"INBOX","query":"fattura","limit":5}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}

	var got MailSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ProviderID != "mail-mcp" || got.ProviderTool != "imap_search_messages" {
		t.Fatalf("unexpected provider mapping: %+v", got)
	}
	if len(got.Messages) != 1 || got.Messages[0].Subject != "Fattura maggio" {
		t.Fatalf("messages not normalized: %+v", got.Messages)
	}

	state.mu.Lock()
	args := state.lastArgs
	state.mu.Unlock()
	for _, want := range []string{`"name":"imap_search_messages"`, `"account":"work"`, `"mailbox":"INBOX"`, `"query":"fattura"`, `"limit":5`} {
		if !strings.Contains(args, want) {
			t.Fatalf("upstream args missing %s in %s", want, args)
		}
	}
}

func TestMCPProviderMailReadMapsCanonicalRequestAndRedactsSecrets(t *testing.T) {
	srv, state := newFakeMCPServer(t, []mcp.Tool{
		{Name: "imap_search_messages"},
		{Name: "imap_get_message"},
	})
	state.resultText = `{"id":"m1","thread_id":"t1","subject":"Accesso","from":"ops@example.com","body":"token=abc123 password=swordfish"}`
	router := newInvokeRouter(t, "mail-mcp", state, srv)

	rr := postRaw(t, router, "/mcp/providers/mail-mcp/mail/read", `{"account":"work","message_id":"m1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}

	var got MailReadResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ProviderID != "mail-mcp" || got.ProviderTool != "imap_get_message" {
		t.Fatalf("unexpected provider mapping: %+v", got)
	}
	if strings.Contains(got.Message.Body, "abc123") || strings.Contains(got.Message.Body, "swordfish") {
		t.Fatalf("message body was not redacted: %q", got.Message.Body)
	}

	state.mu.Lock()
	args := state.lastArgs
	state.mu.Unlock()
	for _, want := range []string{`"name":"imap_get_message"`, `"account":"work"`, `"message_id":"m1"`} {
		if !strings.Contains(args, want) {
			t.Fatalf("upstream args missing %s in %s", want, args)
		}
	}
}

func TestMCPProviderMailBlocksUnapprovedProviderTools(t *testing.T) {
	srv, state := newFakeMCPServer(t, []mcp.Tool{
		{Name: "smtp_send_message"},
	})
	router := newInvokeRouter(t, "mail-mcp", state, srv)

	rr := postRaw(t, router, "/mcp/providers/mail-mcp/mail/search", `{"query":"send something"}`)
	if rr.Code != http.StatusFailedDependency {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}
