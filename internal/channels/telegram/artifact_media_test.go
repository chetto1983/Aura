package telegram

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tele "gopkg.in/telebot.v4"
)

// mediaBot is the artifact consumer's double when the PAYLOAD TYPE is what matters:
// it records every send attempt whatever its type, and can reject a chosen payload so
// the fallback rules are observable.
type mediaBot struct {
	mu       sync.Mutex
	attempts []any
	fail     func(what any) error
}

func (b *mediaBot) Send(_ tele.Recipient, what any, _ ...any) (*tele.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attempts = append(b.attempts, what)
	if b.fail != nil {
		if err := b.fail(what); err != nil {
			return nil, err
		}
	}
	return &tele.Message{ID: len(b.attempts)}, nil
}

func (b *mediaBot) Edit(_ tele.Editable, _ any, _ ...any) (*tele.Message, error) {
	return &tele.Message{}, nil
}

func (b *mediaBot) recorded() []any {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]any, len(b.attempts))
	copy(out, b.attempts)
	return out
}

// fixtureFile creates a REAL file of exactly size bytes (sparse — os.Truncate never
// writes the body) so the routing table is decided by the same os.Stat the production
// path calls, not by a number a test made up.
func fixtureFile(t *testing.T, name string, size int64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}
	if err := os.Truncate(path, size); err != nil {
		t.Fatalf("size fixture to %d: %v", size, err)
	}
	return path
}

// payloadKind names the delivery lane a payload represents, so the table reads like
// the decision it encodes.
func payloadKind(payload any) string {
	switch payload.(type) {
	case *tele.Photo:
		return "photo"
	case *tele.Video:
		return "video"
	case *tele.Document:
		return "document"
	case string:
		return "cockpit"
	}
	return "unknown"
}

// TestArtifactMediaRouting pins the dispatch table. The Telegram ceilings are the
// conservative decimal ones (10 MB photo / 50 MB file); Aura's own asset cap is
// 52428800 bytes, so a clip CAN exist above the upload ceiling and below the cap —
// that one is announced, never uploaded.
func TestArtifactMediaRouting(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mime string
		size int64
		want string
	}{
		{"photo at the ceiling", "image/png", 10_000_000, "photo"},
		{"image one byte over the photo ceiling", "image/png", 10_000_001, "document"},
		{"video at the ceiling", "video/mp4", 50_000_000, "video"},
		{"clip over the ceiling but inside the asset cap", "video/mp4", 50_000_001, "cockpit"},
		{"a non-media file is a document", "application/pdf", 100, "document"},
		{"an unknown MIME stays a document", "", 100, "document"},
		{"an unrecognised MIME stays a document", "application/x-whatever", 100, "document"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, ok := artifactPayload(map[string]any{
				"path": fixtureFile(t, "fixture.bin", tc.size), "filename": "fixture",
				"mime_type": tc.mime, "size_bytes": tc.size, "caption": "a test",
			})
			if !ok {
				t.Fatal("valid descriptor ignored")
			}
			if got := payloadKind(payload); got != tc.want {
				t.Fatalf("%s %d: %s, want %s", tc.mime, tc.size, got, tc.want)
			}
		})
	}
}

// TestArtifactPayloadTrustsStatOverDescriptor: the descriptor's size_bytes is the
// producer's claim, the file is the fact. A stale or wrong claim must not push a
// 10-MB-over image into a photo upload the Bot API would reject.
func TestArtifactPayloadTrustsStatOverDescriptor(t *testing.T) {
	t.Parallel()
	payload, ok := artifactPayload(map[string]any{
		"path": fixtureFile(t, "big.png", 10_000_001), "filename": "big.png",
		"mime_type": "image/png", "size_bytes": int64(100),
	})
	if !ok {
		t.Fatal("valid descriptor ignored")
	}
	if got := payloadKind(payload); got != "document" {
		t.Fatalf("a file bigger than it claims must travel as a document, got %s", got)
	}

	payload, ok = artifactPayload(map[string]any{
		"path": fixtureFile(t, "small.png", 100), "filename": "small.png",
		"mime_type": "image/png", "size_bytes": int64(99_000_000),
	})
	if !ok {
		t.Fatal("valid descriptor ignored")
	}
	if got := payloadKind(payload); got != "photo" {
		t.Fatalf("a file smaller than it claims must travel as a photo, got %s", got)
	}
}

// TestArtifactPayloadUnstattableIsNotSendable: a path that cannot be stat'd is not a
// deliverable artifact. Uploading it would fail at the Bot API anyway, so the
// consumer declines instead of spending a round trip.
func TestArtifactPayloadUnstattableIsNotSendable(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "gone.png")
	if _, ok := artifactPayload(map[string]any{"path": missing, "mime_type": "image/png"}); ok {
		t.Error("a missing file must not be sendable")
	}
	if _, ok := artifactPayload(map[string]any{"path": t.TempDir(), "filename": "dir"}); ok {
		t.Error("a directory must not be sendable")
	}
	if _, ok := artifactPayload(map[string]any{"filename": "x.txt"}); ok {
		t.Error("a pathless descriptor must not be sendable")
	}
}

