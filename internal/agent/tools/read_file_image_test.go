package tools

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

const photoPath = "/workspace/mcp-files/r1/whatsapp/photo.png"

var headBound = regexp.MustCompile(`^head -c (\d+) -- `)

// boxServing answers every bounded read with the first N bytes of data, as `head -c N`
// would inside the box.
func boxServing(t *testing.T, data []byte) *fakeBox {
	t.Helper()
	return &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		m := headBound.FindStringSubmatch(cmd)
		if m == nil {
			t.Errorf("read_file ran %q, want a bounded head -c read", cmd)
			return usersandbox.ExecResult{ExitCode: 1}
		}
		n, _ := strconv.Atoi(m[1])
		return usersandbox.ExecResult{Stdout: data[:min(n, len(data))]}
	}}
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pngHeader is a PNG that declares w x h in a valid IHDR and holds no pixels: all a decode
// bomb needs to reach the pixel cap, and all DecodeConfig reads.
func pngHeader(w, h uint32) []byte {
	chunk := binary.BigEndian.AppendUint32([]byte("IHDR"), w)
	chunk = binary.BigEndian.AppendUint32(chunk, h)
	chunk = append(chunk, 8, 6, 0, 0, 0) // 8-bit RGBA, default compression, filter, no interlace
	out := binary.BigEndian.AppendUint32([]byte("\x89PNG\r\n\x1a\n"), uint32(len(chunk)-4))
	out = append(out, chunk...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
}

// turnCtx is a tool call inside a chat turn: the agent installed the carrier.
func turnCtx(t *testing.T) (context.Context, *llm.ToolMedia) {
	t.Helper()
	ctx, media := llm.WithToolMedia(ctxWith(t, "sess-img", "call-img"))
	return ctx, media
}

func readFile(t *testing.T, ctx context.Context, be *fakeBox, path string) (ToolResult, error) {
	t.Helper()
	return (&ReadFile{Router: routerWith(be)}).Execute(ctx, mustJSON(t, readFileArgs{Path: path}))
}

func TestReadFileAttachesAnImageForTheModel(t *testing.T) {
	raw := testPNG(t, 4, 3)
	ctx, media := turnCtx(t)
	res, err := readFile(t, ctx, boxServing(t, raw), photoPath)
	if err != nil {
		t.Fatalf("read_file on an image: %v", err)
	}
	want := photoPath + " is an image (image/png, 4x3) and is attached below for you to look at."
	if res.Preview != want {
		t.Fatalf("result = %q\nwant     %q", res.Preview, want)
	}
	parts := media.Snapshot()["call-img"]
	if len(parts) != 1 {
		t.Fatalf("media under call-img = %+v, want the one image", media.Snapshot())
	}
	if parts[0].MIMEType != "image/png" || parts[0].Text != photoPath || !bytes.Equal(parts[0].Bytes, raw) {
		t.Fatalf("registered part = {%s %q %d bytes}, want the PNG as read", parts[0].MIMEType, parts[0].Text, len(parts[0].Bytes))
	}
}

func TestReadFileDownscalesALargeImage(t *testing.T) {
	ctx, media := turnCtx(t)
	res, err := readFile(t, ctx, boxServing(t, testPNG(t, 3000, 1500)), photoPath)
	if err != nil {
		t.Fatalf("read_file on a large image: %v", err)
	}
	if !strings.Contains(res.Preview, "(image/png, 3000x1500)") {
		t.Fatalf("result = %q, want the file's own type and size", res.Preview)
	}
	part := media.Snapshot()["call-img"][0]
	cfg, format, err := image.DecodeConfig(bytes.NewReader(part.Bytes))
	if err != nil || part.MIMEType != "image/jpeg" || format != "jpeg" || cfg.Width != 1024 || cfg.Height != 512 {
		t.Fatalf("attached %s %s %dx%d (err %v), want a 1024x512 JPEG", part.MIMEType, format, cfg.Width, cfg.Height, err)
	}
}

// An image larger than the text read cap is still read whole, up to the image cap.
func TestReadFileReadsAnImagePastTheTextCap(t *testing.T) {
	t.Setenv(envFSMaxReadBytes, "64")
	raw := testPNG(t, 40, 40)
	if len(raw) <= 64 {
		t.Fatalf("fixture is %d bytes, want it over the 64-byte text cap", len(raw))
	}
	ctx, media := turnCtx(t)
	be := boxServing(t, raw)
	if _, err := readFile(t, ctx, be, photoPath); err != nil {
		t.Fatalf("read_file on an image over the text cap: %v", err)
	}
	if len(be.execs) != 2 || !strings.HasPrefix(be.execs[1].Command, "head -c "+strconv.Itoa(mcp.MaxFileBytes+1)+" ") {
		t.Fatalf("execs = %+v, want the text-cap read then one read bounded at the image cap", be.execs)
	}
	if got := media.Snapshot()["call-img"]; len(got) != 1 || !bytes.Equal(got[0].Bytes, raw) {
		t.Fatalf("attached %d parts, want the whole image", len(got))
	}
}

func TestReadFileRefusesWhatItCannotShow(t *testing.T) {
	oversized := append(testPNG(t, 2, 2), make([]byte, mcp.MaxFileBytes)...)
	for name, tc := range map[string]struct {
		data    []byte
		carrier bool
		want    string
	}{
		"a PDF":                        {[]byte("%PDF-1.7\n\x00\x01binary"), true, "cannot read binary file"},
		"other binary":                 {[]byte("\x7fELF\x02\x01\x01\x00\x00\x00"), true, "cannot read binary file"},
		"an image with no turn":        {testPNG(t, 2, 2), false, "cannot read binary file"},
		"an image over the cap":        {oversized, true, strconv.Itoa(mcp.MaxFileBytes) + "-byte image cap"},
		"a corrupt image":              {[]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDRtruncated"), true, "does not decode"},
		"an image too large to decode": {pngHeader(10_000, 5_000), true, strconv.Itoa(maxImagePixels) + "-pixel image cap"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := ctxWith(t, "sess-img", "call-img")
			var media *llm.ToolMedia
			if tc.carrier {
				ctx, media = llm.WithToolMedia(ctx)
			}
			_, err := readFile(t, ctx, boxServing(t, tc.data), photoPath)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a refusal containing %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "no vision tool") {
				t.Fatalf("refusal still claims there is no vision tool: %v", err)
			}
			if media.Snapshot() != nil {
				t.Fatalf("a refused read registered media: %+v", media.Snapshot())
			}
		})
	}
}

