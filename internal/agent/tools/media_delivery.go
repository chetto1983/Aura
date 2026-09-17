package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mediagen"
	"github.com/chetto1983/aura/internal/redact"
)

// stagedMediaBasename is fixed so no caller-controlled text ever reaches the filesystem.
const stagedMediaBasename = "generated"

// imageExtensions is the closed set of types image generation delivers, not
// mime.ExtensionsByType: that answer depends on the host (Windows lists .jfif first for
// image/jpeg), and the extension must round-trip through guessDeliveryMIME, which types the
// ingested asset. It holds images only, so an image delivery can never stage a clip.
var imageExtensions = map[string]string{
	"image/png":     ".png",
	"image/jpeg":    ".jpg",
	"image/webp":    ".webp",
	"image/svg+xml": ".svg",
}

const uncheckedOptionsNote = "the model catalog is unavailable, so the options were not checked against the model; OpenRouter validates them"

// mediaDeliveredNote tells the model the file is already in front of the user. Measured live on
// 2026-09-16: a bare asset_id read as "the file exists somewhere", and the model spent the rest
// of the turn on tool_search, find /workspace and skill list looking for it to send again.
const mediaDeliveredNote = "Already shown to the user in this chat. Do not send it again or look for the file."

// mediaResult is the model-facing summary of one delivered generation. CostUSD stays
// null when the provider reported no cost: unknown is not free.
type mediaResult struct {
	AssetID     string   `json:"asset_id"`
	MIMEType    string   `json:"mime_type"`
	Model       string   `json:"model"`
	CostUSD     *float64 `json:"cost_usd"`
	Used        any      `json:"used"`
	Adjustments []string `json:"adjustments"`
	Delivered   string   `json:"delivered"`
}

// mediaDeliveryOwner returns the identity a generated file will be delivered to, after
// checking everything staging and ingestForDelivery need. A generation is paid, so a
// delivery that cannot happen must be refused before the provider is called: unlike
// send_file, there is no path-only fallback for a file that exists nowhere else.
func mediaDeliveryOwner(ctx context.Context) (string, bool) {
	owner := identityctx.IdentityID(ctx)
	tc, ok := toolCallCtx(ctx)
	if owner == "" || !ok || tc.sessionID == "" || tc.toolCallID == "" {
		return "", false
	}
	if _, err := mediaRunDir(ctx); err != nil {
		return "", false
	}
	return owner, true
}

