package conversations

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestMaybeSpillReportsTheFailingSidecarLeg(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("x", 32)

	t.Run("traversal-shaped conversation id never reaches the filesystem", func(t *testing.T) {
		t.Parallel()
		s := newFakeStore(&Store{runDir: t.TempDir(), turnCapBytes: 8})
		if _, _, err := s.maybeSpill("../escape", 1, body); err == nil {
			t.Fatal("maybeSpill accepted a traversal-shaped conversation id")
		}
	})

	t.Run("unwritable sidecar directory names the path", func(t *testing.T) {
		t.Parallel()
		runDir := t.TempDir()
		// The per-conversation directory cannot be created because a regular file
		// already occupies the path MkdirAll needs.
		blocker := filepath.Join(runDir, "conversations")
		if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("seed blocker: %v", err)
		}
		s := newFakeStore(&Store{runDir: runDir, turnCapBytes: 8})
		_, _, err := s.maybeSpill("c1", 3, body)
		if err == nil {
			t.Fatal("maybeSpill succeeded with an unwritable sidecar directory")
		}
		if !strings.Contains(err.Error(), "write turn sidecar") {
			t.Fatalf("error does not name the failing leg: %v", err)
		}
	})
}

func TestOptionalInt8MapsZeroToNull(t *testing.T) {
	t.Parallel()
	if got := optionalInt8(0); got.Valid {
		t.Fatalf("optionalInt8(0) = %+v, want NULL", got)
	}
	got := optionalInt8(42)
	if !got.Valid || got.Int64 != 42 {
		t.Fatalf("optionalInt8(42) = %+v, want 42", got)
	}
}

func TestRepairToolMessagePairsWithEmptyInput(t *testing.T) {
	t.Parallel()
	if got := repairToolMessagePairsWith(nil, false); got != nil {
		t.Fatalf("repair(nil) = %+v, want nil", got)
	}
}

func TestRepairDropsAnAssistantWhoseToolCallIDsAreUnusable(t *testing.T) {
	t.Parallel()
	// An assistant turn whose tool_call IDs are empty or duplicated cannot be paired
	// with its results, so the whole group is dropped rather than emitted as orphans.
	for name, calls := range map[string][]llm.ToolCall{
		"empty id":     {{ID: ""}},
		"duplicate id": {{ID: "dup"}, {ID: "dup"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := []llm.Message{
				{Role: llm.RoleUser, Content: "ask"},
				{Role: llm.RoleAssistant, ToolCalls: calls},
				{Role: llm.RoleTool, ToolCallID: "dup", Content: "result"},
				{Role: llm.RoleUser, Content: "next"},
			}
			got := repairToolMessagePairsWith(in, false)
			if len(got) != 2 || got[0].Content != "ask" || got[1].Content != "next" {
				t.Fatalf("repair kept the unusable group: %+v", got)
			}
		})
	}
}

func TestRepairKeepsCompactedPointersAsAssistantMemory(t *testing.T) {
	t.Parallel()
	pointer := "[" + evictedResultMarker + " see read_tool_output]"
	in := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: ""}}},
		{Role: llm.RoleTool, ToolCallID: "t1", Content: pointer},
		{Role: llm.RoleTool, ToolCallID: "t2", Content: "ordinary result"},
	}
	got := repairToolMessagePairsWith(in, true)
	if len(got) != 1 {
		t.Fatalf("repair returned %d messages, want only the preserved pointer: %+v", len(got), got)
	}
	if got[0].Role != llm.RoleAssistant || got[0].Content != pointer {
		t.Fatalf("pointer was not preserved as assistant memory: %+v", got[0])
	}
}

func TestRepairRecoversAMissingToolResult(t *testing.T) {
	t.Parallel()
	// The assistant's IDs are valid but the group is incomplete, so the recovery leg
	// synthesizes the absent result instead of emitting an unpaired tool call.
	in := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "a"}, {ID: "b"}}},
		{Role: llm.RoleTool, ToolCallID: "a", Content: "real result"},
	}
	got := repairToolMessagePairsWith(in, false)
	if len(got) != 3 {
		t.Fatalf("repair returned %d messages, want assistant + 2 results: %+v", len(got), got)
	}
	if got[2].ToolCallID != "b" || !strings.Contains(got[2].Content, "crash recovery") {
		t.Fatalf("missing result was not recovered: %+v", got[2])
	}
}

func TestRecoveryToolResultContentNamesAnUnnamedTool(t *testing.T) {
	t.Parallel()
	got := recoveryToolResultContent(llm.ToolCall{})
	if !strings.Contains(got, `tool "unknown"`) {
		t.Fatalf("recovery content = %q, want the unknown-tool placeholder", got)
	}
}

