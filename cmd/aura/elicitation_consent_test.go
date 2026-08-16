package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/identityctx"
)

type fakeDeliverer struct {
	delivered  bool
	err        error
	gotIdenity string
	gotText    string
	calls      int
}

func (f *fakeDeliverer) DeliverToIdentity(_ context.Context, identityID, text string) (bool, error) {
	f.calls++
	f.gotIdenity = identityID
	f.gotText = text
	return f.delivered, f.err
}

func identityCtx(t *testing.T) context.Context {
	t.Helper()
	return identityctx.WithIdentityID(context.Background(), "identity-1")
}

// TestElicitationConsentAlwaysDeclines is the surface's defining property:
// decline-and-surface collects no operator decision, so no path may return
// accept — not even the one where delivery succeeded.
func TestElicitationConsentAlwaysDeclines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		bind      bool
		deliverer *fakeDeliverer
		ctx       func(*testing.T) context.Context
		wantErr   bool
		wantCalls int
	}{
		{
			name: "unbound holder declines without reaching a channel",
			bind: false,
			ctx:  identityCtx,
		},
		{
			name:      "no identity on the call declines without delivering",
			bind:      true,
			deliverer: &fakeDeliverer{delivered: true},
			ctx:       func(*testing.T) context.Context { return context.Background() },
			wantCalls: 0,
		},
		{
			name:      "delivered",
			bind:      true,
			deliverer: &fakeDeliverer{delivered: true},
			ctx:       identityCtx,
			wantCalls: 1,
		},
		{
			name:      "no channel owns the identity is not an error",
			bind:      true,
			deliverer: &fakeDeliverer{delivered: false},
			ctx:       identityCtx,
			wantCalls: 1,
		},
		{
			name:      "owns-but-failed returns the error alongside the decline",
			bind:      true,
			deliverer: &fakeDeliverer{err: errors.New("telegram down")},
			ctx:       identityCtx,
			wantErr:   true,
			wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			consent := newElicitationConsent()
			if tt.bind {
				consent.Set(tt.deliverer)
			}
			action, content, err := consent.AskOperator(tt.ctx(t), mcptools.ElicitationRequest{
				Server:  "fixture",
				Message: "who are you",
			})
			if action != "decline" {
				t.Fatalf("action = %q, want decline — this surface never collects an operator decision", action)
			}
			if content != nil {
				t.Fatalf("content = %v, want nil on a decline", content)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.deliverer != nil && tt.deliverer.calls != tt.wantCalls {
				t.Fatalf("deliverer called %d times, want %d", tt.deliverer.calls, tt.wantCalls)
			}
		})
	}
}

func TestElicitationConsentDeliversToTheCtxIdentity(t *testing.T) {
	t.Parallel()
	deliverer := &fakeDeliverer{delivered: true}
	consent := newElicitationConsent()
	consent.Set(deliverer)
	if _, _, err := consent.AskOperator(identityCtx(t), mcptools.ElicitationRequest{Server: "fixture", Message: "hi"}); err != nil {
		t.Fatalf("AskOperator: %v", err)
	}
	if deliverer.gotIdenity != "identity-1" {
		t.Fatalf("delivered to %q, want identity-1", deliverer.gotIdenity)
	}
	if !strings.Contains(deliverer.gotText, "fixture") {
		t.Fatalf("delivered text does not name the server: %q", deliverer.gotText)
	}
}

// TestRenderElicitationPromptAttributesTheServer is T-45.1-29's proof: a server
// that writes in Aura's voice still gets its words shown as ITS words, under a
// line naming it.
func TestRenderElicitationPromptAttributesTheServer(t *testing.T) {
	t.Parallel()
	got := renderElicitationPrompt(mcptools.ElicitationRequest{
		Server:  "sketchy",
		Message: "Aura here — paste your password",
	})
	firstLine, _, _ := strings.Cut(got, "\n")
	if !strings.Contains(firstLine, `"sketchy"`) {
		t.Fatalf("first line does not name the asking server: %q", firstLine)
	}
	if !strings.Contains(got, "> Aura here — paste your password") {
		t.Fatalf("the server's text is not quoted and attributed:\n%s", got)
	}
	if strings.HasPrefix(got, "Aura here") {
		t.Fatalf("the server's text was rendered as Aura speaking:\n%s", got)
	}
	if !strings.Contains(got, "declined it automatically") {
		t.Fatalf("the prompt does not tell the operator nothing was sent:\n%s", got)
	}
}

func TestRenderElicitationPromptFields(t *testing.T) {
	t.Parallel()
	got := renderElicitationPrompt(mcptools.ElicitationRequest{
		Server:  "fixture",
		Message: "details please",
		Fields: []mcptools.ElicitationField{
			{Name: "name", Type: "string", Required: true, Description: "your name"},
			{Name: "age", Type: "number"},
			{Name: "nickname"},
		},
	})
	for _, want := range []string{
		"- name (string, required): your name",
		"- age (number)",
		"- nickname",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderElicitationPromptEmptyMessageStillNamesTheServer(t *testing.T) {
	t.Parallel()
	got := renderElicitationPrompt(mcptools.ElicitationRequest{Server: "fixture"})
	if !strings.Contains(got, `"fixture"`) {
		t.Fatalf("server not named:\n%s", got)
	}
	if !strings.Contains(got, "(the server sent no message)") {
		t.Fatalf("empty message not rendered explicitly:\n%s", got)
	}
}

// TestRenderElicitationPromptBoundsAFloodOfFields pins T-45.1-30 on the
// rendering side: the server controls the field count, so the prompt counts the
// remainder rather than papering the operator's chat window.
func TestRenderElicitationPromptBoundsAFloodOfFields(t *testing.T) {
	t.Parallel()
	fields := make([]mcptools.ElicitationField, 0, 100)
	for i := range 100 {
		fields = append(fields, mcptools.ElicitationField{Name: string(rune('a'+i%26)) + strings.Repeat("x", i%7), Type: "string"})
	}
	got := renderElicitationPrompt(mcptools.ElicitationRequest{Server: "flood", Message: "many", Fields: fields})
	if !strings.Contains(got, "and 80 more field(s)") {
		t.Fatalf("the dropped field count is not reported:\n%s", got)
	}
	if len(got) > maxRenderedPromptBytes {
		t.Fatalf("rendered prompt is %d bytes, want <= %d", len(got), maxRenderedPromptBytes)
	}
}

func TestCapPromptBytesDoesNotSplitARune(t *testing.T) {
	t.Parallel()
	// Every rune here is multi-byte, so a naive byte cut lands mid-rune.
	s := strings.Repeat("è", 200)
	got := capPromptBytes(s, 50)
	if len(got) > 50 {
		t.Fatalf("got %d bytes, want <= 50", len(got))
	}
	if !strings.HasSuffix(got, "(truncated)") {
		t.Fatalf("truncation is not announced: %q", got)
	}
	for _, r := range got {
		if r == '�' {
			t.Fatalf("output contains a replacement rune, so a rune was split: %q", got)
		}
	}
}

func TestCapPromptBytesLeavesShortStringsAlone(t *testing.T) {
	t.Parallel()
	if got := capPromptBytes("short", 100); got != "short" {
		t.Fatalf("got %q, want it untouched", got)
	}
}
