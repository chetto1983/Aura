package agui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// server_swarm_transcript_test.go pins SWARM-10's HTTP exposure: a foreign
// identity's conversation answers 404 (never 403, existence hidden — T-51-29), a
// malformed conversation id is the SAME 404 before any store round-trip, an unwired
// reader (nil, flag-off posture) also 404s, and the happy path serves the reader's
// bytes as application/x-ndjson.

// fakeSwarmTranscriptReader is a scripted swarmTranscriptReader double: it records
// the (conv, childID, offset) it was called with and returns the seeded
// (body, offset, err).
type fakeSwarmTranscriptReader struct {
	body                []byte
	offset              int64
	err                 error
	calls               int
	gotConv, gotChildID string
	gotOffset           int64
}

func (f *fakeSwarmTranscriptReader) ReadTranscript(_ context.Context, conv, childID string, fromOffset int64) ([]byte, int64, error) {
	f.calls++
	f.gotConv, f.gotChildID, f.gotOffset = conv, childID, fromOffset
	return f.body, f.offset, f.err
}

func swarmTranscriptTestServer(t *testing.T, store ConversationStore, reader swarmTranscriptReader) *httptest.Server {
	t.Helper()
	s := NewServer(&scriptedRunner{conv: store}, store, ServerConfig{})
	if reader != nil {
		s.SetSwarmTranscripts(reader)
	}
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	return srv
}

func transcriptPathFor(conv, childID string) string {
	return "/api/conversations/" + conv + "/swarm/" + childID + "/transcript"
}

// TestSwarmTranscriptRouteForeignIdentityIs404 proves a foreign identity's
// conversation answers 404 — the SAME status handleGetConversation uses, hiding
// existence rather than a distinguishable 403 (T-51-29).
func TestSwarmTranscriptRouteForeignIdentityIs404(t *testing.T) {
	foreignOwner := uuid.Must(uuid.NewV7()).String() // NOT local → the caller does not own it
	store := newOwnerConvStore(goodID, foreignOwner)
	reader := &fakeSwarmTranscriptReader{body: []byte(`{"ok":true}` + "\n")}
	srv := swarmTranscriptTestServer(t, store, reader)

	code, _ := req(t, srv, http.MethodGet, transcriptPathFor(goodID, "w1"), "")
	if code != http.StatusNotFound {
		t.Fatalf("foreign-identity transcript read status = %d, want 404", code)
	}
	if reader.calls != 0 {
		t.Fatalf("ReadTranscript must not be called before ownership is proven, got %d calls", reader.calls)
	}
}

// TestSwarmTranscriptRouteMalformedConvIs404 proves a non-UUID conv id answers the
// same 404 before any store round-trip (mirrors parseConvID's T-25-02 gate).
func TestSwarmTranscriptRouteMalformedConvIs404(t *testing.T) {
	store := newOwnerConvStore(goodID, localIdentityID)
	reader := &fakeSwarmTranscriptReader{}
	srv := swarmTranscriptTestServer(t, store, reader)

	code, _ := req(t, srv, http.MethodGet, transcriptPathFor("not-a-uuid", "w1"), "")
	if code != http.StatusNotFound {
		t.Fatalf("malformed conv id status = %d, want 404", code)
	}
	if reader.calls != 0 {
		t.Fatalf("ReadTranscript must not be called for a malformed conv id, got %d calls", reader.calls)
	}
}

// TestSwarmTranscriptRouteNilReaderIs404 proves the flag-off posture: with no
// SetSwarmTranscripts call, the route hides itself even for the caller's own,
// otherwise-resolvable conversation — mirroring SetGraphView's best-effort 503-like
// posture (404 here, since the route is owner-scoped by conv id).
func TestSwarmTranscriptRouteNilReaderIs404(t *testing.T) {
	store := newOwnerConvStore(goodID, localIdentityID)
	srv := swarmTranscriptTestServer(t, store, nil)

	code, _ := req(t, srv, http.MethodGet, transcriptPathFor(goodID, "w1"), "")
	if code != http.StatusNotFound {
		t.Fatalf("nil-reader transcript read status = %d, want 404", code)
	}
}

// TestSwarmTranscriptRouteServesBody proves the happy path: an owned conversation
// with a wired reader serves the reader's bytes as application/x-ndjson, and the
// reader is invoked with the resolved (conv, childID) and the requested offset.
func TestSwarmTranscriptRouteServesBody(t *testing.T) {
	store := newOwnerConvStore(goodID, localIdentityID)
	reader := &fakeSwarmTranscriptReader{body: []byte(`{"seq":1}` + "\n"), offset: 42}
	srv := swarmTranscriptTestServer(t, store, reader)

	resp, err := http.Get(srv.URL + transcriptPathFor(goodID, "w1") + "?offset=7")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("Content-Type = %q, want application/x-ndjson", ct)
	}
	if string(body) != `{"seq":1}`+"\n" {
		t.Fatalf("body = %q, want the reader's seeded bytes", body)
	}
	if reader.calls != 1 || reader.gotConv != goodID || reader.gotChildID != "w1" || reader.gotOffset != 7 {
		t.Fatalf("reader called with conv=%q childID=%q offset=%d (calls=%d), want conv=%q childID=w1 offset=7",
			reader.gotConv, reader.gotChildID, reader.gotOffset, reader.calls, goodID)
	}
}

// TestSwarmTranscriptRouteReaderErrorIs404 proves a ReadTranscript failure (a
// hostile/rejected childID, or an I/O error) never leaks detail — it renders the
// SAME opaque 404 as an ownership miss, so the wire never distinguishes "hostile
// input" from "not found" (T-51-28/29).
func TestSwarmTranscriptRouteReaderErrorIs404(t *testing.T) {
	store := newOwnerConvStore(goodID, localIdentityID)
	reader := &fakeSwarmTranscriptReader{err: errors.New("swarm: child id contains a path separator")}
	srv := swarmTranscriptTestServer(t, store, reader)

	code, body := req(t, srv, http.MethodGet, transcriptPathFor(goodID, "bad-child"), "")
	if code != http.StatusNotFound {
		t.Fatalf("reader-error status = %d, want 404", code)
	}
	if body == "" {
		t.Fatalf("expected a body")
	}
}
