//go:build craft_integration

package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/google/uuid"
	"github.com/moby/moby/client"
)

type existingCraftBox struct {
	craftPreviewSandbox
	handle usersandbox.BoxHandle
}

func (b existingCraftBox) EnsureBox(_ context.Context, identity string) (usersandbox.BoxHandle, error) {
	if identity != b.handle.IdentityID {
		return usersandbox.BoxHandle{}, documents.ErrPreviewUnavailable
	}
	return b.handle, nil
}

func nativeCraftBox(t *testing.T) existingCraftBox {
	t.Helper()
	box := os.Getenv("AURA_CRAFT_PREVIEW_TEST_BOX")
	if box == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("AURA_CRAFT_PREVIEW_TEST_BOX required for native Craft integration")
		}
		t.Skip("set AURA_CRAFT_PREVIEW_TEST_BOX to an existing Craft sandbox")
	}
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	backend := usersandbox.NewDockerBackend(cli, "", usersandbox.Resources{})
	router := usersandbox.NewSandboxRouter(backend, config.SandboxConfig{})
	// Only this test's resolver is overridden: transfer, execution, cancellation and
	// copy-out use production transport; no box is created, reconfigured or destroyed.
	return existingCraftBox{craftPreviewSandbox{router}, usersandbox.BoxHandle{ContainerID: box, IdentityID: "native-preview-test"}}
}

func TestCraftPreviewNative(t *testing.T) {
	box := nativeCraftBox(t)
	p := documents.NewCraftPreviews(box)
	fixtures := filepath.Join("..", "..", "scripts", "fixtures", "document_pipeline_e2e")
	inputs := map[string][]byte{
		"notes.md":   []byte("# Aura preview\n\nFirst page of a native Markdown document."),
		"letter.rtf": []byte(`{\rtf1\ansi Aura native RTF preview\par First page.}`),
		"table.csv":  []byte("Name,Count\nCedar,42\nMaple,17\n"),
	}
	for _, name := range []string{"sample.pdf", "sample.docx", "sample.xlsx", "sample.pptx"} {
		body, err := os.ReadFile(filepath.Join(fixtures, name))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = body
	}
	video, err := os.ReadFile(filepath.Join("testdata", "craft-preview.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	inputs["synthetic.mp4"] = video
	img := image.NewRGBA(image.Rect(0, 0, 960, 480))
	for y := range 480 {
		for x := range 960 {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var photo bytes.Buffer
	if err := png.Encode(&photo, img); err != nil {
		t.Fatal(err)
	}
	inputs["photo.png"] = photo.Bytes()
	if extra := os.Getenv("AURA_CRAFT_PREVIEW_TEST_FIXTURES"); extra != "" {
		entries, err := os.ReadDir(extra)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			body, err := os.ReadFile(filepath.Join(extra, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			inputs[entry.Name()] = body
		}
	}
	for name, body := range inputs {
		t.Run(name, func(t *testing.T) {
			began := time.Now()
			out, err := p.Preview(t.Context(), box.handle.IdentityID, name, func(context.Context) (io.ReadCloser, int64, error) {
				return io.NopCloser(bytes.NewReader(body)), int64(len(body)), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := png.Decode(bytes.NewReader(out))
			if err != nil || decoded.Bounds().Dx() > 480 || decoded.Bounds().Dy() > 480 || len(out) > 2<<20 {
				t.Fatalf("output=%dbytes image=%v err=%v", len(out), decoded, err)
			}
			background := decoded.At(0, 0)
			var differs bool
			for y := 0; y < decoded.Bounds().Dy() && !differs; y++ {
				for x := 0; x < decoded.Bounds().Dx(); x++ {
					if decoded.At(x, y) != background {
						differs = true
						break
					}
				}
			}
			if !differs {
				t.Fatal("native output is blank")
			}
			t.Logf("native %s: %dx%d, %d bytes, %s", name, decoded.Bounds().Dx(), decoded.Bounds().Dy(), len(out), time.Since(began))
		})
	}
	for _, name := range []string{"broken.pdf", "broken.png", "broken.docx", "broken.xlsx", "broken.pptx", "broken.mp4"} {
		// GridCraft deliberately imports plain text under an .xlsx name as CSV.
		// A broken ZIP package exercises corruption rather than that supported import.
		broken := "PK\x03\x04not a valid Office package"
		_, err := p.Preview(t.Context(), box.handle.IdentityID, name, func(context.Context) (io.ReadCloser, int64, error) {
			return io.NopCloser(strings.NewReader(broken)), int64(len(broken)), nil
		})
		if !errors.Is(err, documents.ErrPreviewInvalid) {
			t.Fatalf("%s=%v", name, err)
		}
	}
}

func TestCraftPreviewNativeCancellation(t *testing.T) {
	box := nativeCraftBox(t)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := box.RunPreview(ctx, box.handle, usersandbox.ExecRequest{Command: "sleep 30", Dir: "/workspace"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled native process=%v", err)
	}
	// A finished Wait means the SDK stream and its feeder/copy goroutines joined.
}

func TestCraftPreviewNativeResistantCancellation(t *testing.T) {
	box := nativeCraftBox(t)
	dir := "/workspace/scratch/resistant-preview-" + uuid.NewString()
	pidPath := dir + "/child.pid"
	script := "import os, signal, time\nsignal.signal(signal.SIGTERM, signal.SIG_IGN)\nwith open('" + pidPath + "', 'w') as f: f.write(str(os.getpid()))\nwhile True: time.sleep(0.05)\n"
	if err := box.WriteFile(t.Context(), box.handle, dir+"/resistant.py", []byte(script)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := box.RunPreview(ctx, box.handle, usersandbox.ExecRequest{Command: "python3 " + dir + "/resistant.py", Dir: "/workspace"})
		done <- err
	}()
	pid := 0
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		command := "rm -rf -- " + dir
		if pid > 0 {
			command = "kill -KILL " + strconv.Itoa(pid) + " 2>/dev/null; " + command
		}
		_, _ = box.RunPreview(cleanup, box.handle, usersandbox.ExecRequest{Command: command, Dir: "/workspace"})
	})
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		result, err := box.RunPreview(t.Context(), box.handle, usersandbox.ExecRequest{Command: "cat " + pidPath, Dir: "/workspace"})
		if err == nil {
			pid, err = strconv.Atoi(strings.TrimSpace(string(result)))
			if err == nil && pid > 0 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("resistant child did not become ready")
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation=%v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("cancellation failed to join within its bound")
	}
	state, err := box.RunPreview(t.Context(), box.handle, usersandbox.ExecRequest{Command: "if test -e /proc/" + strconv.Itoa(pid) + "/stat; then cat /proc/" + strconv.Itoa(pid) + "/stat; else echo gone; fi", Dir: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(state)) != "gone" {
		t.Fatalf("preview returned while resistant child still exists after %s: %s", time.Since(start), state)
	}
}
