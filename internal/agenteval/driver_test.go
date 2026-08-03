package agenteval

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// recordExec captures what the driver would have run, so the argument assembly is
// asserted rather than assumed. It is the part that fails quietly: a probe that
// forgets -i hangs, and one that forgets the temperature override measures a
// sampled turn while reporting it as a gate result.
type recordExec struct {
	stdin, name string
	args        []string
	out         string
	err         error
}

func (r *recordExec) run(_ context.Context, stdin, name string, args ...string) ([]byte, error) {
	r.stdin, r.name, r.args = stdin, name, args
	return []byte(r.out), r.err
}

func TestCLIDriverAsksTheRunningContainerDeterministically(t *testing.T) {
	t.Parallel()
	rec := &recordExec{out: `{"msg":"agent turn start","request_id":"019fc8a3-478b-7cdf-a897-56a145d98e0d",` +
		`"thread_id":"019fc8a2-e472-7d1c-99c6-7e56dc7799fb"}` + "\nIl codice è 632587.\n"}
	d := CLIDriver{exec: rec.run}

	conv, err := d.Ask(context.Background(), "dammi il codice cliente di WPT SRL")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if conv != "019fc8a2-e472-7d1c-99c6-7e56dc7799fb" {
		t.Errorf("conversation = %q; the request id must not be mistaken for it", conv)
	}
	if rec.stdin != "dammi il codice cliente di WPT SRL\n" {
		t.Errorf("stdin = %q — the REPL needs the trailing newline to submit", rec.stdin)
	}
	joined := strings.Join(rec.args, " ")
	// -i or the REPL never reads the question; the temperature override or the gate
	// is measuring a sample at 0.7 and will flap; "aura" is the default container.
	for _, want := range []string{"exec -i", "AURA_LLM_TEMPERATURE=0", " aura aura shell"} {
		if !strings.Contains(joined, want) {
			t.Errorf("command is missing %q: docker %s", want, joined)
		}
	}
	if rec.name != "docker" {
		t.Errorf("ran %q, want docker", rec.name)
	}

	named := CLIDriver{Container: "aura-staging", exec: rec.run}
	if _, err := named.Ask(context.Background(), "q"); err != nil {
		t.Fatalf("Ask with an explicit container: %v", err)
	}
	if !strings.Contains(strings.Join(rec.args, " "), "aura-staging") {
		t.Errorf("explicit container ignored: %v", rec.args)
	}
}

func TestCLIDriverFailsLoudlyWhenNoTurnRan(t *testing.T) {
	t.Parallel()
	// A turn that never started leaves no thread_id. Returning an empty
	// conversation id here would send the reader at a nonexistent conversation and
	// surface as "no turns recorded" three layers away.
	silent := CLIDriver{exec: (&recordExec{out: "usage: aura shell\n"}).run}
	if _, err := silent.Ask(context.Background(), "q"); err == nil ||
		!strings.Contains(err.Error(), "thread_id") {
		t.Fatalf("a turn that never started was accepted: %v", err)
	}
	broken := CLIDriver{exec: (&recordExec{out: "boom", err: errors.New("exit 1")}).run}
	if _, err := broken.Ask(context.Background(), "q"); err == nil ||
		!strings.Contains(err.Error(), "boom") {
		t.Fatalf("the exec failure lost the container output: %v", err)
	}
}

func TestPostgresEvidenceScopesTheReadToTheOwner(t *testing.T) {
	t.Parallel()
	rec := &recordExec{out: `SET
[{"seq":1,"role":"assistant","content":"632587","tools":["document_open"]}]`}
	p := PostgresEvidence{IdentityID: "dc98a3ee-e38e-4288-8d64-27ce4c9cde65", exec: rec.run}

	e, err := p.Read(context.Background(), "019fc8a2-e472-7d1c-99c6-7e56dc7799fb")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if e.LLMCalls != 1 || e.Answer != "632587" {
		t.Errorf("evidence = %+v", e)
	}
	sql := strings.Join(rec.args, " ")
	// Without the SET the table is fail-closed and answers with zero rows, which is
	// not an error — it is a gate that measured nothing and went green. The SQL is
	// shell-quoted by the time it lands in argv, so the literal is checked in
	// pieces rather than verbatim.
	if !strings.Contains(sql, "SET app.current_identity") ||
		!strings.Contains(sql, "dc98a3ee-e38e-4288-8d64-27ce4c9cde65") {
		t.Errorf("the read is not scoped to the owner: %s", truncate(sql, 400))
	}
	if !strings.Contains(sql, "019fc8a2-e472-7d1c-99c6-7e56dc7799fb") {
		t.Errorf("the conversation id never reached the query: %s", truncate(sql, 400))
	}
	if rec.args[0] != "exec" || rec.args[1] != "aura-postgres" {
		t.Errorf("default postgres container not used: %v", rec.args[:2])
	}
}

func TestShellQuoteSurvivesTheQuotesInTheQuery(t *testing.T) {
	t.Parallel()
	// The SQL is full of single quotes — every id and every jsonb cast literal —
	// so the wrapper has to close, escape and reopen rather than backslash them.
	got := shellQuote(`SET x = 'a'; SELECT '[]'::json;`)
	if want := `'SET x = '\''a'\''; SELECT '\''[]'\''::json;'`; got != want {
		t.Errorf("shellQuote =\n%s\nwant\n%s", got, want)
	}
}
