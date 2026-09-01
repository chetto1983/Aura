package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// WriteFile is a Go port of hermes-agent's tools/file_tools.py write_file (schema at
// file_tools.py:2173, handler at file_tools.py:2267, implementation at
// file_operations.py:1412). It writes (creating or overwriting) a file INSIDE the caller's
// per-identity box through the router's tar copy-in. There is no host arm: a box that cannot be
// reached fails CLOSED (D-09/GATE-01). The skills-library fence (#54 / D-43) is box-relative over
// the literal /skills mount and needs no configured directory.
//
// TWO GUARANTEES HERMES' local backend HAS AND THIS PATH DOES NOT, recorded rather than hidden
// (carried over from the pre-port fs_write's own note): an overwrite does not preserve the
// target's mode, and there is no client-side temp-file+rename — Router.WriteFile hardcodes 0o644
// and CopyFileIn tar-extracts in place, the only write primitive the box exposes.
type WriteFile struct {
	Router *usersandbox.SandboxRouter
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *WriteFile) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path inside your workspace container (absolute, or relative to /workspace). Created if it doesn't exist, OVERWRITTEN if it does."},
    "content": {"type": "string", "description": "Complete content to write to the file."}
  },
  "required": ["path", "content"]
}`)
	return Spec{
		Name:    "write_file",
		Summary: "Write a file to disk (create or overwrite).",
		Description: "Write content to a file in your workspace container, completely replacing any existing content. Use " +
			"this instead of echo/cat-heredoc in shell_exec — content rides the tool call, never a shell command string, " +
			"so quoting never breaks it. Creates parent directories automatically. OVERWRITES the entire file; use `patch` " +
			"for a targeted edit that preserves the rest of the file. Pass the COMPLETE `content`. The result's " +
			"verified:true means the on-disk content hash was confirmed — do NOT re-read the file with read_file to check " +
			"the write landed; that hash check already did. Always report the absolute path of what you wrote. " +
			"Example: {\"path\":\"results/report.md\",\"content\":\"# Results\\n\\nAll tests passed.\\n\"}.",
		Parameters: params,
		// Deferred alongside patch/search_files (matches the pre-port fs_write): only read_file
		// stays always-visible.
		Deferred:       false,
		Mutating:       true,
		OperationScope: OperationScopeAgent, OperationNormalizer: OperationNormalizerCanonical,
		ReplayPolicy: ReplayToolResult,
	}
}

func (t *WriteFile) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a writeFileArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("write_file args: %w", err)
	}
	if strings.TrimSpace(a.Path) == "" {
		return ToolResult{}, fmt.Errorf("write_file: missing required field 'path'. Re-emit the tool call with " +
			"both 'path' and 'content' set")
	}
	// write_file's raw JSON always carries a "content" key once Unmarshal succeeds — an omitted
	// field decodes to "", which is indistinguishable from an intentional empty-file write at
	// this layer. hermes tells them apart by inspecting the raw args dict for key presence
	// (file_tools.py:2274); mirror that here rather than silently writing an empty file for what
	// is, per hermes' own telemetry, almost always a dropped-arg bug under context pressure.
	if !hasJSONKey(raw, "content") {
		return ToolResult{}, fmt.Errorf("write_file: missing required field 'content'. The tool call included a " +
			"path but no content argument — this is almost always a dropped-arg bug under context pressure. " +
			"Re-emit the tool call with the full content payload")
	}
	if cap := fsMaxReadBytes(); int64(len(a.Content)) > cap {
		return ToolResult{}, fmt.Errorf("write_file: content is %d bytes, over the %d-byte cap (%s)", len(a.Content), cap, envFSMaxReadBytes)
	}
	boxPath, err := boxPathArg("write_file", a.Path)
	if err != nil {
		return ToolResult{}, err
	}
	if deniedBoxSkillsWrite(boxPath) {
		return ToolResult{}, fmt.Errorf("write_file: %s is inside the sandbox skills mount; author skills through the gated "+
			"`skill` tool (action=create/update/delete), not direct file writes", boxPath)
	}

	// Every guard above is content- or argument-based and runs BEFORE the route on purpose: an
	// oversized write, a bad path or a fenced target is the model's own error, not a sandbox
	// outage, and must read as one — and the skills fence must refuse without touching the box.
	boxHandle, routeErr := t.Router.Route(ctx)
	if routeErr != nil {
		return sandboxUnavailableResult("write_file", routeErr), nil
	}
	if err := t.Router.WriteFile(ctx, boxHandle, boxPath, []byte(a.Content)); err != nil {
		return sandboxUnavailableResult("write_file", err), nil
	}

	verified, verr := verifyBoxWrite(ctx, t.Router, boxHandle, boxPath, []byte(a.Content))
	if verr != nil {
		return ToolResult{}, fmt.Errorf("write_file: post-write verification failed for %s: on-disk content hash "+
			"differs from the intended write. The write did not persist correctly — re-read the file and retry: %w", boxPath, verr)
	}
	result, err := NewResult(ctx, fmt.Sprintf("wrote %d bytes to %s\nverified:%t — the on-disk content hash was confirmed; "+
		"do not re-read the file to check the write landed", len(a.Content), boxPath, verified))
	if err != nil {
		return ToolResult{}, err
	}
	attachDurableArtifactEvidence(ctx, &result, boxPath, "write")
	return result, nil
}

// hasJSONKey reports whether raw's top-level object carries key, distinguishing an omitted field
// from one explicitly sent as its zero value.
func hasJSONKey(raw json.RawMessage, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}

// verifyBoxWrite re-hashes the file inside the box (sha256sum) and compares it against the
// content's own sha256, mirroring write_file's post-write verification (file_operations.py:1601):
// production mining showed models re-reading a file right after writing it just to confirm
// persistence, so an explicit verified flag removes the need for that round trip. verified is
// false (not an error) only when the box has no sha256sum to check with; a HASH MISMATCH is
// always returned as an error, never a silent flag.
func verifyBoxWrite(
	ctx context.Context,
	router *usersandbox.SandboxRouter,
	h usersandbox.BoxHandle,
	boxPath string,
	content []byte,
) (verified bool, err error) {
	res, execErr := router.Exec(ctx, h, usersandbox.ExecRequest{
		Command: fmt.Sprintf("sha256sum -- %s 2>/dev/null", ShellQuoteArg(boxPath)),
	})
	if execErr != nil || res.ExitCode != 0 {
		return false, nil // no sha256sum available inside this box image — degrade to unverified, not an error
	}
	fields := strings.Fields(string(res.Stdout))
	if len(fields) == 0 {
		return false, nil
	}
	sum := sha256.Sum256(content)
	expected := hex.EncodeToString(sum[:])
	if fields[0] != expected {
		return false, fmt.Errorf("expected sha256 %s, box reports %s", expected, fields[0])
	}
	return true, nil
}