// TestArtifactCaptionBoundedAfterSanitization: the Bot API caps a caption at 1024
// characters, and the cut happens AFTER the ASCII folding (folding can only shrink,
// but capping first would still hand the API a longer string than it accepts).
func TestArtifactCaptionBoundedAfterSanitization(t *testing.T) {
	t.Parallel()
	payload, ok := artifactPayload(map[string]any{
		"path": fixtureFile(t, "shot.png", 100), "filename": "shot.png",
		"mime_type": "image/png", "caption": strings.Repeat("città ", 1000),
	})
	if !ok {
		t.Fatal("valid descriptor ignored")
	}
	photo, isPhoto := payload.(*tele.Photo)
	if !isPhoto {
		t.Fatalf("want a photo, got %s", payloadKind(payload))
	}
	if n := len([]rune(photo.Caption)); n != telegramCaptionCap {
		t.Fatalf("caption is %d runes, want the Bot API cap %d", n, telegramCaptionCap)
	}
	for i, r := range photo.Caption {
		if r > 0x7F {
			t.Fatalf("caption[%d]=%q is non-ASCII", i, r)
		}
	}
}

// TestArtifactOversizedVideoAnnouncesTheCockpit: a clip Telegram will not take is
// still in the cockpit, so the chat says where it is instead of failing silently.
func TestArtifactOversizedVideoAnnouncesTheCockpit(t *testing.T) {
	t.Parallel()
	bot := &mediaBot{}
	a := newArtifact(bot, tele.ChatID(42))
	msg, ok := a.consumeEvent(artifactCustom(map[string]any{
		"path": fixtureFile(t, "clip.mp4", 50_000_001), "filename": "clip.mp4",
		"mime_type": "video/mp4", "caption": "a long clip",
	}))
	if !ok || msg == nil {
		t.Fatal("an oversized clip must still produce a chat message")
	}
	sent := bot.recorded()
	if len(sent) != 1 {
		t.Fatalf("want 1 send, got %d", len(sent))
	}
	text, isText := sent[0].(string)
	if !isText || text != videoInCockpitMessage("a long clip") {
		t.Fatalf("sent %#v, want the cockpit fallback %q", sent[0], videoInCockpitMessage("a long clip"))
	}
}

// TestArtifactOversizedVideoNamesTheClip: two clips in one turn must not send the same
// sentence twice — the notice carries the (already capped) caption, and stands alone
// when there is none.
func TestArtifactOversizedVideoNamesTheClip(t *testing.T) {
	t.Parallel()
	payload, ok := artifactPayload(map[string]any{
		"path": fixtureFile(t, "clip.mp4", 50_000_001), "filename": "clip.mp4",
		"mime_type": "video/mp4", "caption": strings.Repeat("b", 2000),
	})
	if !ok {
		t.Fatal("valid descriptor ignored")
	}
	text, isText := payload.(string)
	if !isText {
		t.Fatalf("want the cockpit fallback, got %s", payloadKind(payload))
	}
	if !strings.HasPrefix(text, videoInCockpitPrefix) {
		t.Errorf("the notice must open with the package glyph, got %q", text[:40])
	}
	if want := len([]rune(videoInCockpitPrefix)) + 1 + telegramCaptionCap; len([]rune(text)) != want {
		t.Errorf("notice is %d runes, want %d (prefix + the already capped caption)", len([]rune(text)), want)
	}

	payload, _ = artifactPayload(map[string]any{
		"path": fixtureFile(t, "clip.mp4", 50_000_001), "filename": "clip.mp4", "mime_type": "video/mp4",
	})
	if text, _ := payload.(string); text != videoInCockpitPrefix {
		t.Errorf("a captionless clip must send the bare notice, got %q", text)
	}
}

// TestArtifactNativeVideoIsStreamable: a clip inside the ceiling is a real video
// upload with supports_streaming, so Telegram plays it inline instead of offering a
// 50 MB download.
func TestArtifactNativeVideoIsStreamable(t *testing.T) {
	t.Parallel()
	bot := &mediaBot{}
	a := newArtifact(bot, tele.ChatID(42))
	if _, ok := a.consumeEvent(artifactCustom(map[string]any{
		"path": fixtureFile(t, "clip.mp4", 2048), "filename": "clip.mp4",
		"mime_type": "video/mp4", "caption": "a clip",
	})); !ok {
		t.Fatal("a native clip must be delivered")
	}
	sent := bot.recorded()
	if len(sent) != 1 {
		t.Fatalf("want 1 send, got %d", len(sent))
	}
	video, isVideo := sent[0].(*tele.Video)
	if !isVideo {
		t.Fatalf("sent %#v, want a *tele.Video", sent[0])
	}
	if !video.Streaming {
		t.Error("a native video upload must set supports_streaming")
	}
	if video.FileName != "clip.mp4" || video.Caption != "a clip" {
		t.Errorf("video = %q/%q, want clip.mp4/a clip", video.FileName, video.Caption)
	}
}

