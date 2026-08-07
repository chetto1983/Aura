package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// Patch is a Go port of hermes-agent's tools/file_tools.py patch (schema at file_tools.py:2191,
// handler at file_tools.py:2294, implementation at file_operations.py's patch_replace/patch_v4a).
// It edits a file INSIDE the caller's per-identity box. There is no host arm: a box that cannot be
// reached fails CLOSED (D-09/GATE-01), and the skills-library fence (#54 / D-43) is box-relative
// over the literal /skills mount.
//
// hermes' nine-strategy hand-rolled fuzzy matcher (fuzzy_match.py) is deliberately NOT ported —
// github.com/sergi/go-diff/diffmatchpatch (the Go port of Google's diff-match-patch) solves the
// same problem: PatchMake(old_string, new_string) builds a patch anchored on old_string's own
// text, and PatchApply(patch, content) locates the best fuzzy match of that anchor inside content
// (Bitap, weighted for accuracy AND location) before applying — best-effort exactly when the
// file's bytes do not match old_string byte-for-byte (whitespace/indentation drift, re-wrapped
// lines). One library call replaces the whole strategy ladder.
//
// mode="patch" accepts a diff-match-patch FORMAT patch (the text PatchToText produces / PatchFromText
// consumes: one or more "@@ -start1,len1 +start2,len2 @@" hunks, each line prefixed ' '/'-'/'+') —
// not hermes' OpenAI-specific V4A dialect, which has no maintained Go parser and is not something
// this port hand-rolls. Both modes go through the SAME PatchApply fuzzy-application path, so a
// hunk written against a slightly different on-disk version of the file still has a chance to land.
type Patch struct {
	Router *usersandbox.SandboxRouter
}

type patchArgs struct {
	Mode       string `json:"mode"`
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
	PatchText  string `json:"patch"`
}

