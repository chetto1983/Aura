package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// ReadFile is a Go port of hermes-agent's tools/file_tools.py read_file (schema at
// file_tools.py:2159, handler at file_tools.py:2262, implementation at
// file_operations.py:1143). It reads a file from INSIDE the caller's per-identity box through
// boxReadFileCapped/boxReadFileRaw. There is no host arm: a box that cannot be reached fails
// CLOSED (D-09/GATE-01), same invariant the collapsed fs_read carried.
type ReadFile struct {
	Router *usersandbox.SandboxRouter
}

type readFileArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

// maxLineLength clamps a single rendered line, mirroring file_operations.MAX_LINE_LENGTH (2000).
// Without it one pathological line (a minified bundle, a base64 blob pasted into a text file)
// could alone blow the char budget below and starve every other line in the page.
const maxLineLength = 2000

// maxReadChars is read_file's char-count budget on the LINE-NUMBERED output (ported from
// file_tools.py's _DEFAULT_MAX_READ_CHARS). Characters, not tokens, because the tool is
// model-agnostic and can't count tokens; ~100K chars is roughly 25-35K tokens across common
// tokenizers, past which a single read is a context-window hazard the model should page through
// with offset+limit instead.
const maxReadChars = 100_000

// defaultReadFileLimit / maxReadFileLimit are read_file's line-count pagination defaults, per its
// JSON-schema contract (file_tools.py READ_FILE_SCHEMA: default 2000, maximum 2000).
const (
	defaultReadFileLimit = 2000
	maxReadFileLimit     = 2000
)

