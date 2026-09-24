package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	pathpkg "path"
	"strings"
	"unicode"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// MCPFileSink puts the files an MCP tool result carried into the caller's box, at
// /workspace/mcp-files/<request-id>/<server>/<name>, and asks the turn to remove
// /workspace/mcp-files/<request-id> when it ends. It writes through the router seam
// document_open uses, for the reason document_open gives: a path the agent is handed
// must be one shell_exec and fs_read can open.
//
// It lives here, not in mcptools, so the MCP bridge never imports the sandbox; the
// bridge sees it as mcptools.FileSink.
type MCPFileSink struct {
	Router *usersandbox.SandboxRouter
}

const (
	mcpFilesBoxDir = boxWorkspaceRoot + "/mcp-files"
	// maxMCPFileNameBytes keeps a server's file name well under the 255-byte limit of
	// every filesystem the box mounts, with room left for a -N suffix.
	maxMCPFileNameBytes = 200
	// maxMCPExtensionBytes is the longest suffix still kept as an extension when a
	// long name is cut; anything longer is part of the name.
	maxMCPExtensionBytes = 16
)

// mcpFileExtensions gives a nameless or extensionless file an extension from its
// MIME type. It lists the types mail and chat actually carry; any other type gets no
// extension rather than a guess.
var mcpFileExtensions = map[string]string{
	"application/pdf": ".pdf",
	"audio/mp4":       ".m4a",
	"audio/mpeg":      ".mp3",
	"audio/ogg":       ".ogg",
	"image/gif":       ".gif",
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"text/plain":      ".txt",
	"video/mp4":       ".mp4",
}

// Materialize writes every part it can and reports each part's outcome, in order.
// A part it cannot write is reported with the reason, and the others still go in.
func (s *MCPFileSink) Materialize(ctx context.Context, server string, parts []mcp.FilePart) []mcp.FileOutcome {
	out := make([]mcp.FileOutcome, len(parts))
	pending := make([]int, 0, len(parts))
	total := 0
	for i, part := range parts {
		switch {
		case part.Unavailable != "":
			out[i] = part.NotMaterialized("")
		case len(part.Data) > mcp.MaxFileBytes:
			out[i] = part.NotMaterialized(mcp.FileCapExceeded(int64(len(part.Data))))
		default:
			pending = append(pending, i)
			total += len(part.Data)
		}
	}
	if len(pending) == 0 {
		return out
	}
	w, reason := s.open(ctx, server, total)
	for _, i := range pending {
		if reason != "" {
			out[i] = parts[i].NotMaterialized(reason)
			continue
		}
		out[i] = w.write(ctx, parts[i])
	}
	return out
}

// open routes to the caller's box and prepares the call's directory. The turn owns
// it, so it is registered for removal before anything is written into it.
func (s *MCPFileSink) open(ctx context.Context, server string, total int) (*mcpFileWriter, string) {
	cleanup := TurnCleanupFromContext(ctx)
	requestID := RequestIDFromContext(ctx)
	if cleanup == nil || requestID == "" {
		// Nothing would ever remove the file: toolpipe, the docs MCP and a readiness
		// probe get the text alone.
		return nil, "no agent turn owns the file"
	}
	if total > mcp.MaxCallFileBytes {
		return nil, fmt.Sprintf("the call's files exceed the %d-byte cap", mcp.MaxCallFileBytes)
	}
	handle, err := s.Router.Route(ctx)
	if err != nil {
		return nil, "sandbox unavailable: " + err.Error()
	}
	turnDir := pathpkg.Join(mcpFilesBoxDir, mcpPathSegment(requestID))
	cleanup.Add(handle.ContainerID+":"+turnDir, func(ctx context.Context) error {
		return removeBoxDir(ctx, s.Router, handle, turnDir)
	})
	dir := pathpkg.Join(turnDir, mcpPathSegment(server))
	taken, err := s.names(ctx, handle, dir)
	if err != nil {
		return nil, "sandbox unavailable: " + err.Error()
	}
	return &mcpFileWriter{router: s.Router, handle: handle, dir: dir, taken: taken}, ""
}

