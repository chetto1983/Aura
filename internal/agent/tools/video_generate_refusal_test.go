package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mediagen"
)

// assertNoVideoSpend proves a refusal happened before any provider request, asset read, job
// row or staged file: a video is paid, so every refusal decided locally comes first.
func assertNoVideoSpend(t *testing.T, f *videoFixture) {
	t.Helper()
	if seen := f.provider.seen(); len(seen) != 0 {
		t.Fatalf("provider saw %v, want no request", seen)
	}
	if len(f.references.opened) != 0 || f.library.opens() != 0 || f.jobs.count("Insert") != 0 {
		t.Fatalf("opened %v references and %d clips, inserted %d jobs; want none",
			f.references.opened, f.library.opens(), f.jobs.count("Insert"))
	}
	if dirs := stagedMediaDirs(t, f.runDir); len(dirs) != 0 {
		t.Fatalf("staged %v, want nothing", dirs)
	}
}

func TestVideoGenerateRefusesWithoutPromptOrJobID(t *testing.T) {
	for name, args := range map[string]string{
		"empty object":   `{}`,
		"no arguments":   ``,
		"blank prompt":   `{"prompt":"  \n"}`,
		"blank job id":   `{"job_id":" "}`,
		"options only":   `{"duration":5,"resolution":"768p"}`,
		"both are blank": `{"prompt":"","job_id":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newVideoFixture(t)
			code, message := toolError(t, f.execute(t, f.callCtx("call-video"), args))
			if code != "unsupported" || !strings.Contains(message, "prompt") || !strings.Contains(message, "job_id") {
				t.Fatalf("%s -> %q %q, want unsupported naming prompt and job_id", name, code, message)
			}
			if f.credentials.calls != 0 || f.settings.calls != 0 {
				t.Fatal("a call with neither prompt nor job_id still read credentials or settings")
			}
			assertNoVideoSpend(t, f)
		})
	}
}

func TestVideoGenerateRefusesMalformedInvocations(t *testing.T) {
	for name, args := range map[string]string{
		"not JSON":          `{"prompt":`,
		"not an object":     `["waves"]`,
		"agent picks model": `{"prompt":"waves","model":"google/veo-3.1"}`,
		"trailing value":    `{"prompt":"waves"} {}`,
		"fractional length": `{"prompt":"waves","duration":5.5}`,
		"negative length":   `{"prompt":"waves","duration":-5}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newVideoFixture(t)
			if code, message := toolError(t, f.execute(t, f.callCtx("call-video"), args)); code != "unsupported" || message == "" {
				t.Fatalf("%s -> %q %q, want an actionable unsupported", name, code, message)
			}
			if f.credentials.calls != 0 {
				t.Fatal("a malformed invocation still resolved credentials")
			}
			assertNoVideoSpend(t, f)
		})
	}
}

func TestVideoGenerateRefusesWhenADependencyIsMissing(t *testing.T) {
	for name, strip := range map[string]func(*VideoGenerate){
		"credentials":     func(g *VideoGenerate) { g.Credentials = nil },
		"settings":        func(g *VideoGenerate) { g.Settings = nil },
		"catalog":         func(g *VideoGenerate) { g.Catalog = nil },
		"client":          func(g *VideoGenerate) { g.Client = nil },
		"references":      func(g *VideoGenerate) { g.References = nil },
		"jobs":            func(g *VideoGenerate) { g.Jobs = nil },
		"watcher":         func(g *VideoGenerate) { g.Watcher = nil },
		"video assets":    func(g *VideoGenerate) { g.VideoAssets = nil },
		"max image bytes": func(g *VideoGenerate) { g.MaxImageBytes = 0 },
		"max video bytes": func(g *VideoGenerate) { g.MaxVideoBytes = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			f := newVideoFixture(t)
			strip(f.tool)
			for _, args := range []string{`{"prompt":"waves"}`, `{"job_id":"` + f.seedJob(t, mediagen.StatusCompleted, videoThread).ID + `"}`} {
				if code, message := toolError(t, f.execute(t, f.callCtx("call-video"), args)); code != "unsupported" || message == "" {
					t.Fatalf("missing %s, %s -> %q %q, want unsupported", name, args, code, message)
				}
			}
			if f.credentials.calls != 0 || f.jobs.count("Get") != 0 {
				t.Fatal("an unconfigured tool still resolved credentials or read a job")
			}
			assertNoVideoSpend(t, f)
		})
	}
	res, err := (&VideoGenerate{}).Execute(mediaCtx(t, t.TempDir()), json.RawMessage(`{"prompt":"waves"}`))
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := toolError(t, res); code != "unsupported" {
		t.Fatalf("the registry's zero-value tool -> %q, want unsupported", code)
	}
}

