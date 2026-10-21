package documents

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

type filmPreviewBox struct {
	*previewBox
	probe, imported string
}

func (b *filmPreviewBox) RunPreview(ctx context.Context, h usersandbox.BoxHandle, req usersandbox.ExecRequest) ([]byte, error) {
	if strings.Contains(req.Command, "filmcraft-cli") {
		b.commands = append(b.commands, req.Command)
		if strings.Contains(req.Command, " probe ") {
			return []byte(b.probe), nil
		}
		if strings.Contains(req.Command, " import ") {
			return []byte(b.imported), nil
		}
		return []byte(`{}`), nil
	}
	return b.previewBox.RunPreview(ctx, h, req)
}

func TestCraftPreviewVideoUsesNativeMetadataAndReturnedItem(t *testing.T) {
	b := &filmPreviewBox{newPreviewBox(t), `{"video":{"width":1920,"height":1080},"duration":13208832000000}`, `{"errors":[],"items":[17]}`}
	out, err := NewCraftPreviews(b).Preview(t.Context(), "owner", "clip.mp4", previewSource("video bytes"))
	if err != nil || len(out) == 0 {
		t.Fatalf("video=%dbytes %v", len(out), err)
	}
	joined := strings.Join(b.commands, "\n")
	for _, part := range []string{"filmcraft-cli --data-dir ", " probe ", " import ", `"fromItem":17`, " render --seconds 5 --scale 0.25"} {
		if !strings.Contains(joined, part) {
			t.Fatalf("missing native step %q: %s", part, joined)
		}
	}
	if strings.Contains(joined, "--allow-offline") {
		t.Fatal("offline placeholder accepted")
	}
}

func TestCraftPreviewVideoRejectsUnsafeMetadataBeforeImport(t *testing.T) {
	for _, probe := range []string{`not JSON`, `{"video":{"width":0,"height":1080},"duration":100}`, `{"video":{"width":100000,"height":100000},"duration":100}`, `{"video":{"width":1920,"height":1080},"duration":-1}`, `{"duration":100}`} {
		b := &filmPreviewBox{newPreviewBox(t), probe, `{"errors":[],"items":[17]}`}
		_, err := NewCraftPreviews(b).Preview(t.Context(), "owner", "clip.mp4", previewSource("video"))
		if err == nil {
			t.Fatalf("unsafe metadata accepted: %s", probe)
		}
		if strings.Contains(strings.Join(b.commands, "\n"), " import ") {
			t.Fatal("unsafe dimensions reached import")
		}
	}
}

func TestCraftPreviewVideoRefusesFailedOrMissingImport(t *testing.T) {
	for _, imported := range []string{`not JSON`, `{"errors":["unsupported codec"],"items":[17]}`, `{"errors":[],"items":[]}`, `{"items":["untrusted;command"]}`} {
		b := &filmPreviewBox{newPreviewBox(t), `{"video":{"width":1920,"height":1080},"duration":254016000000}`, imported}
		_, err := NewCraftPreviews(b).Preview(t.Context(), "owner", "clip.mp4", previewSource("video"))
		if !errors.Is(err, ErrPreviewInvalid) {
			t.Fatalf("failed import=%v", err)
		}
		if strings.Contains(strings.Join(b.commands, "\n"), "file.newSequence") {
			t.Fatal("sequence created from invalid import")
		}
	}
}
