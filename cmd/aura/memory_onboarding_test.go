package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/onboarding"
)

type recordedCall struct {
	tool string
	args map[string]any
}

// fakeTransport is a recording mcp.Transport. It counts its own Close so the unit tier can
// assert the open/close pairing; the SESSION count itself is proven at the wire in
// TestStoreConfirmedOpensOneMCPSessionForTheWholeSubmission, because a fake counting its
// own invocations is not evidence about the protocol.
type fakeTransport struct {
	mu      sync.Mutex
	calls   []recordedCall
	closes  int
	callErr error
	result  string
}

func (f *fakeTransport) CallTool(_ context.Context, name string, args map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, recordedCall{tool: name, args: args})
	if f.callErr != nil {
		return "", f.callErr
	}
	return f.result, nil
}

func (f *fakeTransport) ListTools(context.Context) ([]mcp.ToolDef, error) { return nil, nil }
func (f *fakeTransport) Ping(context.Context) error                       { return nil }

func (f *fakeTransport) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	return nil
}

func (f *fakeTransport) recorded() []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedCall(nil), f.calls...)
}

func (f *fakeTransport) closeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closes
}

// openCounter is the seam fixture: it hands out ONE fakeTransport and counts how many times
// the store asked to open a connection.
type openCounter struct {
	transport *fakeTransport
	opens     int
	openErr   error
}

func (o *openCounter) open(context.Context) (mcp.Transport, error) {
	o.opens++
	if o.openErr != nil {
		return nil, o.openErr
	}
	return o.transport, nil
}

// fakeStore builds a memoryProfileStore over the recording open seam and a fixed clock.
func fakeStore() (*memoryProfileStore, *openCounter) {
	oc := &openCounter{transport: &fakeTransport{result: `{"stored":true}`}}
	m := &memoryProfileStore{
		open: oc.open,
		now:  func() time.Time { return time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC) },
	}
	return m, oc
}

// fullSeedAnswers is the amendment's worked example: the six typed fields, which
// MapProfile projects onto 3 entities + 4 facts + 1 preference, plus the sentinel = 9.
var fullSeedAnswers = onboarding.Answers{
	Name: "Davide", Lang: "it", Location: "Caraglio",
	Timezone: "Europe/Rome", Role: "founder", Company: "PmSync",
}

func TestStoreConfirmedScopesEveryCallAndEndsWithSentinel(t *testing.T) {
	m, oc := fakeStore()
	if err := m.StoreConfirmed(context.Background(), "id-uuid", fullSeedAnswers); err != nil {
		t.Fatalf("StoreConfirmed: %v", err)
	}
	calls := oc.transport.recorded()
	if len(calls) != 9 {
		t.Fatalf("memory calls = %d, want 9 (3 entities + 4 facts + 1 preference + 1 sentinel)", len(calls))
	}
	// EVERY call must carry user_identifier = identityID. This stamp is the ONLY thing
	// keeping these writes out of the memory server's fail-open GLOBAL scope, because the
	// path calls the transport directly and so bypasses scopeMemoryArgs.
	for _, c := range calls {
		if c.args["user_identifier"] != "id-uuid" {
			t.Errorf("call %q missing user_identifier=id-uuid: %#v", c.tool, c.args)
		}
	}
	if calls[0].tool != "memory_add_entity" {
		t.Errorf("first call = %q, want memory_add_entity (entities before the facts that name them)", calls[0].tool)
	}
	last := calls[len(calls)-1]
	if last.tool != "memory_add_fact" || last.args["predicate"] != onboarding.PredicateOnboardingCompleted {
		t.Errorf("last call = %q pred=%v, want completed sentinel", last.tool, last.args["predicate"])
	}
	if last.args["subject"] != "id-uuid" {
		t.Errorf("sentinel subject = %v, want identity UUID", last.args["subject"])
	}
}

