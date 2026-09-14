package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mediagen"
)

// videoModelsFixture is the default model as OpenRouter's video catalog declared it on
// 2026-09-14 (durations 5-15, two resolutions, first and last frames, no aspect ratios, no
// audio), plus a text-only model that declares no frame images.
const videoModelsFixture = `{"data":[` +
	`{"id":"minimax/hailuo-3-max","supported_durations":[5,6,7,8,9,10,11,12,13,14,15],` +
	`"supported_resolutions":["768p","480p"],"supported_aspect_ratios":null,` +
	`"supported_frame_images":["first_frame","last_frame"],"generate_audio":false},` +
	`{"id":"acme/text-to-video","supported_durations":[5],"supported_frame_images":null,"generate_audio":false}]}`

// generatedClip starts with the ftyp box http.DetectContentType recognizes as video/mp4.
var generatedClip = []byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isommp41\x00\x00\x00\x08free")

const (
	videoOwner  = "owner-1"
	videoThread = "thread"
	videoPrompt = "onde all'alba, luce dorata 🌅"
)

// timeline orders events recorded on different goroutines.
type timeline struct {
	mu     sync.Mutex
	events []string
}

func (tl *timeline) add(event string) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.events = append(tl.events, event)
}

func (tl *timeline) snapshot() []string {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return slices.Clone(tl.events)
}

// fakeVideoProvider is OpenRouter's video API: the free catalog, the paid submit, the poll and
// the content download. Every request is recorded, so a refusal can prove it spent nothing and
// a timeout can prove it never submitted twice. A non-nil pollGate holds each poll until closed.
type fakeVideoProvider struct {
	server *httptest.Server
	events *timeline

	catalogStatus int
	submitStatus  int
	submitBody    string
	submitHold    bool
	pollGate      chan struct{}
	pollBody      string

	mu        sync.Mutex
	requests  []string
	submitted map[string]any
	auth      string
}

type videoProviderOption func(*fakeVideoProvider)

func newFakeVideoProvider(t *testing.T, events *timeline, opts ...videoProviderOption) *fakeVideoProvider {
	t.Helper()
	p := &fakeVideoProvider{
		events:     events,
		submitBody: "\n {\"id\":\"vid_1\",\"status\":\"pending\"}\n",
		pollBody:   `{"id":"vid_1","status":"completed","usage":{"cost":0.4}}`,
	}
	for _, opt := range opts {
		opt(p)
	}
	p.server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.server.Close)
	return p
}

func (p *fakeVideoProvider) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	p.mu.Lock()
	p.requests = append(p.requests, r.Method+" "+r.URL.Path)
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/videos/models":
		if p.catalogStatus != 0 {
			w.WriteHeader(p.catalogStatus)
			_, _ = io.WriteString(w, `{"error":{"message":"catalog down"}}`)
			return
		}
		_, _ = io.WriteString(w, videoModelsFixture)
	case r.Method == http.MethodPost && r.URL.Path == "/videos":
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		p.mu.Lock()
		p.submitted, p.auth = decoded, r.Header.Get("Authorization")
		p.mu.Unlock()
		p.respondToSubmit(w, r)
	case strings.HasSuffix(r.URL.Path, "/content"):
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(generatedClip)
	case strings.HasPrefix(r.URL.Path, "/videos/"):
		p.events.add("poll")
		if p.pollGate != nil {
			select {
			case <-p.pollGate:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = io.WriteString(w, p.pollBody)
	default:
		http.NotFound(w, r)
	}
}

func (p *fakeVideoProvider) respondToSubmit(w http.ResponseWriter, r *http.Request) {
	switch {
	case p.submitHold:
		<-r.Context().Done()
	case p.submitStatus != 0:
		w.WriteHeader(p.submitStatus)
		_, _ = io.WriteString(w, `{"error":{"message":"provider unavailable"}}`)
	default:
		_, _ = io.WriteString(w, p.submitBody)
	}
}

func (p *fakeVideoProvider) seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.requests)
}

