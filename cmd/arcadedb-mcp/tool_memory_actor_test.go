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

	"github.com/chetto1983/aura/internal/arcadedb"
)

// A request that reached memoryUpsertFactHandler without the actor headers
// internal/agent/mcptools attaches on every identity-scoped mount is a
// wiring bug, not a malformed call the model could retry its way out of --
// so this is a real Go error, and nothing is written.
func TestUpsertFactRequiresHostDerivedActor(t *testing.T) {
	client, rec := newRecordingDB(t)
	_, _, err := memoryUpsertFactHandler(singleTenant(t, client), testClock, "")(
		context.Background(), reqWithIdentity(testIdentity), validFactInput())
	if err == nil {
		t.Fatal("expected an error for a request with no host-derived actor")
	}
	if !strings.Contains(err.Error(), "host-derived actor") {
		t.Fatalf("err = %v, want it to name the missing host-derived actor", err)
	}
	if len(rec.statements) != 0 {
		t.Fatalf("statements = %v, want nothing written before the actor is even resolved", rec.statements)
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
