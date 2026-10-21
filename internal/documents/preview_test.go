package documents

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

type previewBox struct {
	owner    string
	files    map[string][]byte
	commands []string
	output   []byte
	archive  []byte
	fail     string
	block    chan struct{}
	entered  chan struct{}
}

func (b *previewBox) EnsureBox(_ context.Context, owner string) (usersandbox.BoxHandle, error) {
	b.owner = owner
	if b.fail == "route" {
		return usersandbox.BoxHandle{}, errors.New("route failed")
	}
	return usersandbox.BoxHandle{IdentityID: owner}, nil
}
func (b *previewBox) WriteFileStream(_ context.Context, h usersandbox.BoxHandle, name string, size int64, body io.Reader) error {
	if h.IdentityID != b.owner {
		return errors.New("wrong owner")
	}
	if b.fail == "source" {
		return errors.New("copy failed")
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return io.ErrUnexpectedEOF
	}
	b.files[name] = data
	return nil
}
func (b *previewBox) WriteFile(_ context.Context, _ usersandbox.BoxHandle, name string, body []byte) error {
	if b.fail == "script" {
		return errors.New("write failed")
	}
	b.files[name] = body
	return nil
}
func (b *previewBox) RunPreview(ctx context.Context, _ usersandbox.BoxHandle, req usersandbox.ExecRequest) ([]byte, error) {
	b.commands = append(b.commands, req.Command)
	if strings.Contains(req.Command, "rm -rf") {
		if b.fail == "cleanup" {
			return nil, errors.New("cleanup failed")
		}
		return nil, nil
	}
	if b.fail == "run" {
		return nil, errors.New("native command failed")
	}
	if b.block != nil && strings.Contains(req.Command, "-cli") {
		close(b.entered)
		select {
		case <-b.block:
		case <-ctx.Done():
			if b.fail == "unconfirmed" {
				return nil, errors.Join(ctx.Err(), usersandbox.ErrExecUnconfirmed)
			}
			return nil, ctx.Err()
		}
	}
	if b.fail == "photo reply" {
		return []byte(`{"id":2,"ok":false,"error":"decode failed"}`), nil
	}
	return []byte(`{"id":2,"ok":true}`), nil
}
func (b *previewBox) CopyArtifactOut(_ context.Context, _ usersandbox.BoxHandle, name string) (io.ReadCloser, error) {
	if b.fail == "output" {
		return nil, errors.New("copy-out failed")
	}
	if b.archive != nil {
		return io.NopCloser(bytes.NewReader(b.archive)), nil
	}
	return io.NopCloser(bytes.NewReader(previewArchive(name[strings.LastIndex(name, "/")+1:], b.output, tar.TypeReg))), nil
}

