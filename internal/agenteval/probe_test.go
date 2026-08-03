package agenteval

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeDriver struct {
	conv string
	err  error
	got  string
}

func (f *fakeDriver) Ask(_ context.Context, q string) (string, error) {
	f.got = q
	return f.conv, f.err
}

type fakeReader struct {
	evidence Evidence
	err      error
	gotConv  string
}

func (f *fakeReader) Read(_ context.Context, conv string) (Evidence, error) {
	f.gotConv = conv
	return f.evidence, f.err
}

// TestRunThreadsTheConversationFromAskToRead: the evidence must be read from the
// conversation the question actually landed in. Reading a stale one is how a gate
// reports yesterday's behaviour as today's.
func TestRunThreadsTheConversationFromAskToRead(t *testing.T) {
	t.Parallel()
	d := &fakeDriver{conv: "019fc8bc-2f42-7b5c-b783-40329b0399a0"}
	r := &fakeReader{evidence: Evidence{Answer: "632587", LLMCalls: 3}}
	c := Case{Name: "x", Question: "dammi il codice", AnswerContains: []string{"632587"}, MaxLLMCalls: 5}

	evidence, failures, err := Run(context.Background(), d, r, c)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if d.got != c.Question {
		t.Errorf("driver got %q, want the case question %q", d.got, c.Question)
	}
	if r.gotConv != d.conv {
		t.Errorf("evidence read from %q, want the conversation the ask returned (%q)", r.gotConv, d.conv)
	}
	if len(failures) != 0 || evidence.LLMCalls != 3 {
		t.Errorf("failures=%v evidence=%+v", failures, evidence)
	}
}

// TestRunNamesTheCaseOnFailure: a bare "exec failed" in a suite of live turns
// costs a re-run to find out which one.
func TestRunNamesTheCaseOnFailure(t *testing.T) {
	t.Parallel()
	c := Case{Name: "customer-code", Question: "q"}
	_, _, err := Run(context.Background(), &fakeDriver{err: errors.New("boom")}, &fakeReader{}, c)
	if err == nil || !strings.Contains(err.Error(), "customer-code") {
		t.Fatalf("ask error does not name the case: %v", err)
	}
	_, _, err = Run(context.Background(),
		&fakeDriver{conv: "abc"}, &fakeReader{err: errors.New("psql down")}, c)
	if err == nil || !strings.Contains(err.Error(), "customer-code") || !strings.Contains(err.Error(), "abc") {
		t.Fatalf("read error does not name the case and conversation: %v", err)
	}
}

func TestParseEvidence(t *testing.T) {
	t.Parallel()
	// psql echoes "SET" ahead of the payload; the parser must take the LAST
	// non-empty line, not the first.
	raw := "SET\n" + `[
 {"seq":1,"role":"user","content":"dammi il codice","tools":[]},
 {"seq":2,"role":"assistant","content":"","tools":["tool_search"]},
 {"seq":3,"role":"tool","content":"{\"facts\":[]}","tools":[]},
 {"seq":4,"role":"assistant","content":"","tools":["memory__memory_search","document_open"]},
 {"seq":5,"role":"tool","content":"{\"facts\":[{\"a\":1},{\"b\":2}]}","tools":[]},
 {"seq":6,"role":"tool","content":"not json at all","tools":[]},
 {"seq":7,"role":"assistant","content":"Il codice è 632587.","tools":[]}
]` + "\n\n"

	e, err := parseEvidence(raw)
	if err != nil {
		t.Fatalf("parseEvidence: %v", err)
	}
	if e.LLMCalls != 3 {
		t.Errorf("LLMCalls = %d, want 3 (assistant turns only)", e.LLMCalls)
	}
	if want := []string{"tool_search", "memory__memory_search", "document_open"}; !equal(e.Tools, want) {
		t.Errorf("Tools = %v, want %v in call order", e.Tools, want)
	}
	// The last NON-EMPTY assistant turn is the reply: the tool-calling turns in
	// between carry no text and must not blank it out.
	if e.Answer != "Il codice è 632587." {
		t.Errorf("Answer = %q", e.Answer)
	}
	// Two memory results, and only those: a tool result that is not JSON, and one
	// with no facts key, are not memory searches and must not be counted as
	// empty ones — that would let a case pass by never searching.
	if want := []int{0, 2}; !equalInts(e.MemoryFactCounts, want) {
		t.Errorf("MemoryFactCounts = %v, want %v", e.MemoryFactCounts, want)
	}
}

func TestParseEvidenceRejectsNothingToMeasure(t *testing.T) {
	t.Parallel()
	// An empty result is what fail-closed RLS returns for an unscoped read. It must
	// be an error: as a zero-valued Evidence it would satisfy every budget and
	// every forbidden-tool rule, and the gate would go green having measured
	// nothing at all.
	if _, err := parseEvidence("SET\n[]\n"); err == nil {
		t.Fatal("an empty conversation was accepted; a gate that measures nothing must not pass")
	}
	if _, err := parseEvidence("SET\nERROR:  permission denied\n"); err == nil {
		t.Fatal("a psql error payload was accepted as evidence")
	}
}

func TestPostgresEvidenceRefusesUnscopedRead(t *testing.T) {
	t.Parallel()
	conv := "019fc8bc-2f42-7b5c-b783-40329b0399a0"
	if _, err := (PostgresEvidence{}).Read(context.Background(), conv); err == nil ||
		!strings.Contains(err.Error(), "not a uuid") {
		t.Fatalf("a missing identity was not refused: %v", err)
	}
	// Both ids reach a SQL template, so both are shape-checked before they get there.
	bad := PostgresEvidence{IdentityID: "dc98a3ee-e38e-4288-8d64-27ce4c9cde65"}
	if _, err := bad.Read(context.Background(), "'; DROP TABLE aura.conversation_turns; --"); err == nil ||
		!strings.Contains(err.Error(), "not a uuid") {
		t.Fatalf("a non-uuid conversation id reached the query: %v", err)
	}
}

func TestThreadIDIsReadFromTheTurnLog(t *testing.T) {
	t.Parallel()
	line := `{"time":"2026-08-03T17:19:31Z","msg":"agent turn start","request_id":"019fc8a3-478b-7cdf-a897-56a145d98e0d","thread_id":"019fc8a2-e472-7d1c-99c6-7e56dc7799fb"}`
	m := threadID.FindStringSubmatch(line)
	if m == nil {
		t.Fatal("thread_id not found in a real turn-start line")
	}
	// request_id precedes thread_id on the line and is the same shape: the pattern
	// must key on the FIELD NAME, or every case reads evidence from a conversation
	// that does not exist.
	if m[1] != "019fc8a2-e472-7d1c-99c6-7e56dc7799fb" {
		t.Errorf("matched %q — that is the request id, not the thread id", m[1])
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
