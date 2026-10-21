package documents

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"path"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/google/uuid"
)

const (
	previewInputLimit  = 32 << 20
	previewOutputLimit = 2 << 20
	previewEdge        = 480
	previewTimeout     = 30 * time.Second
)

// Preview errors distinguish an unavailable engine, unsupported input, invalid bytes and caps.
var (
	ErrPreviewUnavailable = errors.New("native preview unavailable")
	ErrPreviewUnsupported = errors.New("unsupported preview format")
	ErrPreviewInvalid     = errors.New("invalid preview")
	ErrPreviewTooLarge    = errors.New("preview exceeds limit")
)

// PreviewSource opens an authorized object's current stream and authoritative byte size.
type PreviewSource func(context.Context) (io.ReadCloser, int64, error)

// PreviewSandbox is existing sandbox transport plus a streamed native command execution.
// RunPreview must cancel and join the process on ctx cancellation, bounding captured output.
type PreviewSandbox interface {
	EnsureBox(context.Context, string) (usersandbox.BoxHandle, error)
	WriteFileStream(context.Context, usersandbox.BoxHandle, string, int64, io.Reader) error
	WriteFile(context.Context, usersandbox.BoxHandle, string, []byte) error
	RunPreview(context.Context, usersandbox.BoxHandle, usersandbox.ExecRequest) ([]byte, error)
	CopyArtifactOut(context.Context, usersandbox.BoxHandle, string) (io.ReadCloser, error)
}

// CraftPreviews orchestrates native Craft apps without sharing their editor sessions or outputs.
type CraftPreviews struct {
	box         PreviewSandbox
	slot        chan struct{}
	unavailable chan struct{}
}

// NewCraftPreviews limits thumbnail work to one conversion, including source materialization.
func NewCraftPreviews(box PreviewSandbox) *CraftPreviews {
	return &CraftPreviews{box: box, slot: make(chan struct{}, 1), unavailable: make(chan struct{})}
}

// Preview streams one original into a fresh tenant scratch directory and returns a bounded PNG.
func (p *CraftPreviews) Preview(ctx context.Context, identityID, fileName string, open PreviewSource) (out []byte, err error) {
	kind, extension := previewKind(fileName)
	if kind == "" {
		return nil, ErrPreviewUnsupported
	}
	if p == nil || p.box == nil {
		return nil, ErrPreviewUnavailable
	}
	if strings.TrimSpace(identityID) == "" {
		return nil, ErrPreviewUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, previewTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-p.unavailable:
		return nil, ErrPreviewUnavailable
	case p.slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() {
		if errors.Is(err, usersandbox.ErrExecUnconfirmed) {
			close(p.unavailable)
			return
		}
		<-p.slot
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h, err := p.box.EnsureBox(ctx, identityID)
	if err != nil {
		return nil, err
	}
	if h.IdentityID != identityID {
		return nil, ErrPreviewUnavailable
	}
	dir := "/workspace/scratch/document-preview-" + uuid.NewString()
	defer func() {
		if errors.Is(err, usersandbox.ErrExecUnconfirmed) {
			return
		}
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		if _, cleanupErr := p.box.RunPreview(cleanup, h, usersandbox.ExecRequest{Command: "rm -rf -- " + dir, Dir: "/workspace"}); cleanupErr != nil {
			out = nil
			err = errors.Join(err, fmt.Errorf("preview scratch cleanup: %w", cleanupErr))
		}
	}()
	body, size, err := open(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, body.Close())
		if err != nil {
			out = nil
		}
	}()
	if size < 0 {
		return nil, ErrPreviewInvalid
	}
	if size > previewInputLimit {
		return nil, ErrPreviewTooLarge
	}
	if _, err := p.box.RunPreview(ctx, h, usersandbox.ExecRequest{Command: "mkdir -m 700 -p " + dir, Dir: "/workspace"}); err != nil {
		return nil, err
	}
	source := path.Join(dir, "source"+extension)
	original := io.LimitReader(body, size+1)
	if kind == "photo" {
		original, err = screenPreviewRaster(original)
		if err != nil {
			return nil, err
		}
	}
	if err := p.box.WriteFileStream(ctx, h, source, size, original); err != nil {
		return nil, err
	}
	if err := p.renderPreview(ctx, h, kind, dir, source); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	archive, err := p.box.CopyArtifactOut(ctx, h, path.Join(dir, "preview.png"))
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, archive.Close())
		if err != nil {
			out = nil
		}
	}()
	return readPreview(archive)
}