// TestStoreConfirmedStoresTheNameByteIdentically is the defect Amendment #95 exists for:
// the interview's LLM wrote "David" for a typed "Davide". Nothing on this path may trim,
// fold or rewrite the submitted name.
func TestStoreConfirmedStoresTheNameByteIdentically(t *testing.T) {
	for _, name := range []string{"Davide", "José-María", "Anne-Marie O'Brien"} {
		m, oc := fakeStore()
		if err := m.StoreConfirmed(context.Background(), "id-uuid", onboarding.Answers{Name: name}); err != nil {
			t.Fatalf("StoreConfirmed(%q): %v", name, err)
		}
		calls := oc.transport.recorded()
		if calls[0].tool != "memory_add_entity" || calls[0].args["name"] != name {
			t.Fatalf("stored entity name = %v, want byte-identical %q", calls[0].args["name"], name)
		}
		// The PERSON entity is the subject of every fact the mapper emits.
		for _, c := range calls[1:] {
			if c.tool == "memory_add_fact" && c.args["predicate"] != onboarding.PredicateOnboardingCompleted &&
				c.args["subject"] != name {
				t.Errorf("fact %v subject = %v, want the typed name %q", c.args["predicate"], c.args["subject"], name)
			}
		}
	}
}

// TestStoreConfirmedWritesNoMessageNode is the inverted assertion. It used to require the
// raw Agent.md draft be stored via memory_store_message; Amendment #95 DELETES that safety
// net (it produced the graph's only unreadable artifact and 0 MENTIONS edges), so the
// contract is now that onboarding writes no :Message node at all. The test changed because
// the contract changed, not because it was broken.
func TestStoreConfirmedWritesNoMessageNode(t *testing.T) {
	m, oc := fakeStore()
	if err := m.StoreConfirmed(context.Background(), "id-uuid", fullSeedAnswers); err != nil {
		t.Fatalf("StoreConfirmed: %v", err)
	}
	if containsToolCall(oc.transport.recorded(), "memory_store_message") {
		t.Error("onboarding wrote a :Message node; the raw-draft safety net is deleted (Amendment #95)")
	}
}

// TestStoreConfirmedOpensOneSessionSeam is the fast unit variant of the one-connection
// gate: the store must ask for a transport exactly once and close it exactly once, no
// matter how many writes the seed maps to.
func TestStoreConfirmedOpensOneSessionSeam(t *testing.T) {
	m, oc := fakeStore()
	if err := m.StoreConfirmed(context.Background(), "id-uuid", fullSeedAnswers); err != nil {
		t.Fatalf("StoreConfirmed: %v", err)
	}
	if oc.opens != 1 {
		t.Errorf("opens = %d, want 1 for the whole submission", oc.opens)
	}
	if got := oc.transport.closeCount(); got != 1 {
		t.Errorf("closes = %d, want 1", got)
	}
}

func TestStoreSkippedWritesOnlySkippedSentinel(t *testing.T) {
	m, oc := fakeStore()
	if err := m.StoreSkipped(context.Background(), "id-uuid"); err != nil {
		t.Fatalf("StoreSkipped: %v", err)
	}
	calls := oc.transport.recorded()
	if len(calls) != 1 {
		t.Fatalf("StoreSkipped calls = %d, want 1 — skipping stays cheap", len(calls))
	}
	c := calls[0]
	if c.tool != "memory_add_fact" || c.args["predicate"] != onboarding.PredicateOnboardingSkipped || c.args["user_identifier"] != "id-uuid" {
		t.Errorf("skip sentinel = %#v", c.args)
	}
	if oc.opens != 1 || oc.transport.closeCount() != 1 {
		t.Errorf("skip opened %d / closed %d sessions, want 1/1", oc.opens, oc.transport.closeCount())
	}
}