func (p *fakeVideoProvider) count(request string) int {
	return strings.Count(strings.Join(p.seen(), "\n")+"\n", request+"\n")
}

func (p *fakeVideoProvider) lastSubmit() (body map[string]any, authorization string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.submitted, p.auth
}

// fakeVideoSettings answers the live video model and inline wait and counts every read.
type fakeVideoSettings struct {
	model             string
	wait              time.Duration
	modelErr, waitErr error
	calls             int
}

func (s *fakeVideoSettings) Model(_ context.Context, kind mediagen.Kind) (string, error) {
	s.calls++
	if kind != mediagen.KindVideo {
		return "", errors.New("video_generate asked for a non-video model")
	}
	return s.model, s.modelErr
}

func (s *fakeVideoSettings) VideoInlineWait(context.Context) (time.Duration, error) {
	s.calls++
	return s.wait, s.waitErr
}

// watcherCredentials is the watcher's own credential port: it holds no state, so its
// supervisors never race the test's fakes.
type watcherCredentials struct{ baseURL string }

func (c watcherCredentials) For(context.Context, string) (string, string, error) {
	return c.baseURL, "identity-key", nil
}

type videoFixture struct {
	events      *timeline
	provider    *fakeVideoProvider
	credentials *fakeMediaCredentials
	settings    *fakeVideoSettings
	references  *fakeReferenceReader
	jobs        *fakeVideoJobs
	library     *fakeVideoLibrary
	watcher     *mediagen.Watcher
	notices     chan mediagen.Completion
	tool        *VideoGenerate
	runDir      string
}

// newVideoFixture wires the tool to a real watcher polling the fake provider every 5 ms; the
// watcher is stopped before the provider closes.
func newVideoFixture(t *testing.T, opts ...videoProviderOption) *videoFixture {
	t.Helper()
	events := &timeline{}
	provider := newFakeVideoProvider(t, events, opts...)
	jobs := newFakeVideoJobs(events)
	client := mediagen.NewClient(provider.server.Client(), 1<<20)
	f := &videoFixture{
		events: events, provider: provider, jobs: jobs,
		credentials: &fakeMediaCredentials{baseURL: provider.server.URL},
		settings:    &fakeVideoSettings{model: mediagen.DefaultVideoModel, wait: 10 * time.Second},
		references: &fakeReferenceReader{assets: map[string]ownedReference{
			"frame-1": {owner: videoOwner, mimeType: "image/png", modality: "image", data: pngBytes(t, 4)},
			"ref-1":   {owner: videoOwner, mimeType: "image/png", modality: "image", data: pngBytes(t, 5)},
		}},
		library: &fakeVideoLibrary{jobs: jobs, clips: map[string]storedClip{}, bySource: map[string]string{}},
		notices: make(chan mediagen.Completion, 8),
		runDir:  t.TempDir(),
	}
	f.watcher = mediagen.NewWatcher(context.Background(), jobs, client, watcherCredentials{baseURL: provider.server.URL},
		f.library, func(c mediagen.Completion) { f.notices <- c },
		mediagen.WatcherOptions{PollInterval: 5 * time.Millisecond, MaxAge: mediagen.VideoJobMaxAge, MaxVideoBytes: 1 << 20})
	t.Cleanup(func() { f.stopWatcher(t) })
	f.tool = &VideoGenerate{
		Credentials: f.credentials, Settings: f.settings, Catalog: mediagen.NewCatalog(provider.server.Client()),
		Client: client, References: f.references, Jobs: jobs, Watcher: f.watcher, VideoAssets: f.library,
		MaxImageBytes: 1 << 20, MaxVideoBytes: 1 << 20,
	}
	return f
}

func (f *videoFixture) callCtx(call string) context.Context {
	return WithToolCallContext(identityctx.WithIdentityID(context.Background(), videoOwner), videoThread, call, f.runDir, 8192)
}

func (f *videoFixture) execute(t *testing.T, ctx context.Context, args string) ToolResult {
	t.Helper()
	res, err := f.tool.Execute(ctx, json.RawMessage(args))
	if err != nil {
		t.Fatalf("Execute returned a Go error, want a tool result: %v", err)
	}
	return res
}