func (p *CraftPreviews) renderPreview(ctx context.Context, h usersandbox.BoxHandle, kind, dir, source string) error {
	if kind == "video" {
		return p.renderVideoPreview(ctx, h, dir, source)
	}
	command, files := previewRecipe(kind, dir, source)
	for name, body := range files {
		if err := p.box.WriteFile(ctx, h, path.Join(dir, name), body); err != nil {
			return err
		}
	}
	// Paths contain only a generated UUID directory and literal basenames; source names
	// and request parameters never enter shell syntax. The file cap bounds native exports.
	reply, err := p.box.RunPreview(ctx, h, usersandbox.ExecRequest{Command: "ulimit -f 131072 && " + command, Dir: dir})
	if err != nil {
		return err
	}
	if kind == "photo" && !successfulPhotoPreview(reply) {
		return ErrPreviewInvalid
	}
	return nil
}

func successfulPhotoPreview(reply []byte) bool {
	for line := range bytes.SplitSeq(reply, []byte("\n")) {
		var result struct {
			ID int  `json:"id"`
			OK bool `json:"ok"`
		}
		if json.Unmarshal(line, &result) == nil && result.ID == 2 {
			return result.OK
		}
	}
	return false
}

func previewKind(name string) (kind, extension string) {
	extension = strings.ToLower(path.Ext(name))
	switch extension {
	case ".pdf":
		kind = "pdf"
	case ".docx", ".docm", ".dotx", ".dotm", ".doc", ".dot", ".odt", ".rtf", ".txt", ".md", ".markdown":
		kind = "word"
	case ".xlsx", ".xlsm", ".xltx", ".xltm", ".xls", ".xlt", ".xlsb", ".ods", ".csv", ".tsv":
		kind = "grid"
	case ".pptx", ".potx", ".ppsx":
		kind = "deck"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff":
		kind = "photo"
	case ".mp4", ".m4v", ".mov", ".webm", ".mkv", ".avi", ".mpeg", ".mpg", ".ogv":
		kind = "video"
	}
	return
}

func readPreview(r io.Reader) ([]byte, error) {
	tr := tar.NewReader(io.LimitReader(r, previewOutputLimit+(64<<10)))
	header, err := tr.Next()
	if err != nil {
		return nil, fmt.Errorf("%w: archive: %v", ErrPreviewInvalid, err)
	}
	if header.Name != "preview.png" || header.Typeflag != tar.TypeReg || header.Size < 1 {
		return nil, ErrPreviewInvalid
	}
	if header.Size > previewOutputLimit {
		return nil, ErrPreviewTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(tr, previewOutputLimit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: output: %v", ErrPreviewInvalid, err)
	}
	if int64(len(body)) != header.Size {
		return nil, ErrPreviewInvalid
	}
	if _, err := tr.Next(); !errors.Is(err, io.EOF) {
		return nil, ErrPreviewInvalid
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: PNG header", ErrPreviewInvalid)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > previewEdge || cfg.Height > previewEdge {
		return nil, ErrPreviewTooLarge
	}
	if _, err := png.Decode(bytes.NewReader(body)); err != nil {
		return nil, fmt.Errorf("%w: PNG pixels", ErrPreviewInvalid)
	}
	return body, nil
}
