package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
)

// Unit tier for the `/compact` routes: the two capabilities are read off s.run / s.conv by
// type assertion, so the interesting cases are exactly the ones a DB cannot show — a driver
// that cannot compact, a conversation with nothing to compact, and a thread the caller does
// not own. The summarizer round-trip itself belongs to conversations.Store.

// compactingRunner is a Runner that CAN compact. The base scriptedRunner deliberately
// cannot, so it doubles as the "unavailable" case.
type compactingRunner struct {
	*scriptedRunner
	result conversations.CompactionResult
	err    error
	calls  int
	gotID  string
}

func (c *compactingRunner) CompactConversation(_ context.Context, conversationID string) (conversations.CompactionResult, error) {
	c.calls++
	c.gotID = conversationID
	return c.result, c.err
}

// compactionAwareConv is a ConversationStore that can read the durable summary back.
type compactionAwareConv struct {
	*fakeConvStore
	stored  conversations.Compaction
	present bool
	loadErr error
}

func (c *compactionAwareConv) LoadCompaction(_ context.Context, _, _ string) (conversations.Compaction, bool, error) {
	if c.loadErr != nil {
		return conversations.Compaction{}, false, c.loadErr
	}
	return c.stored, c.present, nil
}

func knownConv() *fakeConvStore {
	return &fakeConvStore{known: map[string]bool{goodID: true}}
}

func doCompactionReq(t *testing.T, srv *httptest.Server, method, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(body)
}

func TestCompactConversation_ReturnsWhatTheCompactionDid(t *testing.T) {
	run := &compactingRunner{
		scriptedRunner: &scriptedRunner{},
		result: conversations.CompactionResult{
			Summary: "the thread so far", CoversThroughSeq: 12, SourceTurns: 9,
			TokensBefore: 41000, TokensAfter: 2600,
		},
	}
	srv := newTestServer(t, run, knownConv())

	code, body := doCompactionReq(t, srv, http.MethodPost, "/api/conversations/"+goodID+"/compact")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", code, body)
	}
	var dto compactionDTO
	if err := json.Unmarshal([]byte(body), &dto); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if run.calls != 1 || run.gotID != goodID {
		t.Fatalf("compactor calls = %d for %q, want exactly one for the thread", run.calls, run.gotID)
	}
	want := compactionDTO{
		CoversThroughSeq: 12, SourceTurns: 9, Summary: "the thread so far",
		TokensBefore: 41000, TokensAfter: 2600,
	}
	if dto != want {
		t.Fatalf("dto = %+v, want %+v", dto, want)
	}
}

// A thread with no earlier turns is a state change that did NOT happen; a 200 carrying a
// zero result would read in the composer exactly like a compaction that worked.
func TestCompactConversation_NothingToCompactIsAConflict(t *testing.T) {
	run := &compactingRunner{scriptedRunner: &scriptedRunner{}, err: conversations.ErrNothingToCompact}
	srv := newTestServer(t, run, knownConv())

	code, body := doCompactionReq(t, srv, http.MethodPost, "/api/conversations/"+goodID+"/compact")
	if code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", code, body)
	}
	if !strings.Contains(body, "no earlier turns") {
		t.Fatalf("body = %q, want the reason the operator can act on", body)
	}
}

// A summary longer than the turns it replaces would be a permanent tax, since a stored
// compaction stays in force. Refusing it is the command doing its job, not failing.
func TestCompactConversation_NotWorthwhileIsUnprocessable(t *testing.T) {
	run := &compactingRunner{
		scriptedRunner: &scriptedRunner{}, err: conversations.ErrCompactionNotWorthwhile,
	}
	srv := newTestServer(t, run, knownConv())

	code, body := doCompactionReq(t, srv, http.MethodPost, "/api/conversations/"+goodID+"/compact")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", code, body)
	}
	if !strings.Contains(body, "nothing to gain") {
		t.Fatalf("body = %q, want the trade the operator is being told about", body)
	}
}

// A summarizer that does not answer is an UPSTREAM failure, and the operator's line about it
// has to say that nothing was lost. This surfaced as a bare 500 in the cockpit on 2026-08-17.
func TestCompactConversation_SummarizerFailureIsABadGateway(t *testing.T) {
	run := &compactingRunner{
		scriptedRunner: &scriptedRunner{},
		err:            fmt.Errorf("compact %s: %w", goodID, conversations.ErrCompactionFailed),
	}
	srv := newTestServer(t, run, knownConv())

	code, body := doCompactionReq(t, srv, http.MethodPost, "/api/conversations/"+goodID+"/compact")
	if code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", code, body)
	}
	if !strings.Contains(body, "nothing in this conversation was changed") {
		t.Fatalf("body = %q, want the reassurance the operator needs", body)
	}
}