func mediaRunDir(ctx context.Context) (string, error) {
	tc, ok := toolCallCtx(ctx)
	if !ok || !filepath.IsAbs(tc.runDir) {
		return "", errors.New("media staging needs an absolute run directory")
	}
	info, err := os.Stat(tc.runDir)
	if err != nil {
		return "", fmt.Errorf("media run directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("media run directory %q is not a directory", tc.runDir)
	}
	return tc.runDir, nil
}

// mediaCatalogEntry finds model in the catalog. An unreadable catalog is treated like a model
// the catalog does not list: no entry, so the request goes out unclamped, and one note, because
// refusing a paid call over a free lookup helps nobody. Any other catalog error is returned.
func mediaCatalogEntry(ctx context.Context, catalog *mediagen.Catalog, baseURL string, kind mediagen.Kind, model string) (*mediagen.Model, []string, error) {
	entry, err := catalog.Find(ctx, baseURL, kind, model)
	switch {
	case errors.Is(err, mediagen.ErrCatalogUnavailable):
		return nil, []string{uncheckedOptionsNote}, nil
	case err != nil:
		return nil, nil, err
	}
	return entry, []string{}, nil
}

// stageImage writes a generated image to a fresh staging directory.
func stageImage(ctx context.Context, data []byte, mimeType string) (path, filename string, err error) {
	ext, ok := imageExtensions[mimeType]
	if !ok {
		return "", "", fmt.Errorf("no staging extension for image type %q", mimeType)
	}
	return stageMedia(ctx, ext, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// stageExistingVideo stages an owned video asset for delivery, read within maxBytes through
// the same owned open as a reference image; the asset is never ingested again.
func stageExistingVideo(ctx context.Context, reader mediagen.ReferenceReader, owner, assetID string, maxBytes int64) (path, filename, mimeType string, size int64, err error) {
	rc, meta, err := mediagen.OpenOwned(ctx, reader, owner, assetID, mediagen.AssetVideo, maxBytes)
	if err != nil {
		return "", "", "", 0, err
	}
	defer func() { _ = rc.Close() }()
	ext, err := mediagen.VideoExtension(meta.MIMEType)
	if err != nil {
		return "", "", "", 0, err
	}
	path, filename, err = stageMedia(ctx, ext, func(w io.Writer) error {
		copied, copyErr := io.Copy(w, rc)
		size = copied
		return copyErr
	})
	if err != nil {
		return "", "", "", 0, err
	}
	return path, filename, meta.MIMEType, size, nil
}

// stageMedia writes a file named with ext to a fresh directory under the run directory's tmp
// tree, the subtree the run-directory sweeper reclaims. The file must outlive Execute:
// channels open the descriptor's path after the turn returns.
func stageMedia(ctx context.Context, ext string, write func(io.Writer) error) (path, filename string, err error) {
	runDir, err := mediaRunDir(ctx)
	if err != nil {
		return "", "", err
	}
	root, err := stagingRoot(ctx)
	if err != nil {
		return "", "", err
	}
	resolvedRoot, inside, err := fenceWithinRoot(runDir, root)
	if err != nil {
		return "", "", err
	}
	if !inside {
		return "", "", fmt.Errorf("media staging root %q escapes the run directory", root)
	}
	dir, err := os.MkdirTemp(resolvedRoot, "media-")
	if err != nil {
		return "", "", err
	}
	filename = stagedMediaBasename + ext
	path = filepath.Join(dir, filename)
	if err := writeStagedMedia(path, write); err != nil {
		discardStagedMedia(path)
		return "", "", err
	}
	return path, filename, nil
}

func writeStagedMedia(path string, write func(io.Writer) error) error {
	// #nosec G304 -- path is a fresh os.MkdirTemp directory joined with a fixed basename.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	writeErr := write(f)
	if closeErr := f.Close(); writeErr == nil {
		writeErr = closeErr
	}
	return writeErr
}

// discardStagedMedia removes the directory stageMedia created for path.
func discardStagedMedia(path string) {
	_ = os.RemoveAll(filepath.Dir(path))
}

// mediaFailedBeforeSubmit answers a plain error. The client codes every failure of the paid
// call itself (a provider refusal, or outcome_unknown), so a plain error can only come from a
// step before it: credentials, settings, the catalog or a reference read.
const mediaFailedBeforeSubmit = "Media generation failed before anything was sent to the provider, so nothing was billed. " +
	"You may try once more; if it fails again, tell the operator."

// mediaErrorResult maps a failed generation to its tool error and logs the cause, which the model
// never sees: any error text can name infrastructure. Measured live on 2026-09-17, when a failure
// answered only "Media generation failed." and left no trace to diagnose it.
func mediaErrorResult(err error) ToolResult {
	code := mediagen.ErrorCode(err)
	message := mediaFailedBeforeSubmit
	if mediaErr, ok := errors.AsType[*mediagen.Error](err); ok {
		message = mediaErr.Message
	}
	attrs := []any{"code", code, "err", redact.String(err.Error())}
	if cause := errors.Unwrap(err); cause != nil {
		attrs = append(attrs, "cause", redact.String(cause.Error()))
	}
	slog.Warn("media generation failed", attrs...)
	return errorResult(code, message)
}

// mediaArtifactResult emits the send_file artifact descriptor for a delivered generation.
// The caption is the original prompt; channels sanitize it for their own limits.
func mediaArtifactResult(ctx context.Context, path, filename, mimeType, assetID, prompt string, size int64, preview mediaResult) ToolResult {
	preview.Delivered = mediaDeliveredNote
	result, ok := mediaPreviewResult(preview)
	if !ok {
		return result
	}
	descriptor := map[string]any{
		"path": path, "filename": filename, "mime_type": mimeType,
		"asset_id": assetID, "caption": prompt,
		"tool_call_id": ToolCallIDFromContext(ctx), "size_bytes": size,
	}
	result.Meta = &ToolResultMeta{"artifact": descriptor}
	return result
}

// mediaPreviewResult encodes a generation summary as the model-facing result; false means it
// could not be encoded and the result is the job_failed error instead.
func mediaPreviewResult(preview any) (ToolResult, bool) {
	encoded, err := json.Marshal(preview)
	if err != nil {
		return errorResult("job_failed", "Cannot encode the generation result."), false
	}
	return ToolResult{Preview: string(encoded), Bytes: len(encoded)}, true
}
