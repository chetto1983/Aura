package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// SendFile is the channel-agnostic artifact delivery tool (D-05): the model calls
// it with an absolute path (and an optional caption) when IT decides to deliver a
// file to the user. There is NO renderer auto-detect — delivery is an explicit
// agent action. The tool reads the path, gates the size, and emits a generic
// artifact descriptor on the ToolResult's Meta; the agent loop lifts that onto
// Actions.ArtifactDelta (toolResultEvent), and the AG-UI translator maps it to a
// custom event each channel renders its own way (D-06). The substrate never names
// any channel — SendFile is delivery-mechanism unaware.
//
// It is Deferred (path/caption schema + an inline example, the deferred-tool rule)
// and NON-Mutating (it reads a file and describes a delivery — no host state
// changes), so it never arms the completion-gate critic.
type SendFile struct {
	// WorkspaceRoot, when set, fences delivery to files whose real path stays
	// inside this workspace. Empty preserves legacy unrestricted delivery in a CLI
	// context (the operator drives the agent directly).
	WorkspaceRoot string
	// RequireWorkspace makes the fence fail CLOSED when WorkspaceRoot is empty
	// (AG-019): in a non-CLI context (a channel-driven agent) an unset workspace
	// must NOT silently grant unrestricted host-file delivery. The composition root
	// sets it true for non-CLI runners; CLI keeps it false (legacy behavior).
	RequireWorkspace bool
}

// maxSendFileBytes is the upload ceiling enforced before a descriptor is emitted
// (the Bot-API document upload limit). A file over the cap returns a
// file_too_large error result the agent surfaces — NEVER a silent truncation
// (OQ3 / T-13-02-Artifact). It stays a generic "upload limit" — not channel-named.
const maxSendFileBytes = 50 << 20 // 50 MiB

type sendFileArgs struct {
	Path    string `json:"path"`
	Caption string `json:"caption"`
}

