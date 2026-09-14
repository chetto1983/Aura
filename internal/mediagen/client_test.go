package mediagen

import (
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestNewClientNeverMutatesTheCallerHTTPClient(t *testing.T) {
	original := &http.Client{}
	_ = NewClient(original, 1<<20)
	if original.CheckRedirect != nil {
		t.Fatal("NewClient must not set CheckRedirect on the caller's *http.Client")
	}
}

func TestNewClientAcceptsNilHTTPClient(t *testing.T) {
	c := NewClient(nil, 1<<20)
	if c.http == nil {
		t.Fatal("NewClient(nil, ...) must still produce a usable http.Client")
	}
}

func TestReadCappedRejectsInvalidLimits(t *testing.T) {
	for _, limit := range []int64{0, -1, 1<<63 - 1} {
		if _, err := readCapped(nil, limit); ErrorCode(err) != "too_large" {
			t.Fatalf("limit %d: ErrorCode = %q, want too_large", limit, ErrorCode(err))
		}
	}
}

func TestReadCappedEnforcesTheLimit(t *testing.T) {
	r := &fakeReader{data: []byte("0123456789")}
	if _, err := readCapped(r, 4); ErrorCode(err) != "too_large" {
		t.Fatalf("ErrorCode = %q, want too_large", ErrorCode(err))
	}
}

func TestReadCappedPropagatesReadErrors(t *testing.T) {
	wantErr := errors.New("boom")
	r := &fakeReader{err: wantErr}
	if _, err := readCapped(r, 10); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want it to wrap %v", err, wantErr)
	}
}

func TestValidProviderIDRejectsUnsafeIDs(t *testing.T) {
	for _, id := range []string{"", ".", "..", "a/b", "a\\b", "a?b", "a#b"} {
		if _, err := validProviderID(id); err == nil {
			t.Fatalf("id %q: want an error", id)
		}
	}
	if got, err := validProviderID("job-abc123"); err != nil || got != "job-abc123" {
		t.Fatalf("validProviderID(job-abc123) = %q, %v", got, err)
	}
}

func TestBoundedMessageCapsLength(t *testing.T) {
	long := make([]byte, 600)
	for i := range long {
		long[i] = 'x'
	}
	got := boundedMessage(string(long))
	if len(got) != 503 { // 500 chars + "..."
		t.Fatalf("len(got) = %d, want 503", len(got))
	}
}

// fakeReader lets a test control exactly what io.Reader returns without a
// real network or file body.
type fakeReader struct {
	data []byte
	err  error
}

func (r *fakeReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}
