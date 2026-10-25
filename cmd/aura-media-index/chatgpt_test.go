package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/chatgptplan"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/settings"
)

const (
	testIdentity = "a696df2b-b7bc-4ee7-870b-15d2cced1839"
	testSecret   = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
)

// readerFunc stands in for the ChatGPT client reading an image.
type readerFunc func(ctx context.Context, image []byte, mimeType, prompt string) (string, error)

func (f readerFunc) Describe(ctx context.Context, image []byte, mimeType, prompt string) (string, error) {
	return f(ctx, image, mimeType, prompt)
}

func chatGPTConfig(skillsDir string) *config.Config {
	return &config.Config{
		LLM:           llm.Config{Provider: llm.ChatGPTProvider, Model: "gpt-6.1-sol", BaseURL: llm.ChatGPTBaseURL},
		SkillsDir:     skillsDir,
		AuthulaSecret: testSecret,
	}
}

func identityEnv(id string) func(string) string {
	return func(name string) string {
		if name == "AURA_INGEST_IDENTITY_ID" {
			return id
		}
		return ""
	}
}

func TestChatGPTRouteIsNilOffChatGPT(t *testing.T) {
	cfg := &config.Config{LLM: llm.Config{Provider: "ollama", Model: "gemma4:31b-cloud"}}
	ctx := t.Context()
	got, reader, err := chatGPTRoute(ctx, cfg, identityEnv(testIdentity))
	if err != nil || reader != nil || got != ctx {
		t.Fatalf("chatGPTRoute off ChatGPT = %v, %v, %v; want the same context, no reader", got, reader, err)
	}
}

func TestChatGPTRouteNeedsTheIngestIdentity(t *testing.T) {
	_, _, err := chatGPTRoute(t.Context(), chatGPTConfig(t.TempDir()+"/skills"), identityEnv(""))
	if err == nil || !strings.Contains(err.Error(), "AURA_INGEST_IDENTITY_ID") {
		t.Fatalf("chatGPTRoute without the identity = %v, want an error naming the variable", err)
	}
}

func TestChatGPTRouteOpensTheStoreForTheIdentity(t *testing.T) {
	root := t.TempDir()
	if _, err := chatgptplan.New(root, testSecret); err != nil {
		t.Fatal(err)
	}
	ctx, reader, err := chatGPTRoute(t.Context(), chatGPTConfig(filepath.Join(root, "skills")), identityEnv(" "+testIdentity+" "))
	if err != nil || reader == nil {
		t.Fatalf("chatGPTRoute = %v, %v; want a reader", reader, err)
	}
	if got := identityctx.IdentityID(ctx); got != testIdentity {
		t.Fatalf("identity in context = %q, want %q", got, testIdentity)
	}
}

func chatGPTRunEnv(t *testing.T, identity string) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("AURA_DB_URL", "")
	t.Setenv("AURA_LLM_PROVIDER", llm.ChatGPTProvider)
	t.Setenv("AURA_LLM_MODEL", "gpt-6.1-sol")
	t.Setenv("AURA_LLM_BASE_URL", llm.ChatGPTBaseURL)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("MULTIMODAL_BASE_URL", "")
	t.Setenv("MULTIMODAL_MODEL", "")
	t.Setenv("AURA_AUTHULA_SECRET", testSecret)
	t.Setenv("AURA_SKILLS_DIR", filepath.Join(t.TempDir(), "skills"))
	t.Setenv("AURA_INGEST_IDENTITY_ID", identity)
}

func runOnImage(t *testing.T) (int, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scan.png")
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{"-kind", "image", "-name", "scan.png", path}, &stdout, &stderr)
	return code, stderr.String()
}

func TestRunFailsWhenTheChatGPTStoreDoesNotOpen(t *testing.T) {
	chatGPTRunEnv(t, testIdentity)
	code, stderr := runOnImage(t)
	if code != 1 || !strings.Contains(stderr, "no credential store here") {
		t.Fatalf("exit = %d (stderr %q), want 1 naming the missing store", code, stderr)
	}
}

func TestRunFailsOnAChatGPTRouteWithoutTheIngestIdentity(t *testing.T) {
	chatGPTRunEnv(t, "")
	code, stderr := runOnImage(t)
	if code != 1 || !strings.Contains(stderr, "AURA_INGEST_IDENTITY_ID") {
		t.Fatalf("exit = %d (stderr %q), want 1 naming the identity", code, stderr)
	}
}

func TestMediaRuntimeReadsWithTheReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cockpit.png")
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := readerFunc(func(context.Context, []byte, string, string) (string, error) { return "Pilot workspace", nil })
	runtime := newMediaRuntime(chatGPTConfig(""), settings.Credentials{}, nil, false, reader)
	text, err := runtime.derive(t.Context(), mediaKindImage, path, "cockpit.png")
	if err != nil || text != "Pilot workspace" {
		t.Fatalf("derive = %q, %v", text, err)
	}
}

func TestMediaRuntimeIsNoRouteWhenTheReaderCannotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cockpit.png")
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := readerFunc(func(context.Context, []byte, string, string) (string, error) {
		return "", fmt.Errorf("%w: chatgpt: gpt-6.1-sol does not read images", llm.ErrMandatoryModalityUnavailable)
	})
	runtime := newMediaRuntime(chatGPTConfig(""), settings.Credentials{}, nil, false, reader)
	if _, err := runtime.derive(t.Context(), mediaKindImage, path, "cockpit.png"); !errors.Is(err, multimodal.ErrNoVisionRoute) {
		t.Fatalf("derive err = %v, want ErrNoVisionRoute", err)
	}
}

func TestMediaRuntimeReadsScannedPagesInOrderWithTheReader(t *testing.T) {
	pdf := filepath.Join(t.TempDir(), "verbale.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-image-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	read := 0
	reader := readerFunc(func(context.Context, []byte, string, string) (string, error) {
		read++
		return fmt.Sprintf("page-%d", read), nil
	})
	runtime := newMediaRuntime(chatGPTConfig(""), settings.Credentials{}, nil, false, reader)
	runtime.renderPDF = func(_ context.Context, _ string, outDir string) ([]string, error) {
		pages := make([]string, 3)
		for i := range pages {
			pages[i] = filepath.Join(outDir, fmt.Sprintf("page-%d.png", i+1))
			if err := os.WriteFile(pages[i], []byte("image"), 0o600); err != nil {
				return nil, err
			}
		}
		return pages, nil
	}
	text, err := runtime.derive(t.Context(), mediaKindPDF, pdf, "verbale.pdf")
	want := "PDF page 1:\npage-1\n\nPDF page 2:\npage-2\n\nPDF page 3:\npage-3"
	if err != nil || text != want {
		t.Fatalf("derive = %q, %v; want %q", text, err, want)
	}
}