// TestArtifactRejectedPhotoFallsBackToDocument: a 400 is a DEFINITE Bot API refusal —
// nothing was delivered, so the same bytes may be re-offered as a file (the provider
// format Telegram would not process as an image still travels fine).
func TestArtifactRejectedPhotoFallsBackToDocument(t *testing.T) {
	t.Parallel()
	bot := &mediaBot{fail: func(what any) error {
		if _, isPhoto := what.(*tele.Photo); isPhoto {
			return tele.NewError(400, "Bad Request: IMAGE_PROCESS_FAILED")
		}
		return nil
	}}
	a := newArtifact(bot, tele.ChatID(42))
	msg, ok := a.consumeEvent(artifactCustom(map[string]any{
		"path": fixtureFile(t, "art.png", 100), "filename": "art.png",
		"mime_type": "image/png", "caption": "art",
	}))
	if !ok || msg == nil {
		t.Fatal("a rejected photo must still be delivered as a document")
	}
	sent := bot.recorded()
	if len(sent) != 2 {
		t.Fatalf("want a photo attempt then a document, got %d sends", len(sent))
	}
	doc, isDoc := sent[1].(*tele.Document)
	if !isDoc {
		t.Fatalf("second send = %#v, want a *tele.Document", sent[1])
	}
	if doc.FileName != "art.png" || doc.Caption != "art" {
		t.Errorf("fallback document = %q/%q, want art.png/art", doc.FileName, doc.Caption)
	}
}

// TestArtifactAmbiguousFailureIsNeverRetried: a transport failure or a 5xx leaves the
// delivery UNKNOWN — the message may well have reached the chat, so a second send
// would duplicate it. One attempt, then give up (best-effort, the turn continues).
func TestArtifactAmbiguousFailureIsNeverRetried(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"a dropped connection", errors.New("read tcp 1.2.3.4:443: connection reset by peer")},
		{"a server-side failure", tele.NewError(500, "Internal Server Error")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bot := &mediaBot{fail: func(any) error { return tc.err }}
			a := newArtifact(bot, tele.ChatID(42))
			if _, ok := a.consumeEvent(artifactCustom(map[string]any{
				"path": fixtureFile(t, "art.png", 100), "filename": "art.png",
				"mime_type": "image/png", "caption": "art",
			})); ok {
				t.Error("an ambiguous failure must not report a delivery")
			}
			if n := len(bot.recorded()); n != 1 {
				t.Fatalf("want exactly 1 attempt, got %d", n)
			}
		})
	}
}

// TestArtifactRejectedVideoIsNotRetried: only a photo has a cheaper lane to fall back
// to. A refused video upload is reported, not re-sent as something else.
func TestArtifactRejectedVideoIsNotRetried(t *testing.T) {
	t.Parallel()
	bot := &mediaBot{fail: func(any) error { return tele.NewError(400, "Bad Request: VIDEO_INVALID") }}
	a := newArtifact(bot, tele.ChatID(42))
	if _, ok := a.consumeEvent(artifactCustom(map[string]any{
		"path": fixtureFile(t, "clip.mp4", 2048), "filename": "clip.mp4",
		"mime_type": "video/mp4", "caption": "a clip",
	})); ok {
		t.Error("a refused video must not report a delivery")
	}
	if n := len(bot.recorded()); n != 1 {
		t.Fatalf("want exactly 1 attempt, got %d", n)
	}
}

// TestArtifactHoldsUploadActionAcrossTheSend is the point of the hold: the tool call is
// already over when the bytes start moving (TOOL_CALL_END, TOOL_CALL_RESULT and the
// artifact descriptor ride the same source event), so without it the chat would say
// "typing…" through the heaviest part of the turn.
func TestArtifactHoldsUploadActionAcrossTheSend(t *testing.T) {
	t.Parallel()
	rn := &recordingNotifier{}
	ctrl := newMediaActionController(rn, tele.ChatID(7))
	ctrl.Start("call-1", "video_generate")
	ctrl.Finish("call-1") // the pane's terminal, fired before the upload begins

	var duringSend tele.ChatAction
	bot := &mediaBot{fail: func(any) error {
		duringSend = ctrl.action()
		return nil
	}}
	a := &artifact{bot: bot, to: tele.ChatID(7), actions: ctrl}

	if _, ok := a.consumeEvent(artifactCustom(map[string]any{
		"path": fixtureFile(t, "clip.mp4", 2048), "filename": "clip.mp4",
		"mime_type": "video/mp4", "caption": "a clip", "tool_call_id": "call-1",
	})); !ok {
		t.Fatal("the clip must be delivered")
	}
	if duringSend != tele.UploadingVideo {
		t.Errorf("action during the upload = %q, want %q", duringSend, tele.UploadingVideo)
	}
	if got := ctrl.action(); got != tele.Typing {
		t.Errorf("action after the upload = %q, want %q", got, tele.Typing)
	}
	assertActions(t, rn, tele.UploadingVideo, tele.Typing, tele.UploadingVideo, tele.Typing)
}

