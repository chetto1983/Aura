package objectstore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const getFromContent = "0123456789"

// s3RangeServer stands in for Garage on the two requests GetFrom and Put make. An offset at or
// past the end is answered the way Garage answers it (src/api/s3/get.rs parse_range_header ->
// error.rs InvalidRange): 416 with an InvalidRange XML body and Content-Range bytes */size.
type s3RangeServer struct {
	mu      sync.Mutex
	objects map[string][]byte
	ranges  []string
}

func (s *s3RangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		data, _ := io.ReadAll(r.Body)
		s.objects[r.URL.Path] = data
		w.Header().Set("ETag", `"etag"`)
		return
	case http.MethodGet:
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.ranges = append(s.ranges, r.Header.Get("Range"))
	if strings.HasSuffix(r.URL.Path, "/forbidden") {
		writeS3Error(w, http.StatusForbidden, "AccessDenied")
		return
	}
	data, ok := s.objects[r.URL.Path]
	if !ok {
		writeS3Error(w, http.StatusNotFound, "NoSuchKey")
		return
	}
	spec := r.Header.Get("Range")
	if spec == "" {
		_, _ = w.Write(data)
		return
	}
	first, _ := strings.CutPrefix(spec, "bytes=")
	first, _ = strings.CutSuffix(first, "-")
	start, err := strconv.Atoi(first)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if start >= len(data) {
		w.Header().Set("Content-Range", "bytes */"+strconv.Itoa(len(data)))
		writeS3Error(w, http.StatusRequestedRangeNotSatisfiable, "InvalidRange")
		return
	}
	w.Header().Set("Content-Range", "bytes "+first+"-"+strconv.Itoa(len(data)-1)+"/"+strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(data[start:])
}

func writeS3Error(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, "<Error><Code>"+code+"</Code><Message>"+code+"</Message></Error>")
}

func newS3RangeStore(t *testing.T) (*S3Store, *s3RangeServer) {
	t.Helper()
	fake := &s3RangeServer{objects: map[string][]byte{}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	store, err := NewS3(context.Background(), S3Config{
		Endpoint:  srv.URL,
		Region:    "garage",
		AccessKey: "test",
		SecretKey: "test",
		PathStyle: true,
	})
	if err != nil {
		t.Fatalf("NewS3() error = %v", err)
	}
	return store, fake
}

func readGetFrom(t *testing.T, store Store, ref ObjectRef, offset int64) string {
	t.Helper()
	body, err := store.GetFrom(context.Background(), ref, offset)
	if err != nil {
		t.Fatalf("GetFrom(%d) error = %v", offset, err)
	}
	defer func() {
		if err := body.Close(); err != nil {
			t.Fatalf("close GetFrom(%d) body: %v", offset, err)
		}
	}()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read GetFrom(%d) body: %v", offset, err)
	}
	return string(got)
}

// Every store honours one contract: the bytes from the offset to the end, an empty body (not an
// error) at or past the end, an error for a negative offset, and a missing object stays a
// not-found error even when the offset alone would have been unsatisfiable.
func TestGetFromReadsFromTheOffsetToTheEndOnEveryStore(t *testing.T) {
	stores := map[string]func(t *testing.T) Store{
		"fake":       func(*testing.T) Store { return NewFake() },
		"filesystem": func(t *testing.T) Store { return NewFilesystem(t.TempDir()) },
		"s3": func(t *testing.T) Store {
			store, _ := newS3RangeStore(t)
			return store
		},
	}
	for name, newStore := range stores {
		t.Run(name, func(t *testing.T) {
			store := newStore(t)
			ctx := context.Background()
			ref := ObjectRef{Bucket: "bucket", Key: "identity/a/asset/clip.mp4"}
			if _, err := store.Put(ctx, ref, strings.NewReader(getFromContent),
				PutOptions{MIMEType: "video/mp4", Size: int64(len(getFromContent))}); err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			for _, tc := range []struct {
				offset int64
				want   string
			}{
				{0, getFromContent},
				{4, "456789"},
				{9, "9"},
				{10, ""},
				{25, ""},
			} {
				if got := readGetFrom(t, store, ref, tc.offset); got != tc.want {
					t.Fatalf("GetFrom(%d) = %q, want %q", tc.offset, got, tc.want)
				}
			}
			if body, err := store.GetFrom(ctx, ref, -1); err == nil {
				_ = body.Close()
				t.Fatal("GetFrom(-1) error = nil, want a negative-offset refusal")
			}
			missing := ObjectRef{Bucket: "bucket", Key: "identity/a/asset/missing.mp4"}
			if _, err := store.GetFrom(ctx, missing, 3); !IsNotFound(err) {
				t.Fatalf("GetFrom(missing) error = %v, want not found", err)
			}
		})
	}
}

// The ranged request is the whole point: a mid-object offset must reach S3 as an open-ended
// Range, and offset zero must stay a plain GET so an empty object never draws a 416.
func TestS3GetFromSendsAnOpenEndedRangeOnlyPastTheStart(t *testing.T) {
	store, fake := newS3RangeStore(t)
	ref := ObjectRef{Bucket: "bucket", Key: "clip.mp4"}
	if _, err := store.Put(context.Background(), ref, strings.NewReader(getFromContent),
		PutOptions{MIMEType: "video/mp4", Size: int64(len(getFromContent))}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	readGetFrom(t, store, ref, 0)
	readGetFrom(t, store, ref, 7)
	readGetFrom(t, store, ref, 10)
	want := []string{"", "bytes=7-", "bytes=10-"}
	if strings.Join(fake.ranges, ",") != strings.Join(want, ",") {
		t.Fatalf("Range headers = %q, want %q", fake.ranges, want)
	}
}

// Only a 416 means "past the end". Any other refusal must still surface, or a permissions
// fault would stream as an empty clip.
func TestS3GetFromKeepsOtherStoreErrors(t *testing.T) {
	store, _ := newS3RangeStore(t)
	body, err := store.GetFrom(context.Background(), ObjectRef{Bucket: "bucket", Key: "forbidden"}, 4)
	if err == nil {
		_ = body.Close()
		t.Fatal("GetFrom(forbidden) error = nil, want the 403 surfaced")
	}
}

func TestGetFromHonoursACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ref := ObjectRef{Bucket: "bucket", Key: "clip.mp4"}
	for name, store := range map[string]Store{"fake": NewFake(), "filesystem": NewFilesystem(t.TempDir())} {
		if _, err := store.GetFrom(ctx, ref, 0); err == nil {
			t.Fatalf("%s GetFrom(cancelled) error = nil, want context error", name)
		}
	}
}