func (t *ReadFile) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path inside your workspace container (absolute, or relative to /workspace)."},
    "offset": {"type": "integer", "minimum": 1, "default": 1, "description": "1-based line number to start reading from."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 2000, "default": 2000, "description": "Maximum number of lines to return (default and max 2000). Reads are additionally capped at a ~100K-character budget with a next_offset continuation."}
  },
  "required": ["path"]
}`)
	return Spec{
		Name:    "read_file",
		Summary: "Read a file with line numbers and pagination.",
		Description: "Read a text file from your workspace container, with line numbers and pagination. Use this instead " +
			"of cat/head/tail in shell_exec. Output format: 'LINE_NUM|CONTENT'. `path` is absolute (e.g. /workspace/notes.md) " +
			"or relative to /workspace. Use offset and limit to page through a large file instead of pulling the whole thing " +
			"into context; a read is additionally capped at a ~100K-character budget and, if hit, is truncated on a line " +
			"boundary with a next_offset hint to continue from. If the path does not exist, similar filenames in the same " +
			"directory are suggested. Jupyter notebooks (.ipynb), Word documents (.docx), and Excel workbooks (.xlsx) are " +
			"auto-extracted to readable text; other binary files (images included) are refused with a pointer to shell_exec " +
			"or send_file instead. Read a file with this tool BEFORE patching it — `patch` matches against the exact bytes " +
			"returned here. Example: {\"path\":\"cmd/aura/main.go\"} for a whole file, or " +
			"{\"path\":\"app.log\",\"offset\":2000,\"limit\":200} to read just a window.",
		Parameters: params,
		// NOT deferred, alone among the four ported file tools (matches the pre-port fs_read):
		// reading a file whose path the model already has is not a choice between tools, it is
		// the next step, and keeping it deferred bought a search round trip in the middle of
		// every open-then-read flow (see always_active_test.go for the full budget rationale).
		Deferred: false,
	}
}

func (t *ReadFile) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a readFileArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("read_file args: %w", err)
	}
	if strings.TrimSpace(a.Path) == "" {
		return ToolResult{}, fmt.Errorf("read_file: path is required")
	}
	offset, limit := normalizeReadPagination(a.Offset, a.Limit)

	boxPath, err := boxPathArg("read_file", a.Path)
	if err != nil {
		return ToolResult{}, err
	}

	// Argument guards run BEFORE the route on purpose: a malformed path is the model's own
	// error, not a sandbox outage, and must read as one.
	boxHandle, routeErr := t.Router.Route(ctx)
	if routeErr != nil {
		return sandboxUnavailableResult("read_file", routeErr), nil
	}

	content, isExtracted, err := t.readContent(ctx, boxHandle, boxPath)
	if err != nil {
		if deny, ok := err.(*sandboxDenyError); ok {
			return deny.result, nil
		}
		if isNotFoundErr(err) {
			return NewResult(ctx, t.notFoundWithSuggestions(ctx, boxHandle, boxPath, a.Path))
		}
		return ToolResult{}, err
	}

	return NewResult(ctx, renderReadFile(content, offset, limit, isExtracted))
}

// sandboxDenyError carries a fail-closed deny ToolResult through readContent's plain error return,
// so the box-unreachable case still degrades to a ToolResult rather than a Go error.
type sandboxDenyError struct{ result ToolResult }

func (e *sandboxDenyError) Error() string { return "sandbox unavailable" }

// readContent returns the file's full text content (bounded by fsMaxReadBytes), transparently
// extracting a supported document format first. isExtracted flags the latter so the caller can
// note it in the rendered output.
func (t *ReadFile) readContent(ctx context.Context, h usersandbox.BoxHandle, boxPath string) (string, bool, error) {
	if isExtractableDocument(boxPath) {
		raw, deny, err := boxReadFileRaw(ctx, t.Router, h, "read_file", boxPath)
		if deny != nil {
			return "", false, &sandboxDenyError{result: *deny}
		}
		if err == nil {
			text, extractErr := extractDocumentText(boxPath, raw)
			if extractErr == nil {
				return text, true, nil
			}
			// Malformed document: fall through to the normal binary/text path below,
			// mirroring read_extract's "extraction failure falls back" contract.
		} else if !isNotFoundErr(err) {
			return "", false, err
		}
	}

	b, deny, err := boxReadFileCapped(ctx, t.Router, h, "read_file", boxPath)
	if deny != nil {
		return "", false, &sandboxDenyError{result: *deny}
	}
	if err != nil {
		if strings.Contains(err.Error(), "binary file contains NUL bytes") {
			return "", false, fmt.Errorf("read_file: cannot read binary file %q; use shell_exec to inspect it "+
				"or send_file to hand it to the user (images included — there is no vision tool)", boxPath)
		}
		return "", false, err
	}
	return string(b), false, nil
}

// isNotFoundErr recognizes the shell's "no such file" phrasing from a failed head -c exec — the
// only failure shape that should trigger the similar-filename suggestion walk rather than a plain
// error, mirroring read_file_tool's file-not-found branch (file_tools.py:1442).
func isNotFoundErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such file") || strings.Contains(msg, "not a directory")
}

// normalizeReadPagination mirrors file_operations.normalize_read_pagination: offset floors at 1,
// limit floors at 1 and is capped at maxReadFileLimit — a caller-supplied limit above the cap is
// silently clamped rather than rejected, matching the JSON-schema's own "maximum" contract.
func normalizeReadPagination(offset, limit int) (int, int) {
	if offset < 1 {
		offset = 1
	}
	if limit <= 0 {
		limit = defaultReadFileLimit
	}
	if limit > maxReadFileLimit {
		limit = maxReadFileLimit
	}
	return offset, limit
}

// renderReadFile applies the line window, the LINE_NUM|CONTENT gutter, and the char-budget
// truncation, and renders the whole thing (content + trailing hint) as the tool's plain-text
// result. This is the one place read_file's pagination contract is implemented; patch.go's replace
// mode does NOT go through here — it reads raw content for exact-byte matching.
func renderReadFile(content string, offset, limit int, extracted bool) string {
	lines := splitLines(content)
	totalLines := len(lines)

	start := min(offset-1, totalLines)
	end := min(start+limit, totalLines)
	page := lines[start:end]

	var gutter strings.Builder
	for i, line := range page {
		if len(line) > maxLineLength {
			line = line[:maxLineLength] + "... [truncated]"
		}
		if i > 0 {
			gutter.WriteByte('\n')
		}
		gutter.WriteString(strconv.Itoa(offset + i))
		gutter.WriteByte('|')
		gutter.WriteString(line)
	}
	rendered := gutter.String()

	truncatedByLimit := end < totalLines
	var hint string
	if len(rendered) > maxReadChars {
		trimmed, linesKept := truncateToCharBudget(rendered, maxReadChars)
		nextOffset := offset + linesKept
		shownEnd := offset + linesKept - 1
		hint = fmt.Sprintf("\n\n[Output truncated at the %d-char read budget after %d line(s) "+
			"(showing lines %d-%d of %d). Use offset=%d to continue.]",
			maxReadChars, linesKept, offset, shownEnd, totalLines, nextOffset)
		rendered = trimmed
	} else if truncatedByLimit {
		hint = fmt.Sprintf("\n\n[Truncated: showing lines %d-%d of %d. Use offset=%d to continue reading.]",
			offset, end, totalLines, end+1)
	}

	if extracted {
		rendered = "[extracted from a document format — this is rendered text, not the raw file bytes]\n" + rendered
	}
	if rendered == "" {
		rendered = "[empty file]"
	}
	return rendered + hint
}

// splitLines splits content into its real lines the way a line-numbered display wants them: a
// single trailing newline (if present) is not rendered as a phantom empty final line.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	content = strings.TrimSuffix(content, "\n")
	return strings.Split(content, "\n")
}

// truncateToCharBudget trims LINE_NUM|CONTENT text to the last complete line that fits within
// maxChars, ported from file_tools._truncate_to_char_budget. It never returns an empty string when
// content is non-empty: if even the first line overflows the budget, that line is clamped instead
// of dropped, so a read never comes back with nothing and the cursor can still advance.
func truncateToCharBudget(content string, maxChars int) (kept string, linesKept int) {
	if len(content) <= maxChars {
		n := 0
		if content != "" {
			n = strings.Count(content, "\n") + 1
		}
		return content, n
	}
	lines := strings.Split(content, "\n")
	var out []string
	running := 0
	for _, line := range lines {
		addition := len(line)
		if len(out) > 0 {
			addition++ // the "\n" that rejoins this line to the previous one
		}
		if running+addition > maxChars {
			break
		}
		out = append(out, line)
		running += addition
	}
	if len(out) == 0 {
		return truncateRuneBoundary(lines[0], maxChars), 1
	}
	return strings.Join(out, "\n"), len(out)
}

// truncateRuneBoundary clamps s to at most maxBytes bytes without splitting a multi-byte rune.
func truncateRuneBoundary(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut]
}

// notFoundWithSuggestions renders read_file_tool's "File not found" branch (file_tools.py's
// _suggest_similar_files, ported at file_operations.py:1240): list the target's parent directory
// and score candidate names by how closely they resemble the missing filename, so a typo'd
// extension or path segment surfaces the real file instead of a bare ENOENT.
func (t *ReadFile) notFoundWithSuggestions(ctx context.Context, h usersandbox.BoxHandle, boxPath, originalPath string) string {
	dir := boxDir(boxPath)
	base := boxBase(boxPath)

	cmd := fmt.Sprintf("ls -1 -- %s 2>/dev/null | head -50", ShellQuoteArg(dir))
	res, err := t.Router.Exec(ctx, h, usersandbox.ExecRequest{Command: cmd})
	msg := fmt.Sprintf("File not found: %s", originalPath)
	if err != nil || res.ExitCode != 0 {
		return msg
	}
	var candidates []string
	for name := range strings.SplitSeq(strings.TrimRight(string(res.Stdout), "\n"), "\n") {
		if name != "" {
			candidates = append(candidates, name)
		}
	}
	similar := scoreSimilarNames(base, candidates)
	if len(similar) == 0 {
		return msg
	}
	var b strings.Builder
	b.WriteString(msg)
	b.WriteString("\nSimilar files in the same directory:")
	for _, name := range similar {
		b.WriteString("\n  ")
		b.WriteString(boxJoin(dir, name))
	}
	return b.String()
}

// scoreSimilarNames ranks candidate file names against target by the same heuristic ladder as
// _suggest_similar_files: exact (guard case), same-stem-different-extension, prefix relation,
// substring either direction, then same-extension character overlap. Returns up to 5, best first.
func scoreSimilarNames(target string, candidates []string) []string {
	targetLower := strings.ToLower(target)
	targetStem, targetExt := splitExt(target)

	type scored struct {
		score int
		name  string
	}
	var out []scored
	for _, c := range candidates {
		lc := strings.ToLower(c)
		stem, ext := splitExt(c)
		score := 0
		switch {
		case lc == targetLower:
			score = 100
		case strings.EqualFold(stem, targetStem):
			score = 90
		case strings.HasPrefix(lc, targetLower) || strings.HasPrefix(targetLower, lc):
			score = 70
		case strings.Contains(lc, targetLower):
			score = 60
		case len(lc) > 2 && strings.Contains(targetLower, lc):
			score = 40
		case targetExt != "" && strings.EqualFold(ext, targetExt):
			if charOverlapRatio(targetLower, lc) >= 0.4 {
				score = 30
			}
		}
		if score > 0 {
			out = append(out, scored{score, c})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	if len(out) > 5 {
		out = out[:5]
	}
	names := make([]string, len(out))
	for i, s := range out {
		names[i] = s.name
	}
	return names
}

func charOverlapRatio(a, b string) float64 {
	seen := map[rune]bool{}
	for _, r := range a {
		seen[r] = true
	}
	common := 0
	counted := map[rune]bool{}
	for _, r := range b {
		if seen[r] && !counted[r] {
			common++
			counted[r] = true
		}
	}
	longer := max(len(b), len(a))
	if longer == 0 {
		return 0
	}
	return float64(common) / float64(longer)
}

func splitExt(name string) (stem, ext string) {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[:i], name[i:]
	}
	return name, ""
}

func boxDir(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		if i == 0 {
			return "/"
		}
		return p[:i]
	}
	return "."
}

func boxBase(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func boxJoin(dir, name string) string {
	if strings.HasSuffix(dir, "/") {
		return dir + name
	}
	return dir + "/" + name
}