func TestStoreConfirmedEmptyIdentityRefused(t *testing.T) {
	m, oc := fakeStore()
	if err := m.StoreConfirmed(context.Background(), "", onboarding.Answers{Name: "x"}); err == nil {
		t.Fatal("StoreConfirmed with empty identity must error, not hit global scope")
	}
	if err := m.StoreSkipped(context.Background(), ""); err == nil {
		t.Fatal("StoreSkipped with empty identity must error, not hit global scope")
	}
	if oc.opens != 0 {
		t.Errorf("an empty identity opened %d session(s); it must fail before the sidecar", oc.opens)
	}
}

// TestStoreConfirmedSurfacesSessionAndWriteFailures pins the two failure legs: an
// unreachable sidecar fails at open, and a rejected write aborts the remaining ones (a
// partially-seeded graph with a completion sentinel would be worse than none).
func TestStoreConfirmedSurfacesSessionAndWriteFailures(t *testing.T) {
	t.Run("open failure", func(t *testing.T) {
		m := &memoryProfileStore{
			open: func(context.Context) (mcp.Transport, error) { return nil, errors.New("sidecar down") },
			now:  time.Now,
		}
		if err := m.StoreConfirmed(context.Background(), "id-uuid", fullSeedAnswers); err == nil {
			t.Fatal("an unopenable memory session must surface an error")
		}
	})

	t.Run("write failure aborts the rest", func(t *testing.T) {
		tr := &fakeTransport{callErr: errors.New("write rejected")}
		oc := &openCounter{transport: tr}
		m := &memoryProfileStore{open: oc.open, now: time.Now}
		if err := m.StoreConfirmed(context.Background(), "id-uuid", fullSeedAnswers); err == nil {
			t.Fatal("a rejected write must surface an error")
		}
		if got := len(tr.recorded()); got != 1 {
			t.Errorf("calls after a failed write = %d, want 1 (abort, never stamp the sentinel)", got)
		}
		if tr.closeCount() != 1 {
			t.Errorf("a failed submission leaked the session (closes = %d)", tr.closeCount())
		}
	})
}

func TestStatusScansSentinelPredicates(t *testing.T) {
	cases := []struct {
		name         string
		facts        string
		wantC, wantS bool
		expectErr    bool
	}{
		{"completed", `{"facts":[{"predicate":"onboarding_completed"}]}`, true, false, false},
		{"skipped", `{"facts":[{"predicate":"onboarding_skipped"}]}`, false, true, false},
		{"none", `{"facts":[{"predicate":"role"}]}`, false, false, false},
		{"empty", `{"facts":[]}`, false, false, false},
		{"server-error", `{"error":"boom"}`, false, false, true},
		{"bad-json", `not json`, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &fakeTransport{result: tc.facts}
			oc := &openCounter{transport: tr}
			m := &memoryProfileStore{open: oc.open, now: time.Now}
			st, err := m.Status(context.Background(), "id")
			if tc.expectErr != (err != nil) {
				t.Fatalf("err = %v, expectErr = %v", err, tc.expectErr)
			}
			if st.Completed != tc.wantC || st.Skipped != tc.wantS {
				t.Errorf("state = %+v, want completed=%v skipped=%v", st, tc.wantC, tc.wantS)
			}
			calls := tr.recorded()
			if len(calls) != 1 || calls[0].args["user_identifier"] != "id" {
				t.Errorf("status read = %#v, want one user_identifier-scoped call", calls)
			}
		})
	}
}

// TestStatusEmptyIdentityIsUnonboarded proves the anonymous path never touches the sidecar.
func TestStatusEmptyIdentityIsUnonboarded(t *testing.T) {
	m, oc := fakeStore()
	st, err := m.Status(context.Background(), "")
	if err != nil || st.Completed || st.Skipped {
		t.Fatalf("Status(\"\") = %+v err=%v, want a zero state and no error", st, err)
	}
	if oc.opens != 0 {
		t.Errorf("an empty identity opened %d session(s), want 0", oc.opens)
	}
}

func containsToolCall(calls []recordedCall, tool string) bool {
	for _, c := range calls {
		if c.tool == tool {
			return true
		}
	}
	return false
}