func TestVideoGenerateRefusesWithoutADeliverableConversation(t *testing.T) {
	f := newVideoFixture(t)
	owned := identityctx.WithIdentityID(context.Background(), videoOwner)
	for name, ctx := range map[string]context.Context{
		"no identity":  WithToolCallContext(context.Background(), videoThread, "call-video", f.runDir, 8192),
		"no thread":    WithToolCallContext(owned, "", "call-video", f.runDir, 8192),
		"no tool call": WithToolCallContext(owned, videoThread, "", f.runDir, 8192),
		"no run dir":   WithToolCallContext(owned, videoThread, "call-video", filepath.Join(f.runDir, "gone"), 8192),
	} {
		if code, _ := toolError(t, f.execute(t, ctx, `{"prompt":"waves"}`)); code != "unsupported" {
			t.Fatalf("%s -> %q, want unsupported", name, code)
		}
	}
	if f.credentials.calls != 0 {
		t.Fatal("an undeliverable call still resolved credentials")
	}
	assertNoVideoSpend(t, f)
}

func TestVideoGenerateReportsCredentialRefusalsWithoutARequest(t *testing.T) {
	for _, code := range []string{"no_key", "no_credit"} {
		t.Run(code, func(t *testing.T) {
			f := newVideoFixture(t)
			f.credentials.err = &mediagen.Error{Code: code, Message: "refused by the credit decision"}
			gotCode, message := toolError(t, f.execute(t, f.callCtx("call-video"), `{"prompt":"waves","first_frame_asset_id":"frame-1"}`))
			if gotCode != code || message != "refused by the credit decision" {
				t.Fatalf("got %q %q, want %q with the port's message", gotCode, message, code)
			}
			if f.settings.calls != 0 {
				t.Fatal("a refused credential still read the live settings")
			}
			assertNoVideoSpend(t, f)
		})
	}
}

func TestVideoGenerateFailsBeforeChargingWhenSettingsCannotBeRead(t *testing.T) {
	for name, broken := range map[string]func(*fakeVideoSettings){
		"model": func(s *fakeVideoSettings) { s.modelErr = errors.New("settings store unavailable") },
		"wait":  func(s *fakeVideoSettings) { s.waitErr = errors.New("invalid AURA_VIDEO_INLINE_WAIT_SEC") },
	} {
		t.Run(name, func(t *testing.T) {
			f := newVideoFixture(t)
			broken(f.settings)
			code, message := toolError(t, f.execute(t, f.callCtx("call-video"), `{"prompt":"waves"}`))
			if code != "job_failed" || strings.Contains(message, "AURA_") || strings.Contains(message, "store") {
				t.Fatalf("got %q %q, want job_failed without the settings detail", code, message)
			}
			assertNoVideoSpend(t, f)
		})
	}
}

// Animating an image on a model that cannot take one is refused from the catalog alone: the
// image is never read, let alone encoded and sent.
func TestVideoGenerateRefusesAFirstFrameBeforeReadingIt(t *testing.T) {
	f := newVideoFixture(t)
	f.settings.model = "acme/text-to-video"
	code, _ := toolError(t, f.execute(t, f.callCtx("call-video"), `{"prompt":"animate it","first_frame_asset_id":"frame-1"}`))
	if code != "unsupported" || len(f.references.opened) != 0 {
		t.Fatalf("code %q after opening %v, want unsupported before any read", code, f.references.opened)
	}
	if seen := f.provider.seen(); len(seen) != 1 || seen[0] != "GET /videos/models" || f.jobs.count("Insert") != 0 {
		t.Fatalf("requests %v, want only the free catalog read", seen)
	}
}