// The refusal must point the model somewhere true: images open with read_file in a chat
// turn, anything else goes through shell_exec or send_file.
func TestReadFileBinaryRefusalNamesTheWaysOut(t *testing.T) {
	_, err := readFile(t, ctxWith(t, "s", "c"), boxServing(t, testPNG(t, 2, 2)), photoPath)
	if err == nil {
		t.Fatal("an image read with no turn succeeded")
	}
	for _, want := range []string{"read_file", "chat turn", "shell_exec", "send_file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not mention %q", err, want)
		}
	}
}

// Tool batches run in parallel: every image of a batch is downscaled, one at a time, and
// attached under its own call.
func TestReadFileParallelImagesAllAttach(t *testing.T) {
	parent, media := llm.WithToolMedia(context.Background())
	be := boxServing(t, testPNG(t, 1200, 10))
	const calls = 4
	ctxs := make([]context.Context, calls)
	for i := range calls {
		ctxs[i] = WithToolCallContext(parent, "sess-img", "call-"+strconv.Itoa(i), t.TempDir(), testCap)
	}
	var wg sync.WaitGroup
	for _, ctx := range ctxs {
		wg.Go(func() {
			if _, err := readFile(t, ctx, be, photoPath); err != nil {
				t.Errorf("parallel read_file: %v", err)
			}
		})
	}
	wg.Wait()
	got := media.Snapshot()
	for i := range calls {
		parts := got["call-"+strconv.Itoa(i)]
		if len(parts) != 1 || parts[0].MIMEType != "image/jpeg" {
			t.Fatalf("call-%d parts = %d, want one downscaled JPEG", i, len(parts))
		}
	}
}

func TestReadFileRefusesAnImagePastTheTurnLimit(t *testing.T) {
	ctx, media := turnCtx(t)
	for i := range llm.MaxToolMediaPerTurn {
		if err := media.Add("earlier-"+strconv.Itoa(i), llm.ProjectedRequestPart{MIMEType: "image/png", Bytes: []byte{1}}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := readFile(t, ctx, boxServing(t, testPNG(t, 2, 2)), photoPath)
	if !errors.Is(err, llm.ErrToolMediaFull) {
		t.Fatalf("err = %v, want the turn image limit", err)
	}
	if _, ok := media.Snapshot()["call-img"]; ok {
		t.Fatal("an image past the turn limit was registered")
	}
}
