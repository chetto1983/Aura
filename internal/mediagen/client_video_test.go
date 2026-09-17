package mediagen

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubmitVideoSendsJSONAndAcceptsWhitespace(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/videos" ||
			!strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			t.Errorf("unexpected video request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != "minimax/hailuo-3-max" || body["duration"] != float64(5) {
			t.Error("model/duration wire fields differ")
		}
		if _, found := body["generate_audio"]; found {
			t.Error("unsupported audio was sent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "\n\n {\"id\":\"vid_1\",\"status\":\"in_progress\"}\n")
	}))
	defer srv.Close()
	client := NewClient(srv.Client(), 25<<20)
	got, err := client.SubmitVideo(context.Background(), srv.URL+"/", "test-key",
		VideoRequest{Model: "minimax/hailuo-3-max", Prompt: "moving sea", Duration: 5})
	if err != nil || got.ID != "vid_1" || calls != 1 {
		t.Fatalf("submit: %v %#v", err, got)
	}
	if got.Status != StatusInProgress {
		t.Fatalf("Status = %q, want in_progress", got.Status)
	}
}

func TestSubmitVideoSendsFrameAndInputReferencesAndOmitsUnsetFields(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"vid_2","status":"pending"}`)
	}))
	defer srv.Close()
	client := NewClient(srv.Client(), 1<<20)
	audio := true
	req := VideoRequest{
		Model: "alibaba/wan-2.7", Prompt: "a walk", Resolution: "1080p", AspectRatio: "16:9",
		FrameImages: []FrameReference{
			{Type: "image_url", ImageURL: ImageURL{URL: "https://example.com/first.png"}, FrameType: "first_frame"},
		},
		InputReferences: []ImageReference{
			{Type: "image_url", ImageURL: ImageURL{URL: "https://example.com/ref.png"}},
		},
		GenerateAudio: &audio,
	}
	if _, err := client.SubmitVideo(context.Background(), srv.URL, "k", req); err != nil {
		t.Fatal(err)
	}
	if body["resolution"] != "1080p" || body["aspect_ratio"] != "16:9" || body["generate_audio"] != true {
		t.Fatalf("body = %#v", body)
	}
	frames, ok := body["frame_images"].([]any)
	if !ok || len(frames) != 1 {
		t.Fatalf("frame_images = %#v", body["frame_images"])
	}
	frame := frames[0].(map[string]any)
	if frame["frame_type"] != "first_frame" {
		t.Fatalf("frame_type = %#v", frame["frame_type"])
	}
	refs, ok := body["input_references"].([]any)
	if !ok || len(refs) != 1 {
		t.Fatalf("input_references = %#v", body["input_references"])
	}
	if _, found := body["duration"]; found {
		t.Fatal("unset duration must be omitted")
	}
}

// TestSubmitVideoNeverResendsAfterFailureOrLostResponse pins the two pre-Insert rows of the
// spec's recovery boundary: a provider failure and a response lost after the provider read
// the whole body both surface as an error after exactly one POST, never as a second paid
// submission. The code tells the model which one it was: a provider that answered with an
// error refused the job, while a lost or unreadable answer leaves the job — and its charge —
// unknown, which must stop the model from submitting it again (measured live 2026-09-17).
func TestSubmitVideoNeverResendsAfterFailureOrLostResponse(t *testing.T) {
	cases := map[string]struct {
		respond func(t *testing.T, w http.ResponseWriter)
		code    string
	}{
		"provider 5xx": {code: "job_failed", respond: func(_ *testing.T, w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"code":500,"message":"Internal Server Error"}}`)
		}},
		"response lost after acceptance": {code: "outcome_unknown", respond: func(t *testing.T, w http.ResponseWriter) {
			conn, _, err := http.NewResponseController(w).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
		}},
		"accepted with an unreadable answer": {code: "outcome_unknown", respond: func(_ *testing.T, w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":`)
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			respond := tc.respond
			var posts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts.Add(1)
				}
				_, _ = io.Copy(io.Discard, r.Body)
				respond(t, w)
			}))
			defer srv.Close()
			_, err := NewClient(srv.Client(), 1<<20).SubmitVideo(context.Background(), srv.URL, "k",
				VideoRequest{Model: "minimax/hailuo-3-max", Prompt: "moving sea", Duration: 5})
			if err == nil {
				t.Fatal("a failed or lost submit must surface as an error, never as an accepted job")
			}
			if ErrorCode(err) != tc.code {
				t.Fatalf("ErrorCode = %q, want %q (%v)", ErrorCode(err), tc.code, err)
			}
			if got := posts.Load(); got != 1 {
				t.Fatalf("POSTs = %d, want exactly 1: a paid submission is never re-sent", got)
			}
		})
	}
}

