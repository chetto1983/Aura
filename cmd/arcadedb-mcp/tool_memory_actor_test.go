package main

// D-10/D-11 at the memory_upsert_fact boundary: the actor (run id + writer
// role) is host-derived from connection headers (hostDerivedActor,
// tool_memory.go), never a field the model supplies. Split out of
// tool_memory_test.go, which the file-size gate caps at 600 LOC and was
// already at 598 before this file existed.

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// A request carrying NEITHER the bridge's actor headers NOR a verified OAuth
// token has no host that could vouch for it, and is refused before a single
// statement reaches the database. Which of the two gates speaks first is not
// the point and is deliberately not asserted: the identity gate happens to
// reject it earlier, and once a request IS authenticated it always has an
// actor -- that is the whole content of this fix. The actor gate's own message
// is pinned directly on hostDerivedActor below.
func TestUpsertFactRefusesARequestNoHostVouchesFor(t *testing.T) {
	client, rec := newRecordingDB(t)
	_, _, err := memoryUpsertFactHandler(singleTenant(t, client), testClock, "")(
		context.Background(), &mcp.CallToolRequest{
			Params: &mcp.CallToolParamsRaw{}, Extra: &mcp.RequestExtra{},
		}, validFactInput())
	if err == nil {
		t.Fatal("expected an error for a request no host vouches for")
	}
	if len(rec.statements) != 0 {
		t.Fatalf("statements = %v, want nothing written before the caller is even resolved", rec.statements)
	}
}

// An MCP client holding an Aura-issued OAuth token IS a host-derived actor: the
// signature was verified against Aura's JWKS, so the model can no more choose
// these values than it can choose a header it never sees. Before this, the
// memory was readable but not writable by anything except Aura's own bridge --
// measured 2026-09-02 against a live Claude Code mount.
func TestUpsertFactDerivesActorFromTheOAuthToken(t *testing.T) {
	client, rec := newRecordingDB(t)
	_, _, err := memoryUpsertFactHandler(singleTenant(t, client), testClock, "")(
		context.Background(), reqWithIdentity(testIdentity), validFactInput())
	if err != nil {
		t.Fatalf("upsert with an OAuth-only actor: %v", err)
	}
	if len(rec.statements) == 0 {
		t.Fatal("statements = none, want the fact written for an OAuth-authenticated client")
	}
}