func (f *videoFixture) stopWatcher(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := f.watcher.Stop(ctx); err != nil {
		t.Fatalf("watcher Stop: %v", err)
	}
}

func (f *videoFixture) awaitNotice(t *testing.T) mediagen.Completion {
	t.Helper()
	select {
	case notice := <-f.notices:
		return notice
	case <-time.After(10 * time.Second):
		t.Fatal("the wake path was never notified")
		return mediagen.Completion{}
	}
}

// remainingNotices stops the watcher, so no supervisor can notify again, and drains the rest.
func (f *videoFixture) remainingNotices(t *testing.T) []mediagen.Completion {
	t.Helper()
	f.stopWatcher(t)
	var notices []mediagen.Completion
	for {
		select {
		case notice := <-f.notices:
			notices = append(notices, notice)
		default:
			return notices
		}
	}
}

// seedJob stores a job as a submission and its watcher would have left it, in conversation.
// A completed job gets its clip in the library.
func (f *videoFixture) seedJob(t *testing.T, status mediagen.Status, conversation string) mediagen.Job {
	t.Helper()
	request, err := mediagen.JobRequest(
		mediagen.VideoRequest{Model: mediagen.DefaultVideoModel, Prompt: videoPrompt, Duration: 5, Resolution: "768p"},
		mediagen.JobAudit{Origin: f.provider.server.URL, FirstFrameAssetID: "frame-1", Adjustments: []string{"duration 4s is not offered; used 5s"}})
	if err != nil {
		t.Fatal(err)
	}
	cost := 0.4
	job := mediagen.Job{
		IdentityID: videoOwner, ConversationID: conversation, ToolCallID: "call-submit", ProviderJobID: "vid_" + uuid.NewString(),
		Model: mediagen.DefaultVideoModel, Request: request, Status: status, CostUSD: &cost, CreatedAt: time.Now(),
	}
	if status == mediagen.StatusCompleted {
		job.AssetID = f.library.store(videoOwner)
	}
	return f.jobs.seed(job)
}

type videoUsedPreview struct {
	Duration          int      `json:"duration"`
	Resolution        string   `json:"resolution"`
	AspectRatio       string   `json:"aspect_ratio"`
	FirstFrameAssetID string   `json:"first_frame_asset_id"`
	ReferenceAssetIDs []string `json:"reference_asset_ids"`
	Audio             *bool    `json:"audio"`
}

// videoPreview decodes both the delivered clip's summary and a job status: they share the
// model, cost, used and adjustments fields.
type videoPreview struct {
	AssetID     string           `json:"asset_id"`
	MIMEType    string           `json:"mime_type"`
	Status      string           `json:"status"`
	JobID       string           `json:"job_id"`
	Message     string           `json:"message"`
	Model       string           `json:"model"`
	CostUSD     *float64         `json:"cost_usd"`
	Used        videoUsedPreview `json:"used"`
	Adjustments []string         `json:"adjustments"`
}

func decodeVideoPreview(t *testing.T, res ToolResult) videoPreview {
	t.Helper()
	var preview videoPreview
	if err := json.Unmarshal([]byte(res.Preview), &preview); err != nil {
		t.Fatalf("preview %q: %v", res.Preview, err)
	}
	if res.Bytes != len(res.Preview) {
		t.Fatalf("Bytes = %d, want %d", res.Bytes, len(res.Preview))
	}
	return preview
}

// videoStatus decodes a job status result, which never carries an artifact.
func videoStatus(t *testing.T, res ToolResult) videoPreview {
	t.Helper()
	if res.Meta != nil {
		t.Fatalf("a status result carried meta %#v", *res.Meta)
	}
	status := decodeVideoPreview(t, res)
	if status.Status == "" || status.JobID == "" || status.Message == "" {
		t.Fatalf("preview %q is not a job status", res.Preview)
	}
	return status
}