// names lists what dir already holds, so a second file of the same name in the same
// turn gets a -2 instead of overwriting the first. A dir that does not exist yet is
// empty.
func (s *MCPFileSink) names(ctx context.Context, h usersandbox.BoxHandle, dir string) (map[string]bool, error) {
	res, err := s.Router.Exec(ctx, h, usersandbox.ExecRequest{Command: "ls -1A -- " + ShellQuoteArg(dir) + " 2>/dev/null"})
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for name := range strings.SplitSeq(string(res.Stdout), "\n") {
		if name != "" {
			taken[name] = true
		}
	}
	return taken, nil
}

func removeBoxDir(ctx context.Context, router *usersandbox.SandboxRouter, h usersandbox.BoxHandle, dir string) error {
	res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Command: "rm -rf -- " + ShellQuoteArg(dir)})
	if err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("remove %s: exit %d: %s", dir, res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

// mcpFileWriter writes one call's files into one directory of one box.
type mcpFileWriter struct {
	router *usersandbox.SandboxRouter
	handle usersandbox.BoxHandle
	dir    string
	taken  map[string]bool
}

func (w *mcpFileWriter) write(ctx context.Context, part mcp.FilePart) mcp.FileOutcome {
	name := uniqueMCPFileName(mcpFileName(part.Name, part.MIMEType), w.taken)
	boxPath := pathpkg.Join(w.dir, name)
	if err := writeBoxFile(ctx, w.router, w.handle, boxPath, int64(len(part.Data)), bytes.NewReader(part.Data)); err != nil {
		return part.NotMaterialized("write failed: " + err.Error())
	}
	w.taken[name] = true
	sum := sha256.Sum256(part.Data)
	return mcp.FileOutcome{
		Path:      boxPath,
		Name:      name,
		MIMEType:  part.MIMEType,
		SizeBytes: int64(len(part.Data)),
		SHA256:    hex.EncodeToString(sum[:]),
	}
}

// mcpFileName makes a server's file name one path component the box can hold, the
// rule document_open applies to its file names (documents.ValidateStagedFileName):
// the last segment of whatever path it names, without leading dots or control
// characters, at most maxMCPFileNameBytes, and with an extension from its MIME type
// when it has none. Spaces and accents stay: they are part of the name the human
// knows the file by.
func mcpFileName(name, mimeType string) string {
	name = name[strings.LastIndexAny(name, `/\`)+1:]
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	if name == "" {
		name = "file"
	}
	if pathpkg.Ext(name) == "" {
		base, _, _ := strings.Cut(mimeType, ";")
		name += mcpFileExtensions[strings.ToLower(strings.TrimSpace(base))]
	}
	if len(name) > maxMCPFileNameBytes {
		ext := pathpkg.Ext(name)
		if len(ext) > maxMCPExtensionBytes {
			ext = ""
		}
		name = truncatePreview(strings.TrimSuffix(name, ext), maxMCPFileNameBytes-len(ext)) + ext
	}
	return name
}

// uniqueMCPFileName is name, or name with the first free -N before its extension.
func uniqueMCPFileName(name string, taken map[string]bool) string {
	if !taken[name] {
		return name
	}
	ext := pathpkg.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 2; ; n++ {
		if candidate := fmt.Sprintf("%s-%d%s", stem, n, ext); !taken[candidate] {
			return candidate
		}
	}
}

// mcpPathSegment reduces a host-side name -- a mounted server's, a request id -- to
// one directory name: ASCII letters, digits, '-', '_' and '.', no leading dot.
func mcpPathSegment(s string) string {
	seg := strings.Map(func(r rune) rune {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-_.", r)) {
			return r
		}
		return '_'
	}, s)
	if seg = strings.TrimLeft(seg, "."); seg == "" {
		return "_"
	}
	return seg
}