// TestArtifactReleasesTheHoldWhenTheSendFails: a failed upload must free the action too,
// or the chat would claim to be uploading for the rest of the turn.
func TestArtifactReleasesTheHoldWhenTheSendFails(t *testing.T) {
	t.Parallel()
	rn := &recordingNotifier{}
	ctrl := newMediaActionController(rn, tele.ChatID(7))
	bot := &mediaBot{fail: func(any) error { return errors.New("connection reset by peer") }}
	a := &artifact{bot: bot, to: tele.ChatID(7), actions: ctrl}

	if _, ok := a.consumeEvent(artifactCustom(map[string]any{
		"path": fixtureFile(t, "shot.png", 100), "filename": "shot.png",
		"mime_type": "image/png", "caption": "art", "tool_call_id": "call-1",
	})); ok {
		t.Error("a failed upload must not report a delivery")
	}
	if got := ctrl.action(); got != tele.Typing {
		t.Errorf("action after a failed upload = %q, want %q", got, tele.Typing)
	}
	assertActions(t, rn, tele.UploadingPhoto, tele.Typing)
}

// TestArtifactWithoutToolCallIDStillDelivers: the correlation key is best-effort on the
// descriptor, so its absence costs the chat action, never the delivery.
func TestArtifactWithoutToolCallIDStillDelivers(t *testing.T) {
	t.Parallel()
	rn := &recordingNotifier{}
	ctrl := newMediaActionController(rn, tele.ChatID(7))
	bot := &mediaBot{}
	a := &artifact{bot: bot, to: tele.ChatID(7), actions: ctrl}

	if _, ok := a.consumeEvent(artifactCustom(map[string]any{
		"path": fixtureFile(t, "shot.png", 100), "filename": "shot.png", "mime_type": "image/png",
	})); !ok {
		t.Fatal("a descriptor with no tool_call_id must still deliver")
	}
	if n := len(bot.recorded()); n != 1 {
		t.Fatalf("want 1 send, got %d", n)
	}
	if n := rn.count(); n != 0 {
		t.Errorf("want no chat action without a correlation key, got %d", n)
	}
}

// TestArtifactHoldsNothingForADocument: only the native media uploads claim an action —
// a generic send_file delivery is not what this turn promised.
func TestArtifactHoldsNothingForADocument(t *testing.T) {
	t.Parallel()
	rn := &recordingNotifier{}
	ctrl := newMediaActionController(rn, tele.ChatID(7))
	a := &artifact{bot: &mediaBot{}, to: tele.ChatID(7), actions: ctrl}

	if _, ok := a.consumeEvent(artifactCustom(map[string]any{
		"path": fixtureFile(t, "report.pdf", 100), "filename": "report.pdf",
		"mime_type": "application/pdf", "tool_call_id": "call-1",
	})); !ok {
		t.Fatal("the document must be delivered")
	}
	if n := rn.count(); n != 0 {
		t.Errorf("a document upload must claim no action, got %d", n)
	}
}

// TestArtifactUnstattableDeclineIsLogged: every other best-effort miss in this package
// logs, so a file that vanished between staging and delivery must not disappear in
// silence — that is the only trace an operator would have.
func TestArtifactUnstattableDeclineIsLogged(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })

	missing := filepath.Join(t.TempDir(), "gone.png")
	if _, ok := artifactPayload(map[string]any{"path": missing, "mime_type": "image/png"}); ok {
		t.Fatal("a missing file must not be sendable")
	}
	out := logs.String()
	if !strings.Contains(out, "artifact file unreadable") || !strings.Contains(out, "gone.png") {
		t.Fatalf("the decline must be logged with its path, got %q", out)
	}

	logs.Reset()
	if _, ok := artifactPayload(map[string]any{"path": t.TempDir(), "filename": "dir"}); ok {
		t.Fatal("a directory must not be sendable")
	}
	if !strings.Contains(logs.String(), "artifact path is a directory") {
		t.Fatalf("a directory decline must be logged, got %q", logs.String())
	}
}