func (s *SendFile) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Absolute path to a readable file on the host to deliver to the user, e.g. \"/abs/results.xlsx\"."},
    "caption": {"type": "string", "description": "Optional short caption shown alongside the file."}
  },
  "required": ["path"]
}`)
	return Spec{
		Name:    "send_file",
		Summary: "Deliver a file from the host to the user.",
		Description: "Deliver a file you produced or found on the host directly to the user as an attachment. " +
			"Use it when the user should receive an actual file (a spreadsheet, a PDF, an image, a generated report) rather than its contents pasted into the chat. " +
			"Give the absolute path to the file and an optional short caption. " +
			"The file is read and queued for delivery; you do not need to know how the user's channel renders it. " +
			"A file larger than the 50 MB upload limit, or a path that cannot be read, comes back as a small {error,message} object you should read and adapt to — it is not a tool failure. " +
			"Example: {\"path\":\"/abs/file.xlsx\",\"caption\":\"results\"}.",
		Parameters: params,
		Deferred:   true,
		// Reads a file and describes a delivery — no host state mutation (D-43).
		Mutating: false,
	}
}

// Execute reads the path, gates the size, and on success sets res.Meta with a
// channel-agnostic artifact descriptor {path, filename, caption}. The tool CANNOT
// stamp the Event itself (the inbound tool-call ctx is read-only); the agent
// loop's toolResultEvent lifts the Meta artifact onto Actions.ArtifactDelta. A
// too-large or unreadable path returns an inline error result (NOT a Go error)
// the model self-corrects on, with NO artifact Meta.
func (s *SendFile) Execute(_ context.Context, raw json.RawMessage) (ToolResult, error) {
	var a sendFileArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("send_file args: %w", err)
	}
	path := strings.TrimSpace(a.Path)
	if path == "" {
		return errorResult("file_unreadable", "no path was provided; pass the absolute path of the file to deliver"), nil
	}

	if strings.TrimSpace(s.WorkspaceRoot) == "" && s.RequireWorkspace {
		return errorResult("workspace_not_configured",
			"file delivery is disabled: no workspace is configured in this context, so host-file delivery is fenced off (AG-019). Ask the operator to configure a workspace root or deliver via an approved path."), nil
	}
	resolved, ok, ferr := s.checkWorkspace(path)
	if ferr != nil {
		return errorResult("file_unreadable", fmt.Sprintf("cannot resolve %q: %v", path, ferr)), nil
	}
	if !ok {
		return outsideWorkspaceResult(resolved, s.WorkspaceRoot), nil
	}
	path = resolved

	info, err := os.Stat(path)
	if err != nil {
		return errorResult("file_unreadable", fmt.Sprintf("cannot read %q: %v", path, err)), nil
	}
	if info.IsDir() {
		return errorResult("file_unreadable", fmt.Sprintf("%q is a directory, not a file", path)), nil
	}
	if info.Size() > maxSendFileBytes {
		return errorResult("file_too_large", fmt.Sprintf(
			"%q is %d bytes; the upload limit is %d bytes (50 MB). Deliver a smaller file or a link instead",
			path, info.Size(), int64(maxSendFileBytes))), nil
	}

	descriptor := map[string]any{
		"path":     path,
		"filename": filepath.Base(path),
		"caption":  asciiCaption(a.Caption),
	}
	meta := ToolResultMeta{"artifact": descriptor}
	preview := fmt.Sprintf("queued %s for delivery", filepath.Base(path))
	return ToolResult{
		Preview: preview,
		Bytes:   len(preview),
		Meta:    &meta,
	}, nil
}

func (s *SendFile) checkWorkspace(path string) (string, bool, error) {
	root := strings.TrimSpace(s.WorkspaceRoot)
	if root == "" {
		if s.RequireWorkspace {
			// Fail closed (AG-019): a non-CLI context without a configured workspace
			// must not deliver arbitrary host files.
			return path, false, nil
		}
		return path, true, nil
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", false, fmt.Errorf("resolve workspace root: %w", err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", false, fmt.Errorf("resolve workspace root symlinks: %w", err)
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", false, fmt.Errorf("resolve path: %w", err)
	}
	pathReal, err := filepath.EvalSymlinks(pathAbs)
	if err != nil {
		return "", false, err
	}
	rel, err := filepath.Rel(rootReal, pathReal)
	if err != nil {
		return pathReal, false, nil
	}
	if rel == "." || (!filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
		return pathReal, true, nil
	}
	return pathReal, false, nil
}

func outsideWorkspaceResult(path, root string) ToolResult {
	return errorResult("outside_workspace_requires_approval",
		fmt.Sprintf("%q is outside workspace %q. Ask the user for approval with ask_user(kind=approval, resume_context={\"type\":\"send_file_outside_workspace\",\"path\":%q}), then deliver a workspace copy or use an operator-approved path.", path, root, path))
}

// errorResult builds the inline {error,message} object the model self-corrects on
// (mirrors web_fetch's sanitized error shape). It carries NO Meta — an error
// never produces an artifact descriptor.
func errorResult(code, message string) ToolResult {
	b, err := json.Marshal(map[string]string{"error": code, "message": message})
	if err != nil { // map[string]string never fails to marshal; defensive only.
		b = []byte(`{"error":"` + code + `"}`)
	}
	return ToolResult{Preview: string(b), Bytes: len(b)}
}

// asciiCaption renders a caption ASCII-safe: a non-ASCII rune is transliterated to
// its closest ASCII form where trivially possible (accented Latin → base letter),
// otherwise dropped. This avoids the document/voice-caption 400 a non-ASCII byte
// triggers on the Bot API (Pitfall 4 / T-13-02-CaptionInject) without coupling the
// substrate to any channel. The result contains only bytes ≤ 0x7F.
func asciiCaption(caption string) string {
	var b strings.Builder
	b.Grow(len(caption))
	for _, r := range caption {
		switch {
		case r <= unicode.MaxASCII:
			b.WriteRune(r)
		default:
			if base := foldToASCII(r); base != 0 {
				b.WriteByte(base)
			}
			// Otherwise the rune is dropped (best-effort, never a 400).
		}
	}
	return strings.TrimSpace(b.String())
}

// foldToASCII maps a common accented Latin rune to its base ASCII letter, or 0
// when there is no trivial single-byte equivalent (the rune is then dropped). It
// covers the Latin-1/Latin-Extended-A vowels and consonants the model is most
// likely to emit in an Italian/European caption; it is deliberately small, not a
// full Unicode transliteration table.
func foldToASCII(r rune) byte {
	switch r {
	case 'à', 'á', 'â', 'ã', 'ä', 'å', 'ā', 'ă', 'ą':
		return 'a'
	case 'è', 'é', 'ê', 'ë', 'ē', 'ĕ', 'ė', 'ę', 'ě':
		return 'e'
	case 'ì', 'í', 'î', 'ï', 'ī', 'ĭ', 'į':
		return 'i'
	case 'ò', 'ó', 'ô', 'õ', 'ö', 'ø', 'ō', 'ŏ', 'ő':
		return 'o'
	case 'ù', 'ú', 'û', 'ü', 'ū', 'ŭ', 'ů', 'ű':
		return 'u'
	case 'ç', 'ć', 'č', 'ĉ', 'ċ':
		return 'c'
	case 'ñ', 'ń', 'ň':
		return 'n'
	case 'ÿ', 'ý':
		return 'y'
	case 'À', 'Á', 'Â', 'Ã', 'Ä', 'Å', 'Ā', 'Ă', 'Ą':
		return 'A'
	case 'È', 'É', 'Ê', 'Ë', 'Ē', 'Ĕ', 'Ė', 'Ę', 'Ě':
		return 'E'
	case 'Ì', 'Í', 'Î', 'Ï', 'Ī', 'Ĭ', 'Į':
		return 'I'
	case 'Ò', 'Ó', 'Ô', 'Õ', 'Ö', 'Ø', 'Ō', 'Ŏ', 'Ő':
		return 'O'
	case 'Ù', 'Ú', 'Û', 'Ü', 'Ū', 'Ŭ', 'Ů', 'Ű':
		return 'U'
	case 'Ç', 'Ć', 'Č', 'Ĉ', 'Ċ':
		return 'C'
	case 'Ñ', 'Ń', 'Ň':
		return 'N'
	default:
		return 0
	}
}