func (t *Patch) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "mode": {"type": "string", "enum": ["replace", "patch"], "description": "Edit mode. 'replace' (default): requires path + old_string + new_string. 'patch': requires path + patch.", "default": "replace"},
    "path": {"type": "string", "description": "REQUIRED in both modes. File path inside your workspace container (absolute, or relative to /workspace)."},
    "old_string": {"type": "string", "description": "REQUIRED when mode='replace'. Text to find and replace. Must be unique in the file unless replace_all is true. If the exact bytes are not found, a fuzzy match is attempted (whitespace/indentation drift tolerated)."},
    "new_string": {"type": "string", "description": "REQUIRED when mode='replace'. Replacement text. Pass an empty string to delete the matched text. Must differ from old_string."},
    "replace_all": {"type": "boolean", "description": "Replace every exact occurrence instead of requiring a unique match (default false). Only applies to exact matches, never to a fuzzy one.", "default": false},
    "patch": {"type": "string", "description": "REQUIRED when mode='patch'. A diff-match-patch format patch: one or more '@@ -start1,len1 +start2,len2 @@' hunks, each following line prefixed ' ' (context), '-' (remove) or '+' (add). This is the text diffmatchpatch's own PatchToText produces, NOT a git/V4A patch."}
  },
  "required": ["mode", "path"]
}`)
	return Spec{
		Name:    "patch",
		Summary: "Targeted find-and-replace edit in a file, with fuzzy fallback.",
		Description: "Targeted edit of a file in your workspace container. Use this instead of sed/awk in " +
			"shell_exec. Returns a unified diff of what changed. Auto-fails closed (no write) on ambiguous or " +
			"missing matches so you can retry with more context.\n\n" +
			"REPLACE MODE (mode='replace', default): find old_string and replace it with new_string. REQUIRED: " +
			"mode, path, old_string, new_string. old_string must be exactly unique in the file unless " +
			"replace_all=true. If the exact bytes are not present, a fuzzy match is attempted (tolerates " +
			"whitespace/indentation differences from what you remember reading) — read the file first with " +
			"read_file so old_string starts as close to the real bytes as possible.\n\n" +
			"PATCH MODE (mode='patch'): apply a diff-match-patch format patch to path. REQUIRED: mode, path, " +
			"patch. Same fuzzy application as replace mode, so a hunk still lands if the file drifted slightly " +
			"since you last read it.",
		Parameters: params,
		// Always visible, with read_file/write_file/search_files: they are one toolset and
		// splitting them left the model able to read and write but not edit or find without
		// first paying a tool_search round-trip.
		Deferred:       false,
		Mutating:       true,
		OperationScope: OperationScopeAgent, OperationNormalizer: OperationNormalizerCanonical,
		ReplayPolicy: ReplayToolResult,
	}
}

func (t *Patch) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a patchArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("patch args: %w", err)
	}
	mode := strings.TrimSpace(a.Mode)
	if mode == "" {
		mode = "replace"
	}
	if mode != "replace" && mode != "patch" {
		return ToolResult{}, fmt.Errorf("patch: mode must be 'replace' or 'patch', got %q", mode)
	}
	if strings.TrimSpace(a.Path) == "" {
		return ToolResult{}, fmt.Errorf("patch: path is required")
	}
	switch mode {
	case "replace":
		if a.OldString == "" {
			return ToolResult{}, fmt.Errorf("patch: mode='replace' requires 'old_string'. Re-emit the tool " +
				"call with old_string set, or use write_file to create a new file")
		}
		if a.OldString == a.NewString {
			return ToolResult{}, fmt.Errorf("patch: old_string and new_string are identical")
		}
	case "patch":
		if strings.TrimSpace(a.PatchText) == "" {
			return ToolResult{}, fmt.Errorf("patch: mode='patch' requires 'patch'. Re-emit the tool call with " +
				"the diff-match-patch format patch text — this is almost always a dropped-arg bug under " +
				"context pressure")
		}
	}

	boxPath, err := boxPathArg("patch", a.Path)
	if err != nil {
		return ToolResult{}, err
	}
	if deniedBoxSkillsWrite(boxPath) {
		return ToolResult{}, fmt.Errorf("patch: %s is inside the sandbox skills mount; author skills through "+
			"the gated `skill` tool (action=create/update/delete), not direct file edits", boxPath)
	}

	// Every guard above is argument-based and runs BEFORE the route on purpose: a malformed
	// call or a fenced target is the model's own error, not a sandbox outage.
	boxHandle, routeErr := t.Router.Route(ctx)
	if routeErr != nil {
		return sandboxUnavailableResult("patch", routeErr), nil
	}

	content, deny, err := boxReadFileCapped(ctx, t.Router, boxHandle, "patch", boxPath)
	if deny != nil {
		return *deny, nil
	}
	if err != nil {
		return ToolResult{}, err
	}
	original := string(content)

	var newContent string
	var note string
	switch mode {
	case "replace":
		newContent, note, err = t.applyReplace(original, a)
	case "patch":
		newContent, err = t.applyPatchText(original, a.PatchText)
	}
	if err != nil {
		return ToolResult{}, err
	}
	if note != "" {
		// A success-shaped no-op: the edit already landed. No write, no diff.
		return NewResult(ctx, note)
	}

	if err := t.Router.WriteFile(ctx, boxHandle, boxPath, []byte(newContent)); err != nil {
		return sandboxUnavailableResult("patch", err), nil
	}
	if _, verr := verifyBoxWrite(ctx, t.Router, boxHandle, boxPath, []byte(newContent)); verr != nil {
		return ToolResult{}, fmt.Errorf("patch: post-write verification failed for %s: on-disk content hash "+
			"differs from the intended write. The patch did not persist correctly — re-read the file and retry: %w",
			boxPath, verr)
	}

	diff := unifiedDiff(boxPath, original, newContent)
	return NewResult(ctx, diff)
}

// applyReplace implements mode="replace": an exact, unique-match replace when possible, falling
// back to diffmatchpatch's fuzzy PatchApply when old_string's exact bytes are not present. Returns
// (newContent, "", nil) on a real edit, or ("", note, nil) for a success-shaped no-op (the edit was
// already applied), or an error that diagnoses what went wrong.
func (t *Patch) applyReplace(content string, a patchArgs) (string, string, error) {
	count := strings.Count(content, a.OldString)
	switch {
	case count == 1, count > 1 && a.ReplaceAll:
		if a.ReplaceAll {
			return strings.ReplaceAll(content, a.OldString, a.NewString), "", nil
		}
		return strings.Replace(content, a.OldString, a.NewString, 1), "", nil
	case count > 1:
		return "", "", fmt.Errorf("patch: old_string matches %d times in the file; add surrounding context to "+
			"make it unique, or set replace_all", count)
	}

	// count == 0: exact bytes not found. An already-applied edit is the most common cause in
	// production (a re-sent patch whose old_string is gone because new_string already landed) —
	// surface that as a no-op before trying a fuzzy match that would otherwise find nothing.
	if isAlreadyApplied(content, a.OldString, a.NewString) {
		return "", fmt.Sprintf("File already contains the target text — the edit appears to already be " +
			"applied. No write performed; do not re-send this patch."), nil
	}

	newContent, ok := dmpFuzzyReplace(content, a.OldString, a.NewString)
	if !ok {
		return "", "", fmt.Errorf("patch: could not find old_string in the file, exactly or fuzzily. " +
			"Re-read the file with read_file and copy old_string from the current bytes — this is almost " +
			"always a stale view of the file under context pressure")
	}
	return newContent, "", nil
}

// applyPatchText implements mode="patch": parse the diff-match-patch format text and apply it
// fuzzily against content.
func (t *Patch) applyPatchText(content, patchText string) (string, error) {
	dmp := diffmatchpatch.New()
	patches, err := dmp.PatchFromText(patchText)
	if err != nil {
		return "", fmt.Errorf("patch: could not parse patch text: %w", err)
	}
	if len(patches) == 0 {
		return "", fmt.Errorf("patch: patch text carries no hunks")
	}
	newContent, results := dmp.PatchApply(patches, content)
	for i, applied := range results {
		if !applied {
			return "", fmt.Errorf("patch: hunk %d of %d did not apply, exactly or fuzzily — the file has "+
				"drifted too far from the patch's context. Re-read the file with read_file and regenerate the patch",
				i+1, len(results))
		}
	}
	return newContent, nil
}

// isAlreadyApplied mirrors fuzzy_match.is_already_applied: old_string is absent AND new_string is
// already present verbatim — the most common "patch" failure in production is a re-send of an
// edit that already landed.
// minAlreadyAppliedEvidence is how much of new_string must be present before "you already applied
// this" is a defensible read of the file. A short new_string matches by accident and the tool then
// reports a no-op for an edit that never happened — the model is told its change landed and the
// change silently disappears. Measured 2026-08-07: new_string "y" was found in a file whose whole
// content was "totally unrelated content" (inside "totally"), so a genuine edit was refused as
// already-applied. Below this length the fuzzy leg decides instead, which fails loudly.
const minAlreadyAppliedEvidence = 12

func isAlreadyApplied(content, oldString, newString string) bool {
	if strings.Contains(content, oldString) || len(newString) < minAlreadyAppliedEvidence {
		return false
	}
	return strings.Contains(content, newString)
}

// Bitap tuning. diff-match-patch ships MatchThreshold 0.5 / MatchDistance 1000, which accepts a
// match with half the pattern wrong, anywhere within a thousand characters. On a STALE old_string
// that is not a tolerant fuzzy match, it is a wrong-place edit reported as success: measured
// 2026-08-07, old_string "zzz nowhere near" applied cleanly against a file whose entire content
// was "totally unrelated content". A patch tool that silently edits the wrong region is worse
// than one that refuses, because the model has no way to notice.
const (
	fuzzyMatchThreshold = 0.15
	fuzzyMatchDistance  = 200
	// matchMaxBits mirrors diff-match-patch's Bitap word size: the locator only looks at this
	// many leading characters, so the anchor probe below must use the same prefix the library
	// itself would.
	matchMaxBits = 32
)

// dmpFuzzyReplace anchors a patch on old_string's own text (PatchMake), then applies it against
// content (PatchApply), which relocates the anchor fuzzily via Bitap before splicing in new_string.
// Returns (content, false) unchanged when the anchor cannot be located or any hunk fails to apply
// — never a partial edit, and never an edit at a location the anchor did not actually match.
func dmpFuzzyReplace(content, oldString, newString string) (string, bool) {
	dmp := diffmatchpatch.New()
	dmp.MatchThreshold = fuzzyMatchThreshold
	dmp.MatchDistance = fuzzyMatchDistance
	dmp.PatchDeleteThreshold = fuzzyMatchThreshold

	// Locate BEFORE patching. PatchApply on its own reports success for a pattern that appears
	// nowhere in the file: it relocates the anchor and splices anyway. Probing with MatchMain
	// first turns "not in this file" into a refusal instead of a corruption.
	probe := oldString
	if len(probe) > matchMaxBits {
		probe = probe[:matchMaxBits]
	}
	if dmp.MatchMain(content, probe, 0) < 0 {
		return content, false
	}

	patches := dmp.PatchMake(oldString, newString)
	if len(patches) == 0 {
		return content, false
	}
	patched, results := dmp.PatchApply(patches, content)
	for _, ok := range results {
		if !ok {
			return content, false
		}
	}
	return patched, true
}