// The run id is the most specific server-side value on hand and the role is
// always parent: an external client carries no delegated-dispatch context, and
// WriterParent is exactly the "host-driven write with no worker context at all"
// internal/arcadedb/memory.go already names.
func TestOAuthClientRunIDPrefersTheMostSpecificServerValue(t *testing.T) {
	withClientID := reqWithIdentity(testIdentity)
	withClientID.Extra.TokenInfo.Extra = map[string]any{oauthClientIDClaim: "  claude-code  "}

	blankClientID := reqWithIdentity(testIdentity)
	blankClientID.Extra.TokenInfo.Extra = map[string]any{oauthClientIDClaim: "   "}

	nonStringClientID := reqWithIdentity(testIdentity)
	nonStringClientID.Extra.TokenInfo.Extra = map[string]any{oauthClientIDClaim: 42}

	cases := []struct {
		name string
		req  *mcp.CallToolRequest
		want string
	}{
		{name: "no token", req: &mcp.CallToolRequest{Extra: &mcp.RequestExtra{}}},
		{name: "blank subject", req: reqWithIdentity("   ")},
		{name: "subject only", req: reqWithIdentity(testIdentity), want: "mcp-subject:" + testIdentity},
		{name: "client id wins over subject", req: withClientID, want: "mcp-client:claude-code"},
		{name: "blank client id falls back", req: blankClientID, want: "mcp-subject:" + testIdentity},
		{name: "non-string client id falls back", req: nonStringClientID, want: "mcp-subject:" + testIdentity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := oauthClientRunID(tc.req); got != tc.want {
				t.Fatalf("oauthClientRunID = %q, want %q", got, tc.want)
			}
			actor, err := hostDerivedActor(tc.req)
			if tc.want == "" {
				if err == nil {
					t.Fatal("expected an error when no host can vouch for the write")
				}
				if !strings.Contains(err.Error(), "host-derived actor") {
					t.Fatalf("err = %v, want it to name the missing host-derived actor", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("hostDerivedActor: %v", err)
			}
			if actor.Role != arcadedb.WriterParent {
				t.Fatalf("role = %q, want %q for an external client", actor.Role, arcadedb.WriterParent)
			}
		})
	}
}

// The bridge still wins when it speaks: only it can name a swarm worker, and an
// OAuth fallback that overrode it would silently promote every worker to parent
// and hand it D-11's supersede authority.
func TestBridgeHeadersOutrankTheOAuthFallback(t *testing.T) {
	req := reqWithActor(testIdentity, "worker-run-7", string(arcadedb.WriterWorker))
	actor, err := hostDerivedActor(req)
	if err != nil {
		t.Fatalf("hostDerivedActor: %v", err)
	}
	if actor.RunID != "worker-run-7" || actor.Role != arcadedb.WriterWorker {
		t.Fatalf("actor = %+v, want the bridge's own worker actor", actor)
	}
}

// An actor role outside {parent, worker} is equally a wiring bug: nothing
// upstream of this handler should ever produce a third value.
func TestUpsertFactRejectsUnknownActorRole(t *testing.T) {
	client, rec := newRecordingDB(t)
	req := reqWithActor(testIdentity, "run-x", "supervisor")
	_, _, err := memoryUpsertFactHandler(singleTenant(t, client), testClock, "")(
		context.Background(), req, validFactInput())
	if err == nil {
		t.Fatal("expected an error for an unknown actor role")
	}
	if !strings.Contains(err.Error(), "actor role") {
		t.Fatalf("err = %v, want it to name the bad actor role", err)
	}
	if len(rec.statements) != 0 {
		t.Fatalf("statements = %v, want nothing written for an unknown role", rec.statements)
	}
}

// D-11, through the FULL handler (not just internal/arcadedb's unit test):
// a worker's supersede attempt comes back Refused=true, a successful call
// with an effect-free result -- never an mcp.ToolCallError -- and issues no
// close-related Command.
func TestUpsertFactWorkerCannotSupersedeThroughTheFullHandler(t *testing.T) {
	client, rec := newRecordingDB(t)
	req := reqWithActor(testIdentity, "worker-run-1", string(arcadedb.WriterWorker))
	in := validFactInput()
	in.Supersedes = true
	_, out, err := memoryUpsertFactHandler(singleTenant(t, client), testClock, "")(
		context.Background(), req, in)
	if err != nil {
		t.Fatalf("a worker's refused supersede must not be a Go error: %v", err)
	}
	if !out.Refused {
		t.Fatalf("output = %+v, want refused=true for a worker's supersede attempt", out)
	}
	if !strings.Contains(out.Reason, "worker") {
		t.Fatalf("reason = %q, want it to explain the refusal is because the actor is a worker", out.Reason)
	}
	for _, statement := range rec.statements {
		if strings.Contains(statement, "SET valid_to") {
			t.Fatalf("a refused worker supersede must never reach a close statement: %s", statement)
		}
		if strings.HasPrefix(statement, "CREATE EDGE") {
			t.Fatalf("a refused correction must create no fact: %s", statement)
		}
	}
}

// Control case (D-11's OTHER half): a worker may still ADD a fact -- only
// closing one is refused. The persisted source must carry the WORKER's own
// run id and role, proving the host-derived actor reaches the graph, not
// just the refusal path.
func TestUpsertFactWorkerCanAddAFact(t *testing.T) {
	client, rec := newRecordingDB(t)
	req := reqWithActor(testIdentity, "worker-run-2", string(arcadedb.WriterWorker))
	_, out, err := memoryUpsertFactHandler(singleTenant(t, client), testClock, "")(
		context.Background(), req, validFactInput())
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if out.Refused {
		t.Fatalf("output = %+v, want a plain add to succeed for a worker", out)
	}
	_, params, ok := rec.find("CREATE EDGE")
	if !ok {
		t.Fatal("no CREATE EDGE issued for a worker's add")
	}
	sources, ok := params["sources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatalf("sources = %v", params["sources"])
	}
	source, ok := sources[0].(map[string]any)
	if !ok || source["run_id"] != "worker-run-2" || source["writer_role"] != string(arcadedb.WriterWorker) {
		t.Fatalf("source = %v, want the worker's own host-derived run id and role", sources[0])
	}
}