func TestCompactConversation_Unavailable(t *testing.T) {
	for name, run := range map[string]Runner{
		// A runner with no compaction seam at all (an older driver, a scripted fake).
		"driver cannot compact": &scriptedRunner{},
		// A deployment with compaction switched off: no summarizer, so no summary.
		"no summarizer wired": &compactingRunner{
			scriptedRunner: &scriptedRunner{}, err: conversations.ErrCompactionUnavailable,
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newTestServer(t, run, knownConv())
			code, body := doCompactionReq(t, srv, http.MethodPost, "/api/conversations/"+goodID+"/compact")
			if code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503: %s", code, body)
			}
		})
	}
}

// Owner scoping (MUSR-01 / D-06): a thread the caller does not own is 404 on BOTH routes,
// and the compactor is never reached — the gate precedes the work.
func TestCompactionRoutes_UnknownThreadIs404BeforeAnyWork(t *testing.T) {
	run := &compactingRunner{scriptedRunner: &scriptedRunner{}}
	srv := newTestServer(t, run, &fakeConvStore{})

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/conversations/" + goodID + "/compact"},
		{http.MethodGet, "/api/conversations/" + goodID + "/compaction"},
		// A non-UUID can never name a conversation: 404 before the store round-trip.
		{http.MethodPost, "/api/conversations/not-a-uuid/compact"},
		{http.MethodGet, "/api/conversations/not-a-uuid/compaction"},
	} {
		code, body := doCompactionReq(t, srv, tc.method, tc.path)
		if code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404: %s", tc.method, tc.path, code, body)
		}
	}
	if run.calls != 0 {
		t.Fatalf("compactor called %d times for a thread the caller does not own", run.calls)
	}
}

func TestGetCompaction_ProjectsTheStoredSummary(t *testing.T) {
	conv := &compactionAwareConv{
		fakeConvStore: knownConv(),
		present:       true,
		stored: conversations.Compaction{
			Summary: "condensed", CoversThroughSeq: 7, SourceTurns: 5,
		},
	}
	srv := newTestServer(t, &scriptedRunner{}, conv)

	code, body := doCompactionReq(t, srv, http.MethodGet, "/api/conversations/"+goodID+"/compaction")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", code, body)
	}
	var dto compactionDTO
	if err := json.Unmarshal([]byte(body), &dto); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	want := compactionDTO{CoversThroughSeq: 7, SourceTurns: 5, Summary: "condensed"}
	if dto != want {
		t.Fatalf("dto = %+v, want %+v", dto, want)
	}
}

// Never compacted, and a store with no read seam, are the SAME answer: the zero DTO. The
// marker is an annotation on the transcript — refusing to answer would make the client treat
// the normal state of most threads as a failure.
func TestGetCompaction_AbsenceIsTheZeroDTO(t *testing.T) {
	for name, conv := range map[string]ConversationStore{
		"never compacted": &compactionAwareConv{fakeConvStore: knownConv()},
		"no read seam":    knownConv(),
	} {
		t.Run(name, func(t *testing.T) {
			srv := newTestServer(t, &scriptedRunner{}, conv)
			code, body := doCompactionReq(t, srv, http.MethodGet, "/api/conversations/"+goodID+"/compaction")
			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", code, body)
			}
			var dto compactionDTO
			if err := json.Unmarshal([]byte(body), &dto); err != nil {
				t.Fatalf("decode: %v (%s)", err, body)
			}
			if dto != (compactionDTO{}) {
				t.Fatalf("dto = %+v, want the zero DTO", dto)
			}
		})
	}
}

func TestGetCompaction_UnreadableRowIsAnError(t *testing.T) {
	conv := &compactionAwareConv{fakeConvStore: knownConv(), loadErr: errors.New("boom")}
	srv := newTestServer(t, &scriptedRunner{}, conv)

	code, _ := doCompactionReq(t, srv, http.MethodGet, "/api/conversations/"+goodID+"/compaction")
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", code)
	}
}
