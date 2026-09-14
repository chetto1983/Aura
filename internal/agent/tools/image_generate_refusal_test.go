package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mediagen"
)

// assertNothingSpent proves a refusal happened before any provider request, reference
// read or delivery: generation is paid, so every refusal that can be decided locally
// must be decided before the first request leaves.
func assertNothingSpent(t *testing.T, f *imageFixture) {
	t.Helper()
	if seen := f.provider.seen(); len(seen) != 0 {
		t.Fatalf("provider saw %v, want no request", seen)
	}
	if len(f.references.opened) != 0 || f.deliverer.calls != 0 {
		t.Fatalf("opened %v references and delivered %d times, want neither", f.references.opened, f.deliverer.calls)
	}
	if dirs := stagedMediaDirs(t, f.runDir); len(dirs) != 0 {
		t.Fatalf("staged %v, want nothing", dirs)
	}
}

func TestImageGenerateRefusesMalformedInvocations(t *testing.T) {
	for name, args := range map[string]string{
		"not JSON":          `{"prompt":`,
		"not an object":     `["a picture"]`,
		"agent picks model": `{"prompt":"a picture","model":"openai/gpt-image-2"}`,
		"blank prompt":      `{"prompt":"   "}`,
		"missing prompt":    `{"aspect_ratio":"1:1"}`,
		"trailing value":    `{"prompt":"a picture"} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newImageFixture(t)
			code, message := toolError(t, f.execute(t, args))
			if code != "unsupported" || !strings.Contains(message, "prompt") {
				t.Fatalf("%s -> %q %q, want an actionable unsupported naming prompt", name, code, message)
			}
			if f.credentials.calls != 0 {
				t.Fatal("a malformed invocation still resolved credentials")
			}
			assertNothingSpent(t, f)
		})
	}
}

func TestImageGenerateRefusesWhenADependencyIsMissing(t *testing.T) {
	for name, strip := range map[string]func(*ImageGenerate){
		"credentials":     func(g *ImageGenerate) { g.Credentials = nil },
		"settings":        func(g *ImageGenerate) { g.Settings = nil },
		"catalog":         func(g *ImageGenerate) { g.Catalog = nil },
		"client":          func(g *ImageGenerate) { g.Client = nil },
		"references":      func(g *ImageGenerate) { g.References = nil },
		"assets":          func(g *ImageGenerate) { g.Assets = nil },
		"max image bytes": func(g *ImageGenerate) { g.MaxImageBytes = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			f := newImageFixture(t)
			strip(f.tool)
			if code, message := toolError(t, f.execute(t, `{"prompt":"a picture"}`)); code != "unsupported" || message == "" {
				t.Fatalf("missing %s -> %q %q, want unsupported with a message", name, code, message)
			}
			if f.credentials.calls != 0 {
				t.Fatal("an unconfigured tool still resolved credentials")
			}
			assertNothingSpent(t, f)
		})
	}

	res, err := (&ImageGenerate{}).Execute(mediaCtx(t, t.TempDir()), json.RawMessage(`{"prompt":"a picture"}`))
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := toolError(t, res); code != "unsupported" {
		t.Fatalf("the registry's zero-value tool -> %q, want unsupported", code)
	}
}

func TestImageGenerateRefusesWithoutADeliverableConversation(t *testing.T) {
	runDir := t.TempDir()
	owned := identityctx.WithIdentityID(context.Background(), "owner-1")
	for name, ctx := range map[string]context.Context{
		"no identity":  WithToolCallContext(context.Background(), "thread", "call-image", runDir, 8192),
		"no thread":    WithToolCallContext(owned, "", "call-image", runDir, 8192),
		"no tool call": WithToolCallContext(owned, "thread", "", runDir, 8192),
		"no run dir":   WithToolCallContext(owned, "thread", "call-image", filepath.Join(runDir, "gone"), 8192),
	} {
		t.Run(name, func(t *testing.T) {
			f := newImageFixture(t)
			f.ctx = ctx
			if code, _ := toolError(t, f.execute(t, `{"prompt":"a picture"}`)); code != "unsupported" {
				t.Fatalf("%s -> %q, want unsupported", name, code)
			}
			if f.credentials.calls != 0 {
				t.Fatal("an undeliverable call still resolved credentials")
			}
			assertNothingSpent(t, f)
		})
	}
}

func TestImageGenerateReportsCredentialRefusalsWithoutARequest(t *testing.T) {
	for _, code := range []string{"no_key", "no_credit"} {
		t.Run(code, func(t *testing.T) {
			f := newImageFixture(t)
			f.credentials.err = &mediagen.Error{Code: code, Message: "refused by the credit decision"}
			gotCode, message := toolError(t, f.execute(t, `{"prompt":"a picture","reference_asset_ids":["ref-1"]}`))
			if gotCode != code || message != "refused by the credit decision" {
				t.Fatalf("got %q %q, want %q with the port's message", gotCode, message, code)
			}
			assertNothingSpent(t, f)
		})
	}
}

func TestImageGenerateHidesInfrastructureErrorText(t *testing.T) {
	f := newImageFixture(t)
	f.credentials.err = errors.New("dial tcp 10.0.0.7:5432: connection refused")
	code, message := toolError(t, f.execute(t, `{"prompt":"a picture"}`))
	if code != "job_failed" || strings.Contains(message, "10.0.0.7") {
		t.Fatalf("got %q %q, want job_failed without the infrastructure detail", code, message)
	}
	assertNothingSpent(t, f)
}

func TestImageGenerateFailsWhenTheLiveModelCannotBeRead(t *testing.T) {
	f := newImageFixture(t)
	f.tool.Settings = fakeMediaSettings{err: errors.New("settings store unavailable")}
	if code, _ := toolError(t, f.execute(t, `{"prompt":"a picture"}`)); code != "job_failed" {
		t.Fatalf("code = %q, want job_failed", code)
	}
	assertNothingSpent(t, f)
}

func TestImageGenerateRefusesReferencesItCannotUse(t *testing.T) {
	cases := map[string]struct {
		ref      ownedReference
		wantCode string
	}{
		"foreign asset":  {ownedReference{owner: "someone-else", mimeType: "image/png", modality: "image", data: []byte("x")}, "asset_not_found"},
		"not an image":   {ownedReference{owner: "owner-1", mimeType: "application/pdf", modality: "document", data: []byte("%PDF")}, "unsupported"},
		"over the limit": {ownedReference{owner: "owner-1", mimeType: "image/png", modality: "image", data: make([]byte, 64)}, "too_large"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newImageFixture(t)
			f.references.assets = map[string]ownedReference{"ref-1": tc.ref}
			f.tool.MaxImageBytes = 32
			if code, _ := toolError(t, f.execute(t, `{"prompt":"make it night","reference_asset_ids":["ref-1"]}`)); code != tc.wantCode {
				t.Fatalf("code = %q, want %q", code, tc.wantCode)
			}
			if f.provider.generations() != 0 || f.deliverer.calls != 0 {
				t.Fatal("an unusable reference still reached generation or delivery")
			}
		})
	}
}

func TestImageGenerateReportsProviderRejections(t *testing.T) {
	for status, wantCode := range map[int]string{
		http.StatusBadRequest:          "model_rejected",
		http.StatusPaymentRequired:     "no_credit",
		http.StatusInternalServerError: "job_failed",
	} {
		t.Run(wantCode, func(t *testing.T) {
			f := newImageFixture(t, func(p *fakeOpenRouter, _ []byte) { p.imageStatus = status })
			code, message := toolError(t, f.execute(t, `{"prompt":"a picture"}`))
			if code != wantCode || message != "the model refused these options" {
				t.Fatalf("HTTP %d -> %q %q, want %q with the provider's bounded message", status, code, message, wantCode)
			}
			if f.deliverer.calls != 0 || len(stagedMediaDirs(t, f.runDir)) != 0 {
				t.Fatal("a rejected generation was staged or delivered")
			}
		})
	}
}

func TestImageGenerateReportsAStagingFailureWithoutDelivery(t *testing.T) {
	f := newImageFixture(t)
	if err := os.WriteFile(filepath.Join(f.runDir, "tmp"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _ := toolError(t, f.execute(t, `{"prompt":"a picture"}`)); code != "job_failed" {
		t.Fatalf("code = %q, want job_failed", code)
	}
	if f.provider.generations() != 1 || f.deliverer.calls != 0 {
		t.Fatalf("generations=%d ingests=%d, want the paid call and no delivery", f.provider.generations(), f.deliverer.calls)
	}
}

func TestImageGenerateReportsAFailedIngestWithoutAnArtifact(t *testing.T) {
	f := newImageFixture(t)
	f.deliverer.err = errors.New("object store unavailable")
	code, message := toolError(t, f.execute(t, `{"prompt":"a picture"}`))
	if code != "job_failed" || message == "" {
		t.Fatalf("got %q %q, want job_failed", code, message)
	}
	if f.provider.generations() != 1 || f.deliverer.calls != 1 {
		t.Fatalf("generations=%d ingests=%d, want one of each", f.provider.generations(), f.deliverer.calls)
	}
	if dirs := stagedMediaDirs(t, f.runDir); len(dirs) != 0 {
		t.Fatalf("an undelivered image stayed staged in %v", dirs)
	}
}

// A provider answer declaring a video whose bytes really are MP4 passes a sniff-versus-declared
// check, so only an image-only gate keeps image_generate from staging and ingesting a clip.
func TestImageGenerateRefusesAVideoDeclaredAndSniffedAsVideo(t *testing.T) {
	f := newImageFixture(t, func(p *fakeOpenRouter, _ []byte) {
		p.imageBody = `{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(generatedClip) + `","media_type":"video/mp4"}],"usage":{"cost":0.04}}`
	})
	if code, _ := toolError(t, f.execute(t, `{"prompt":"a picture"}`)); code != "unsupported" {
		t.Fatalf("code = %q, want unsupported for a video answered to an image request", code)
	}
	if f.provider.generations() != 1 || f.deliverer.calls != 0 || len(stagedMediaDirs(t, f.runDir)) != 0 {
		t.Fatalf("generations=%d ingests=%d staged=%v, want the paid call and nothing staged or ingested",
			f.provider.generations(), f.deliverer.calls, stagedMediaDirs(t, f.runDir))
	}
}