func previewArchive(name string, data []byte, kind byte) []byte {
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	_ = w.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0600, Typeflag: kind})
	_, _ = w.Write(data)
	_ = w.Close()
	return buf.Bytes()
}
func previewPixels(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func newPreviewBox(t *testing.T) *previewBox {
	return &previewBox{files: map[string][]byte{}, output: previewPixels(t, 480, 240)}
}
func previewSource(body string) PreviewSource {
	return func(context.Context) (io.ReadCloser, int64, error) {
		return io.NopCloser(strings.NewReader(body)), int64(len(body)), nil
	}
}

func TestCraftPreviewNativeFamiliesAndUniqueScratch(t *testing.T) {
	for _, c := range []struct{ name, want string }{{"contract.pdf", "pdfcraft-cli"}, {"letter.docx", "wordcraft-cli convert"}, {"letter.doc", "wordcraft-cli convert"}, {"letter.odt", "wordcraft-cli convert"}, {"letter.rtf", "wordcraft-cli convert"}, {"notes.md", "wordcraft-cli convert"}, {"table.xlsx", "gridcraft-cli run"}, {"table.xls", "gridcraft-cli run"}, {"table.xlsb", "gridcraft-cli run"}, {"table.ods", "gridcraft-cli run"}, {"table.csv", "gridcraft-cli run"}, {"deck.pptx", "deckcraft-cli run"}, {"photo.jpg", "photocraft-cli serve"}, {"photo.PNG", "photocraft-cli serve"}} {
		t.Run(c.name, func(t *testing.T) {
			b := newPreviewBox(t)
			p := NewCraftPreviews(b)
			for range 2 {
				source := "source bytes"
				if c.want == "photocraft-cli serve" {
					source = string(b.output)
				}
				out, err := p.Preview(t.Context(), "owner", c.name, previewSource(source))
				if err != nil || !bytes.Equal(out, b.output) {
					t.Fatalf("preview=%dbytes err=%v", len(out), err)
				}
			}
			joined := strings.Join(b.commands, "\n")
			if b.owner != "owner" || !strings.Contains(joined, c.want) || strings.Count(joined, "rm -rf") != 2 {
				t.Fatalf("owner=%s commands=%s", b.owner, joined)
			}
			if len(b.files) < 4 {
				t.Fatalf("scratch reused: %v", b.files)
			}
			for name, data := range b.files {
				if !strings.HasPrefix(name, "/workspace/scratch/document-preview-") {
					t.Fatalf("unscoped path %s", name)
				}
				if strings.HasSuffix(name, "pdf.json") && (!bytes.Contains(data, []byte(`"max_side":480`)) || !bytes.Contains(data, []byte(`"render_preview"`))) {
					t.Fatalf("unbounded native recipe %s", data)
				}
				if strings.HasSuffix(name, "grid.jsonl") && (!bytes.Contains(data, []byte(`"fitToPage":true`)) || bytes.Contains(data, []byte(`"range"`))) {
					t.Fatalf("sheet crop or missing fit: %s", data)
				}
			}
		})
	}
}

func TestCraftPreviewUnsupportedNeverOpensSource(t *testing.T) {
	for _, name := range []string{"page.html", "image.svg", "deck.ppt", "program.exe", "README", ""} {
		p := NewCraftPreviews(newPreviewBox(t))
		_, err := p.Preview(t.Context(), "owner", name, func(context.Context) (io.ReadCloser, int64, error) {
			t.Fatal("opened unsupported source")
			return nil, 0, nil
		})
		if !errors.Is(err, ErrPreviewUnsupported) {
			t.Fatalf("%q=%v", name, err)
		}
	}
}

func TestCraftPreviewRefusesNativePhotoFailure(t *testing.T) {
	b := newPreviewBox(t)
	b.fail = "photo reply"
	_, err := NewCraftPreviews(b).Preview(t.Context(), "owner", "image.png", previewSource(string(b.output)))
	if !errors.Is(err, ErrPreviewInvalid) {
		t.Fatalf("native PhotoCraft error must not be served as a successful image: %v", err)
	}
}

func TestCraftPreviewClosesSourceAndCleansScratchOnFailures(t *testing.T) {
	for _, failure := range []string{"source", "script", "output", "run", "corrupt", "short", "oversize"} {
		t.Run(failure, func(t *testing.T) {
			b := newPreviewBox(t)
			if failure == "run" {
				b.fail = "script"
			} else {
				b.fail = failure
			}
			if failure == "corrupt" {
				b.output = []byte("<svg>active</svg>")
			}
			closed := false
			p := NewCraftPreviews(b)
			_, err := p.Preview(t.Context(), "owner", "source.pdf", func(context.Context) (io.ReadCloser, int64, error) {
				if failure == "run" {
					b.fail = "run"
				}
				size := int64(3)
				if failure == "short" {
					size = 4
				}
				if failure == "oversize" {
					size = 33 << 20
				}
				return &previewCloser{Reader: strings.NewReader("pdf"), closed: &closed}, size, nil
			})
			if err == nil || !closed || !strings.Contains(strings.Join(b.commands, "\n"), "rm -rf") {
				t.Fatalf("err=%v closed=%v cleanup=%v", err, closed, b.commands)
			}
			if failure == "oversize" && !errors.Is(err, ErrPreviewTooLarge) {
				t.Fatalf("oversized source must be refused before copy-in: %v", err)
			}
		})
	}
}

type previewCloser struct {
	io.Reader
	closed *bool
}

func (r *previewCloser) Close() error { *r.closed = true; return nil }

func TestCraftPreviewBoundsArchiveAndPixels(t *testing.T) {
	bomb := previewPixels(t, 1, 1)
	binary.BigEndian.PutUint32(bomb[16:20], 100000)
	binary.BigEndian.PutUint32(bomb[20:24], 100000)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	corrupt := previewPixels(t, 10, 10)
	corrupt = corrupt[:len(corrupt)-20]
	for name, archive := range map[string][]byte{
		"dimension bomb before allocation": previewArchive("preview.png", bomb, tar.TypeReg),
		"valid header corrupt pixels":      previewArchive("preview.png", corrupt, tar.TypeReg),
		"dimensions":                       previewArchive("preview.png", previewPixels(t, 481, 1), tar.TypeReg),
		"active":                           previewArchive("preview.png", []byte("<html>"), tar.TypeReg),
		"wrong file":                       previewArchive("different.png", previewPixels(t, 1, 1), tar.TypeReg),
		"symlink":                          previewArchive("preview.png", nil, tar.TypeSymlink),
		"too many bytes":                   previewArchive("preview.png", make([]byte, (2<<20)+1), tar.TypeReg),
		"truncated":                        []byte("not a tar"),
	} {
		t.Run(name, func(t *testing.T) {
			b := newPreviewBox(t)
			b.archive = archive
			_, err := NewCraftPreviews(b).Preview(t.Context(), "owner", "x.pdf", previewSource("pdf"))
			if err == nil {
				t.Fatal("unsafe output accepted")
			}
		})
	}
}

func TestCraftPreviewRefusesUnavailableOrFailedSource(t *testing.T) {
	for _, p := range []*CraftPreviews{nil, NewCraftPreviews(nil)} {
		if _, err := p.Preview(t.Context(), "owner", "x.pdf", previewSource("pdf")); !errors.Is(err, ErrPreviewUnavailable) {
			t.Fatalf("nil transport=%v", err)
		}
	}
	b := newPreviewBox(t)
	p := NewCraftPreviews(b)
	if _, err := p.Preview(t.Context(), "", "x.pdf", previewSource("pdf")); !errors.Is(err, ErrPreviewUnavailable) {
		t.Fatalf("empty owner=%v", err)
	}
	b.fail = "route"
	if _, err := p.Preview(t.Context(), "owner", "x.pdf", previewSource("pdf")); err == nil {
		t.Fatal("routing failed open")
	}
	b.fail = ""
	want := errors.New("source missing")
	if _, err := p.Preview(t.Context(), "owner", "x.pdf", func(context.Context) (io.ReadCloser, int64, error) { return nil, 0, want }); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if _, err := p.Preview(t.Context(), "owner", "x.pdf", func(context.Context) (io.ReadCloser, int64, error) {
		return io.NopCloser(strings.NewReader("x")), -1, nil
	}); !errors.Is(err, ErrPreviewInvalid) {
		t.Fatal(err)
	}
	b.fail = "cleanup"
	if out, err := p.Preview(t.Context(), "owner", "x.pdf", previewSource("pdf")); err == nil || out != nil {
		t.Fatalf("failed cleanup=%d bytes %v", len(out), err)
	}
}

func TestCraftPreviewCapacityBeforeSourceAndCancellation(t *testing.T) {
	b := newPreviewBox(t)
	b.block = make(chan struct{})
	b.entered = make(chan struct{})
	p := NewCraftPreviews(b)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := p.Preview(ctx, "one", "x.pdf", previewSource("pdf")); done <- err }()
	<-b.entered
	var opened atomic.Bool
	waitCtx, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stop()
	_, err := p.Preview(waitCtx, "two", "x.pdf", func(context.Context) (io.ReadCloser, int64, error) {
		opened.Store(true)
		return nil, 0, errors.New("must not open")
	})
	if !errors.Is(err, context.DeadlineExceeded) || opened.Load() {
		t.Fatalf("capacity err=%v opened=%v", err, opened.Load())
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("conversion cancel=%v", err)
	}
	if !strings.Contains(strings.Join(b.commands, "\n"), "rm -rf") {
		t.Fatal("cancel left scratch")
	}
	ctx2, cancel2 := context.WithCancel(t.Context())
	cancel2()
	_, err = p.Preview(ctx2, "one", "x.pdf", previewSource("pdf"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("already cancelled=%v", err)
	}
}

func TestCraftPreviewUnconfirmedExitClosesSourceAndRetainsAdmission(t *testing.T) {
	b := newPreviewBox(t)
	b.block = make(chan struct{})
	b.entered = make(chan struct{})
	b.fail = "unconfirmed"
	p := NewCraftPreviews(b)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closed := false
	first := make(chan error, 1)
	go func() {
		_, err := p.Preview(ctx, "one", "x.pdf", func(context.Context) (io.ReadCloser, int64, error) {
			return &previewCloser{Reader: strings.NewReader("pdf"), closed: &closed}, 3, nil
		})
		first <- err
	}()
	<-b.entered
	var opened atomic.Bool
	deniedSource := func(context.Context) (io.ReadCloser, int64, error) {
		opened.Store(true)
		return nil, 0, errors.New("must not open another source")
	}
	queued := make(chan error, 1)
	go func() { _, err := p.Preview(t.Context(), "two", "x.pdf", deniedSource); queued <- err }()
	cancel()
	if err := <-first; !errors.Is(err, usersandbox.ErrExecUnconfirmed) || !closed {
		t.Fatalf("unconfirmed=%v source closed=%v", err, closed)
	}
	select {
	case err := <-queued:
		if !errors.Is(err, ErrPreviewUnavailable) {
			t.Fatalf("queued=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued request waited instead of failing unavailable")
	}
	_, err := p.Preview(t.Context(), "three", "x.pdf", deniedSource)
	if !errors.Is(err, ErrPreviewUnavailable) || opened.Load() {
		t.Fatalf("new request=%v opened=%v", err, opened.Load())
	}
	if strings.Contains(strings.Join(b.commands, "\n"), "rm -rf") {
		t.Fatal("scratch was removed before execution exit could be confirmed")
	}
}