func TestVideoGenerateRefusesImagesItCannotUse(t *testing.T) {
	for name, args := range map[string]string{
		"foreign first frame": `{"prompt":"animate it","first_frame_asset_id":"not-mine"}`,
		"foreign reference":   `{"prompt":"follow it","reference_asset_ids":["ref-1","not-mine"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newVideoFixture(t)
			f.references.assets["not-mine"] = ownedReference{owner: "someone-else", mimeType: "image/png", modality: "image", data: pngBytes(t, 2)}
			if code, _ := toolError(t, f.execute(t, f.callCtx("call-video"), args)); code != "asset_not_found" {
				t.Fatalf("code = %q, want asset_not_found", code)
			}
			if f.provider.count("POST /videos") != 0 || f.jobs.count("Insert") != 0 {
				t.Fatal("an unusable image still reached the paid submit")
			}
		})
	}
}

// The submission interval is excluded from the recovery guarantee (R4): a submit whose answer
// never arrives, or fails, is reported once and never sent again. The code says which: a
// provider that answered with an error refused the job, while a missing answer leaves the
// charge unknown and tells the model not to send it again.
func TestVideoGenerateNeverResubmitsAfterAProviderTimeoutOrFailure(t *testing.T) {
	for name, tc := range map[string]struct {
		opt  videoProviderOption
		code string
	}{
		"timeout": {opt: func(p *fakeVideoProvider) { p.submitHold = true }, code: "outcome_unknown"},
		"5xx":     {opt: func(p *fakeVideoProvider) { p.submitStatus = http.StatusBadGateway }, code: "job_failed"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newVideoFixture(t, tc.opt)
			ctx, cancel := context.WithTimeout(f.callCtx("call-video"), 300*time.Millisecond)
			defer cancel()
			if code, _ := toolError(t, f.execute(t, ctx, `{"prompt":"waves"}`)); code != tc.code {
				t.Fatalf("code = %q, want %q", code, tc.code)
			}
			if f.provider.count("POST /videos") != 1 || f.jobs.count("Insert") != 0 {
				t.Fatalf("requests %v, %d inserts; want one submit and no job", f.provider.seen(), f.jobs.count("Insert"))
			}
		})
	}
}

// A lost Insert leaves a paid provider job that only the log names: the operator reconciles it
// from the owner and provider job ID, and the log never carries the key.
func TestVideoGenerateReportsAnUnrecordedSubmissionWithoutResubmitting(t *testing.T) {
	logs := captureLogs(t)
	f := newVideoFixture(t)
	f.jobs.insertErr = errors.New("dial tcp 10.0.0.7:5432: connection refused")
	code, message := toolError(t, f.execute(t, f.callCtx("call-video"), `{"prompt":"waves"}`))
	if code != "job_failed" || !strings.Contains(message, "not submitted again") || strings.Contains(message, "10.0.0.7") {
		t.Fatalf("got %q %q, want job_failed saying it was not submitted again", code, message)
	}
	if notices := f.remainingNotices(t); len(notices) != 0 || f.provider.count("POST /videos") != 1 || f.provider.count("GET /videos/vid_1") != 0 {
		t.Fatalf("requests %v, wakes %+v; want one submit and no supervision", f.provider.seen(), notices)
	}
	logged := logs.String()
	for _, want := range []string{"level=ERROR", "owner=" + videoOwner, "provider_job_id=vid_1"} {
		if !strings.Contains(logged, want) {
			t.Errorf("log %q lacks %q", logged, want)
		}
	}
	if strings.Count(logged, "level=") != 1 || strings.Contains(logged, "identity-key") || strings.Contains(logged, "Bearer") {
		t.Fatalf("log %q, want one record without the identity key", logged)
	}
}

// captureLogs sends the default logger to a buffer for the test, keeping test output clean.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}