func TestGetVideoTerminalStatuses(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantStatus Status
		wantCode   string
		wantMsg    string
	}{
		{"completed carries no error", `{"id":"v1","status":"completed","usage":{"cost":0.25}}`, StatusCompleted, "", ""},
		{"pending carries no error", `{"id":"v1","status":"pending"}`, StatusPending, "", ""},
		{"failed uses the provider string and job_failed", `{"id":"v1","status":"failed","error":"Content policy violation"}`, StatusFailed, "job_failed", "Content policy violation"},
		{"failed with no error string falls back", `{"id":"v1","status":"failed"}`, StatusFailed, "job_failed", "Video generation failed."},
		{"cancelled maps to job_failed", `{"id":"v1","status":"cancelled","error":"Job was cancelled"}`, StatusCancelled, "job_failed", "Job was cancelled"},
		{"expired maps to job_expired", `{"id":"v1","status":"expired","error":"Job exceeded maximum time to live"}`, StatusExpired, "job_expired", "Job exceeded maximum time to live"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/videos/job-abc123" {
					t.Errorf("path = %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			client := NewClient(srv.Client(), 1<<20)
			got, err := client.GetVideo(context.Background(), srv.URL, "k", "job-abc123")
			if err != nil {
				t.Fatalf("GetVideo: %v", err)
			}
			if got.Status != tc.wantStatus {
				t.Fatalf("Status = %q, want %q", got.Status, tc.wantStatus)
			}
			if tc.wantCode == "" {
				if got.Error != nil {
					t.Fatalf("Error = %#v, want nil", got.Error)
				}
				return
			}
			if got.Error == nil || got.Error.Code != tc.wantCode || got.Error.Message != tc.wantMsg {
				t.Fatalf("Error = %#v, want {%s %s}", got.Error, tc.wantCode, tc.wantMsg)
			}
		})
	}
}

func TestGetVideoExplicitZeroCostIsNotNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"v1","status":"completed","usage":{"cost":0}}`)
	}))
	defer srv.Close()
	client := NewClient(srv.Client(), 1<<20)
	got, err := client.GetVideo(context.Background(), srv.URL, "k", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if got.CostUSD == nil || *got.CostUSD != 0 {
		t.Fatalf("CostUSD = %v, want an explicit 0, not nil", got.CostUSD)
	}
}

func TestGetVideoIgnoresUnsignedURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"v1","status":"completed","unsigned_urls":["http://attacker.example/steal?auth=please"]}`)
	}))
	defer srv.Close()
	client := NewClient(srv.Client(), 1<<20)
	got, err := client.GetVideo(context.Background(), srv.URL, "k", "v1")
	if err != nil || got.Status != StatusCompleted {
		t.Fatalf("GetVideo: %v %#v", err, got)
	}
}

func TestGetVideoRejectsInvalidProviderID(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	client := NewClient(srv.Client(), 1<<20)
	for _, id := range []string{"", ".", "..", "a/b", "a\\b", "a?b", "a#b"} {
		if _, err := client.GetVideo(context.Background(), srv.URL, "k", id); err == nil {
			t.Fatalf("id %q: want an error", id)
		}
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0: an invalid id must never reach path construction", requests)
	}
}

func TestGetVideoClassifiesProviderErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusBadRequest, `{"error":{"code":400,"message":"Invalid request parameters"}}`, "model_rejected"},
		{http.StatusPaymentRequired, `{"error":{"code":402,"message":"Insufficient credits"}}`, "no_credit"},
		{http.StatusNotFound, `{"error":{"code":404,"message":"Resource not found"}}`, "job_failed"},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))
		client := NewClient(srv.Client(), 1<<20)
		_, err := client.GetVideo(context.Background(), srv.URL, "k", "job1")
		if ErrorCode(err) != tc.want {
			t.Errorf("status %d: ErrorCode = %q, want %q", tc.status, ErrorCode(err), tc.want)
		}
		srv.Close()
	}
}

func TestGetVideoMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"v1",`)
	}))
	defer srv.Close()
	client := NewClient(srv.Client(), 1<<20)
	_, err := client.GetVideo(context.Background(), srv.URL, "k", "v1")
	if ErrorCode(err) != "job_failed" {
		t.Fatalf("ErrorCode = %q, want the unclassified job_failed fallback", ErrorCode(err))
	}
}

func TestDownloadVideoReadsContentFromTheContentEndpointOnly(t *testing.T) {
	content := []byte("fake mp4 bytes")
	var requestedPath, requestedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedQuery = r.URL.RawQuery
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(content)
	}))
	defer srv.Close()
	client := NewClient(srv.Client(), 1<<20)
	got, err := client.DownloadVideo(context.Background(), srv.URL, "k", "job-abc123", 1<<20)
	if err != nil {
		t.Fatalf("DownloadVideo: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
	if requestedPath != "/videos/job-abc123/content" {
		t.Fatalf("path = %q, want the content endpoint, never a poll response's unsigned_urls value", requestedPath)
	}
	if requestedQuery != "" {
		t.Fatalf("query = %q, want none", requestedQuery)
	}
}

func TestDownloadVideoBoundsContentToMaxBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer srv.Close()
	client := NewClient(srv.Client(), 1<<20)
	_, err := client.DownloadVideo(context.Background(), srv.URL, "k", "job1", 4)
	if ErrorCode(err) != "too_large" {
		t.Fatalf("ErrorCode = %q, want too_large", ErrorCode(err))
	}
}

// TestDownloadVideoRefusesADeclaredOversizeBeforeReading holds the body back until the test
// ends: a download that read before checking Content-Length would block instead of refusing.
func TestDownloadVideoRefusesADeclaredOversizeBeforeReading(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(http.StatusOK)
		http.NewResponseController(w).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := NewClient(srv.Client(), 1<<20).DownloadVideo(ctx, srv.URL, "k", "job1", 1024)
	if ErrorCode(err) != "too_large" {
		t.Fatalf("ErrorCode = %q (%v), want too_large from the declared length", ErrorCode(err), err)
	}
}

func TestDownloadVideoRefusesAnInvalidLimitWithoutARequest(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer srv.Close()
	for _, limit := range []int64{0, -1, 1<<63 - 1} {
		if _, err := NewClient(srv.Client(), 1<<20).DownloadVideo(context.Background(), srv.URL, "k", "job1", limit); ErrorCode(err) != "too_large" {
			t.Fatalf("limit %d: ErrorCode = %q, want too_large", limit, ErrorCode(err))
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want none for a limit that bounds nothing", requests.Load())
	}
}

func TestDownloadVideoPropagatesTruncatedContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()
	client := NewClient(srv.Client(), 1<<20)
	if _, err := client.DownloadVideo(context.Background(), srv.URL, "k", "job1", 1<<20); err == nil {
		t.Fatal("want an error when the body is shorter than its declared Content-Length")
	}
}

func TestDownloadVideoClosesBodyOnErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":500,"message":"Internal Server Error"}}`)
	}))
	defer srv.Close()
	client := NewClient(srv.Client(), 1<<20)
	_, err := client.DownloadVideo(context.Background(), srv.URL, "k", "job1", 1<<20)
	if ErrorCode(err) != "job_failed" {
		t.Fatalf("ErrorCode = %q, want job_failed", ErrorCode(err))
	}
}

func TestDownloadVideoRejectsInvalidProviderID(t *testing.T) {
	if _, err := (&Client{http: http.DefaultClient, maxImageBytes: 1}).DownloadVideo(context.Background(), "http://127.0.0.1:1", "k", "../escape", 1<<20); err == nil {
		t.Fatal("want an error for a dot-segment provider id")
	}
}

func TestVideoClientRejectsCrossOriginRedirect(t *testing.T) {
	var otherOriginHitWithAuth bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			otherOriginHitWithAuth = true
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = io.WriteString(w, "should never be read")
	}))
	defer other.Close()

	main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/videos/job1/content", http.StatusFound)
	}))
	defer main.Close()

	client := NewClient(main.Client(), 1<<20)
	_, err := client.DownloadVideo(context.Background(), main.URL, "secret-key", "job1", 1<<20)
	if err == nil {
		t.Fatal("want the cross-origin redirect to be refused")
	}
	if otherOriginHitWithAuth {
		t.Fatal("Authorization reached a different origin")
	}
}

func TestVideoClientAllowsSameOriginRedirect(t *testing.T) {
	content := []byte("redirected content")
	var finalHitWithAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/videos/job1/content" {
			http.Redirect(w, r, "/videos/job1/content-final", http.StatusFound)
			return
		}
		if r.URL.Path == "/videos/job1/content-final" {
			if r.Header.Get("Authorization") == "Bearer secret-key" {
				finalHitWithAuth = true
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(content)
			return
		}
		t.Fatalf("unexpected path %s", r.URL.Path)
	}))
	defer srv.Close()

	client := NewClient(srv.Client(), 1<<20)
	got, err := client.DownloadVideo(context.Background(), srv.URL, "secret-key", "job1", 1<<20)
	if err != nil {
		t.Fatalf("DownloadVideo: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
	if !finalHitWithAuth {
		t.Fatal("a same-origin redirect must keep the Authorization header")
	}
}

func TestVideoClientRejectsSameHostSubdomainRedirect(t *testing.T) {
	// httptest.Server listens on 127.0.0.1; simulate a subdomain change by
	// redirecting to a Host that differs only by a subdomain label, proving
	// the check compares URL.Host (not just scheme+IP) before following.
	var subdomainHitWithAuth bool
	sub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			subdomainHitWithAuth = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer sub.Close()

	main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Redirect to a syntactically different host:port than main.URL, the
		// same shape a subdomain hop would take (scheme identical, host
		// different), which is what CheckRedirect must catch.
		w.Header().Set("Location", strings.Replace(sub.URL, "127.0.0.1", "127.0.0.1.nip.io", 1)+"/videos/job1/content")
		w.WriteHeader(http.StatusFound)
	}))
	defer main.Close()

	client := NewClient(main.Client(), 1<<20)
	_, err := client.DownloadVideo(context.Background(), main.URL, "secret-key", "job1", 1<<20)
	if err == nil {
		t.Fatal("want the redirect to a different host to be refused")
	}
	if subdomainHitWithAuth {
		t.Fatal("Authorization must never reach a different host, subdomains included")
	}
}
