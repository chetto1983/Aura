//go:build arcadedb_integration

// tool_forget_test.go proves the ONE thing D-10 explicitly must not break:
// memory_forget's source-scoped detachment still reads MemoryFactSource.RunID
// as a FILTER after memory_upsert_fact stopped accepting it as a WRITE-path
// assertion (tool_memory.go's MemoryUpsertFactWriteSource split). Run against
// a real disposable ArcadeDB database rather than a scripted mock: two real
// host-derived actors (a parent and a worker) merge into ONE fact via D-09's
// shipped dedup, and forgetting by the parent's run id must detach only that
// source, leaving the worker's behind.
//
// Run: go test -tags arcadedb_integration ./cmd/arcadedb-mcp/ -run TestForget
package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// disposableForgetTestClient mirrors internal/arcadedb's disposableArcadeClient
// (memory_integration_test.go): a fresh, throwaway database per test, dropped
// in cleanup, so this file needs no tenant/credential/OAuth machinery at all --
// memoryUpsertFactHandler and memoryForgetHandler take a *tenants and a plain
// *mcp.CallToolRequest, and singleTenant(t, client) already routes any real
// *arcadedb.Client through them exactly like every other unit test here.
func disposableForgetTestClient(t *testing.T) *arcadedb.Client {
	t.Helper()
	base := agentMemoryLiveEnv("ARCADEDB_URL", "http://127.0.0.1:2480")
	password := os.Getenv("ARCADEDB_ADMIN_PASSWORD")
	if password == "" {
		password = os.Getenv("ARCADEDB_PASSWORD")
	}
	if password == "" {
		agentMemoryLiveGap(t, "ARCADEDB_PASSWORD or ARCADEDB_ADMIN_PASSWORD is not set")
	}
	admin, err := arcadedb.New(arcadedb.Config{
		BaseURL: base, Database: "unused", User: agentMemoryLiveEnv("ARCADEDB_ADMIN_USER", "root"),
		Password: password, Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	ctx := context.Background()
	if _, err := admin.ServerVersion(ctx); err != nil {
		agentMemoryLiveGap(t, "ArcadeDB is unreachable: %v", err)
	}
	database := fmt.Sprintf("aura_mcp_forget_test_%d", time.Now().UnixNano())
	if _, err := admin.CreateDatabase(ctx, database); err != nil {
		t.Fatalf("create disposable database %s: %v", database, err)
	}
	t.Cleanup(func() {
		if _, err := admin.DropDatabase(context.Background(), database); err != nil {
			t.Errorf("drop disposable database %s: %v", database, err)
		}
	})
	client, err := arcadedb.New(arcadedb.Config{
		BaseURL: base, Database: database, User: agentMemoryLiveEnv("ARCADEDB_ADMIN_USER", "root"),
		Password: password, Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("disposable client: %v", err)
	}
	if err := client.EnsureMemorySchema(ctx); err != nil {
		t.Fatalf("EnsureMemorySchema: %v", err)
	}
	return client
}

// TestForgetDetachesOneActorsSourceAfterD10SplitLeavesTheOther is the plan's
// own required proof: D-10 removed run_id from memory_upsert_fact's WRITE
// schema, but memory_forget keeps reading MemoryFactSource.RunID as a FILTER
// (tool_forget.go, unchanged by this phase). Two DIFFERENT host-derived
// actors (parent, worker) write the SAME content -- D-09 merges them into one
// fact with two sources -- and forgetting by the parent's run id must detach
// only that source, not the worker's.
func TestForgetDetachesOneActorsSourceAfterD10SplitLeavesTheOther(t *testing.T) {
	client := disposableForgetTestClient(t)
	tenants := singleTenant(t, client)
	const parentRunID = "forget-test-parent"
	const workerRunID = "forget-test-worker"
	in := MemoryUpsertFactInput{
		Subject: "ForgetTestSubject", Predicate: "learned_lesson", Object: "ForgetTestObject",
		Statement: "ForgetTestSubject learned a lesson.",
		Source:    MemoryUpsertFactWriteSource{MemoryIDs: []string{"m-parent"}},
	}
	if _, _, err := memoryUpsertFactHandler(tenants, testClock, "")(
		context.Background(), reqWithParentActor(testIdentity, parentRunID), in,
	); err != nil {
		t.Fatalf("parent upsert: %v", err)
	}

	workerIn := in
	workerIn.Source = MemoryUpsertFactWriteSource{MemoryIDs: []string{"m-worker"}}
	if _, _, err := memoryUpsertFactHandler(tenants, testClock, "")(
		context.Background(), reqWithActor(testIdentity, workerRunID, string(arcadedb.WriterWorker)), workerIn,
	); err != nil {
		t.Fatalf("worker upsert: %v", err)
	}

	_, before, err := memoryFactsAboutHandler(tenants)(
		context.Background(), reqWithIdentity(testIdentity),
		MemoryFactsAboutInput{Entity: "ForgetTestSubject"})
	if err != nil {
		t.Fatalf("facts_about before forget: %v", err)
	}
	if len(before.Facts) != 1 || len(before.Facts[0].Sources) != 2 {
		t.Fatalf("before = %+v, want ONE fact with TWO sources (D-09 merge across two actors)", before.Facts)
	}

	_, forgetOut, err := memoryForgetHandler(tenants)(
		context.Background(), reqWithIdentity(testIdentity),
		MemoryForgetInput{Source: &MemoryFactSource{RunID: parentRunID}},
	)
	if err != nil {
		t.Fatalf("memory_forget: %v", err)
	}
	if forgetOut.Facts != 1 {
		t.Fatalf("forget result = %+v, want exactly 1 fact detached", forgetOut)
	}

	_, after, err := memoryFactsAboutHandler(tenants)(
		context.Background(), reqWithIdentity(testIdentity),
		MemoryFactsAboutInput{Entity: "ForgetTestSubject"})
	if err != nil {
		t.Fatalf("facts_about after forget: %v", err)
	}
	if len(after.Facts) != 1 {
		t.Fatalf("after = %+v, want the fact to survive (the worker's source still supports it)", after.Facts)
	}
	if len(after.Facts[0].Sources) != 1 || after.Facts[0].Sources[0].RunID != workerRunID {
		t.Fatalf("after = %+v, want exactly the worker's source to remain", after.Facts[0].Sources)
	}
}