func TestToolCallHasIDRejectsEmptyAndAbsentIDs(t *testing.T) {
	t.Parallel()
	calls := []llm.ToolCall{{ID: "a"}}
	if toolCallHasID(calls, "") {
		t.Fatal("an empty tool_call_id matched")
	}
	if toolCallHasID(calls, "b") {
		t.Fatal("an absent tool_call_id matched")
	}
	if !toolCallHasID(calls, "a") {
		t.Fatal("a present tool_call_id did not match")
	}
}

func TestValidToolResultGroupRejectsATruncatedGroup(t *testing.T) {
	t.Parallel()
	// Two calls but only one message left after the assistant: the group cannot be
	// complete, and the check must say so before it indexes past the slice.
	in := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "a"}, {ID: "b"}}},
		{Role: llm.RoleTool, ToolCallID: "a"},
	}
	if validToolResultGroup(in, 0) {
		t.Fatal("a truncated tool-result group was accepted")
	}

	// The right number of messages follow, but one is not a tool result.
	interleaved := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "a"}}},
		{Role: llm.RoleUser, Content: "interrupted"},
		{Role: llm.RoleTool, ToolCallID: "a"},
	}
	if validToolResultGroup(interleaved, 0) {
		t.Fatal("a non-tool message was accepted as a tool result")
	}

	// A result answers an id the assistant never called.
	foreign := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "a"}}},
		{Role: llm.RoleTool, ToolCallID: "b"},
		{Role: llm.RoleUser, Content: "next"},
	}
	if validToolResultGroup(foreign, 0) {
		t.Fatal("a foreign tool_call_id was accepted")
	}
}

func TestReadTurnSidecarReservedFailsClosed(t *testing.T) {
	t.Parallel()

	t.Run("missing run root", func(t *testing.T) {
		t.Parallel()
		s := newFakeStore(&Store{runDir: filepath.Join(t.TempDir(), "absent"), turnCapBytes: 8})
		if _, err := s.readTurnSidecar("c1", 1); err == nil {
			t.Fatal("read succeeded against a missing run root")
		}
	})

	t.Run("sidecar path is a directory", func(t *testing.T) {
		t.Parallel()
		runDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(runDir, "conversations", "c1", "1.content"), 0o750); err != nil {
			t.Fatalf("seed directory: %v", err)
		}
		s := newFakeStore(&Store{runDir: runDir, turnCapBytes: 8})
		_, err := s.readTurnSidecar("c1", 1)
		if err == nil || !strings.Contains(err.Error(), "is a directory") {
			t.Fatalf("read a directory as a sidecar: %v", err)
		}
	})

	t.Run("declared size over the cap", func(t *testing.T) {
		t.Parallel()
		s := seededSidecarStore(t, "c1", 1, "0123456789")
		_, err := s.readTurnSidecarReserved("c1", 1, 4, nil)
		if err == nil || !strings.Contains(err.Error(), "limit 4") {
			t.Fatalf("oversized sidecar was accepted: %v", err)
		}
	})

	t.Run("reservation refused", func(t *testing.T) {
		t.Parallel()
		s := seededSidecarStore(t, "c1", 1, "0123456789")
		refused := errors.New("budget exhausted")
		_, err := s.readTurnSidecarReserved("c1", 1, 64, func(int64) error { return refused })
		if !errors.Is(err, refused) {
			t.Fatalf("reservation error was swallowed: %v", err)
		}
	})

	t.Run("capped read returns the whole turn", func(t *testing.T) {
		t.Parallel()
		s := seededSidecarStore(t, "c1", 2, "payload")
		var reserved int64
		got, err := s.readTurnSidecarReserved("c1", 2, 64, func(n int64) error { reserved = n; return nil })
		if err != nil {
			t.Fatalf("capped read failed: %v", err)
		}
		if string(got) != "payload" || reserved != int64(len("payload")) {
			t.Fatalf("capped read = %q reserved=%d, want the whole turn", got, reserved)
		}
	})
}

func seededSidecarStore(t *testing.T, conversationID string, seq int, body string) *Store {
	t.Helper()
	runDir := t.TempDir()
	dir := filepath.Join(runDir, "conversations", conversationID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("seed sidecar dir: %v", err)
	}
	name := filepath.Join(dir, strconv.Itoa(seq)+".content")
	if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
		t.Fatalf("seed sidecar: %v", err)
	}
	return newFakeStore(&Store{runDir: runDir, turnCapBytes: 8})
}
