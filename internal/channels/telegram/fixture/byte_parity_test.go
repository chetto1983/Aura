package fixture

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
	tele "gopkg.in/telebot.v4"
)

// TestSnapshotsByteParity is the Phase 2 / Phase 3 byte-parity gate (US-E03).
//
// For every committed scenario it re-runs CaptureBytes with the exact same
// inputs and asserts that the freshly-marshaled bytes equal the bytes already
// stored in testdata/. Any drift means the Outbound rewire (US-E01 / US-E02)
// changed observable Telegram output — i.e. a regression Phase 2 was designed
// to detect.
//
// Run locally:
//
//	go test -count=1 -run TestSnapshotsByteParity ./internal/channels/telegram/fixture/
//
// CI runs this as part of the regular `go test ./...` invocation.
func TestSnapshotsByteParity(t *testing.T) {
	cases := []struct {
		name        string
		tokens      []llm.Token
		placeholder *tele.Message
		opts        []Option
	}{
		{
			name: "simple_reply",
			tokens: []llm.Token{
				{Content: "**bold** " + strings.Repeat("x", streamingMinThreshold)},
				{Done: true, Usage: llm.TokenUsage{TotalTokens: 7}},
			},
			placeholder: &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}},
		},
		{
			name: "with_cot",
			tokens: []llm.Token{
				{Reasoning: "Considering options carefully so the answer is concise."},
				{Reasoning: " The user wants a short reply."},
				{Content: strings.Repeat("ok ", streamingMinThreshold)},
				{Done: true, Usage: llm.TokenUsage{TotalTokens: 12, ReasoningTokens: 5}},
			},
			placeholder: &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}},
		},
		{
			name: "with_tool_call_and_entity_table",
			tokens: []llm.Token{
				{Content: "| Name  | Value |\n|-------|-------|\n| Alpha | 1     |\n| Beta  | 2     |\n| Gamma | 3     |"},
				{Done: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files"}}, Usage: llm.TokenUsage{TotalTokens: 20}},
			},
			placeholder: &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}},
		},
		{
			name: "fallback_entity_edit_to_plain_text",
			tokens: []llm.Token{
				{Content: "**bold** " + strings.Repeat("x", streamingMinThreshold)},
				{Done: true, Usage: llm.TokenUsage{TotalTokens: 7}},
			},
			placeholder: &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}},
			opts:        []Option{WithEntityEditError(1)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantPath := filepath.Join("testdata", tc.name+".json")
			want, err := os.ReadFile(wantPath)
			if err != nil {
				t.Fatalf("read committed snapshot %s: %v", wantPath, err)
			}
			// Normalise line endings: the file checked into git may be
			// stored with CRLF on a Windows checkout while the marshaler
			// always emits LF. The content bytes are what matter for parity.
			wantNorm := bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))

			_, got := CaptureBytes(t, tc.tokens, tc.placeholder, tc.opts...)

			if !bytes.Equal(wantNorm, got) {
				t.Fatalf("scenario %q: live capture bytes differ from committed snapshot.\n"+
					"  committed: %d bytes\n  live:      %d bytes\n"+
					"  Run `go test -count=1 ./internal/channels/telegram/fixture/` and inspect `git diff testdata/%s.json` for the drift.",
					tc.name, len(wantNorm), len(got), tc.name)
			}
		})
	}
}
