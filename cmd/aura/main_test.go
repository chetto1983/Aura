package main

import (
	"context"
	"encoding/json"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
	"go.uber.org/goleak"
)

// TestMain runs the cmd/aura package tests under goleak so the chat REPL's
// per-turn streaming + two-stage Ctrl+C teardown is asserted leak-free (D-10/
// Req#3): a leaked stream goroutine or an un-cancelled signal notifier would trip
// here. goleak verifies AFTER all tests complete.
func TestMain(m *testing.M) {
	// The two-identity E2E's S3/garage-admin/Authula HTTP clients keep pooled idle
	// keep-alive connections (net/http.Transport persistConn read/write loops, created
	// by (*Transport).dialConn). goleak reports these as leaked at teardown even though
	// they are pooled and reaped on idle-timeout — the canonical http.Transport ignore.
	//
	// It MUST be IgnoreAnyFunction, not IgnoreTopFunction: a blocked readLoop's TOP
	// frame is internal/poll.runtime_pollWait (it parks in bufio.Peek→persistConn.Read),
	// so persistConn.readLoop is a MIDDLE frame — IgnoreTopFunction("...readLoop") never
	// matches. IgnoreAnyFunction matches the frame anywhere in the stack.
	//
	// go-winio's ioCompletionProcessor is the Windows named-pipe I/O pump the moby client
	// starts on its FIRST dial to the Docker daemon (initIO, package-level, one per pipe
	// handle) and never stops. It became reachable in tests when buildSandboxRouter began
	// pinging the daemon at boot — a deliberate diagnostic, since a dead daemon now means a
	// deployment where every tool call denies. The goroutine belongs to the library and lives
	// for the process; it is not something Aura can join. No-op on Linux, where the client
	// dials a unix socket.
	goleak.VerifyTestMain(releasingProcessPools{m},
		goleak.IgnoreAnyFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreAnyFunction("net/http.(*persistConn).writeLoop"),
		goleak.IgnoreAnyFunction("github.com/Microsoft/go-winio.ioCompletionProcessor"),
	)
}

// releasingProcessPools closes what the binary holds for its whole life, in the window
// between the last test and goleak's verification.
//
// The MCP registry and the OAuth grant store share one lazily-opened pool (mcp_registry.go)
// because they are read on the request path and cannot open-use-close like every other
// verb. Production gives that pool back by exiting. A test binary never exits until after
// goleak has already looked, so without this the pgxpool health-check goroutine is standing
// at verification — and it is genuinely standing, which is why the answer is to close the
// pool here rather than to add another Ignore.
type releasingProcessPools struct{ m *testing.M }

func (r releasingProcessPools) Run() int {
	code := r.m.Run()
	if mcpDBPool != nil {
		mcpDBPool.Close()
	}
	return code
}

func TestBuildRegistryBlockedManagedServerNotLaunched(t *testing.T) {
	withMemoryMCPRegistry(t)
	// Disable the default-on memory recipe (15-02 / D-08) so this test isolates the
	// blocked-server assertion: with memory off, the ONLY policy is the blocked
	// server, which must be filtered out (no closer). The memory default-on +
	// disable-respect path is covered by config.TestMemoryDefaultOn*.
	disabled := false
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"blocked": {Command: "aura-nonexistent-mcp-binary-xyz", Source: "manual", Trust: mcp.ManagedTrust{Class: mcp.TrustBlocked}},
		"memory":  {Type: mcp.ServerTypeStreamableHTTP, URL: "http://127.0.0.1:8091/mcp/", Source: "recipe:memory", Enabled: &disabled, Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe}},
	}}
	seedMCPRegistry(t, doc)
	cfg := config.LoadDB()
	servers, policies, err := mcpRuntimeSet()
	if err != nil {
		t.Fatalf("mcpRuntimeSet: %v", err)
	}
	if _, ok := servers["blocked"]; ok {
		t.Fatal("blocked managed server must be filtered before chat boot")
	}
	if _, ok := policies["memory"]; ok {
		t.Fatal("explicitly disabled memory must not reach policies")
	}
	reg, _, closers, err := buildRegistryWithMCP(context.Background(), cfg, nil, nil, nil, nil)
	defer func() { _ = closeMCPServers(closers) }()
	if err != nil {
		t.Fatalf("buildRegistryWithMCP: %v", err)
	}
	if reg == nil || len(closers) != 0 {
		t.Fatalf("blocked server should not mount: reg nil=%v closers=%d", reg == nil, len(closers))
	}
}

// TestBuildRegistryFailSoft proves the D-21 fail-soft boot: a single
// dead/misconfigured MCP server entry no longer aborts boot. buildRegistryWithMCP
// must WARN-and-drop the broken server and return a usable registry (err==nil) with
// the non-deferred built-ins still registered (Pitfall 6 — a boot where every MCP
// server is dropped still passes Registry.Validate via the built-ins).
func TestBuildRegistryFailSoft(t *testing.T) {
	withMemoryMCPRegistry(t)
	seedMCPRegistry(t, withDefaultOnRecipesOff(mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"broken": {Command: "aura-nonexistent-mcp-binary-xyz", Source: "manual"},
	}}))
	reg, _, closers, err := buildRegistryWithMCP(context.Background(), config.LoadDB(), nil, nil, nil, nil)
	defer func() { _ = closeMCPServers(closers) }()
	if err != nil {
		t.Fatalf("a broken MCP server must NOT abort boot (D-21); got err=%v", err)
	}
	if reg == nil {
		t.Fatal("buildRegistryWithMCP must return a usable registry after dropping the broken server")
	}
	// The broken server contributed no closer (it never mounted).
	if len(closers) != 0 {
		t.Fatalf("a server that failed to mount must add no closer, got %d", len(closers))
	}
	// The non-deferred built-ins survive — the registry is actionable (Pitfall 6).
	if err := reg.Validate(); err != nil {
		t.Fatalf("registry must stay valid after dropping every MCP server: %v", err)
	}
	if _, ok := reg.Get("text_response"); !ok {
		t.Fatal("base built-in text_response must remain registered after fail-soft drop")
	}
}

// TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives is the D-06/D-07
// (MCPH-04) bounded-mount regression guard AND the Pitfall #2 healthy-survives
// guard, asserted together in one test per the plan's acceptance criteria: a
// hung sibling server must be dropped within ~AURA_MCP_MOUNT_TIMEOUT while a
// healthy server mounted moments earlier keeps running past that same deadline
// (a single bounded context doubling as both the handshake deadline AND the
// subprocess-lifetime context would silently kill the healthy server the instant
// its own handshake ctx's cancel() fired).
func TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives(t *testing.T) {
	t.Setenv("AURA_MCP_MOUNT_TIMEOUT", "1")

	withMemoryMCPRegistry(t)
	seedMCPRegistry(t, withDefaultOnRecipesOff(mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"calculator": {
			Command: os.Args[0],
			Args:    []string{"-test.run=TestMCPServerHelperProcess", "--"},
			Env:     []string{"AURA_MCP_HELPER=1"},
			Source:  "manual",
		},
		"hung": {
			Command: os.Args[0],
			Args:    []string{"-test.run=TestMCPServerHelperProcess", "--"},
			Env:     []string{"AURA_MCP_HELPER=1", "AURA_MCP_HELPER_MODE=hang"},
			Source:  "manual",
		},
	}}))
	cfg := config.LoadDB()

	type buildResult struct {
		reg     *tools.Registry
		closers []func() error
		err     error
	}
	done := make(chan buildResult, 1)
	start := time.Now()
	go func() {
		reg, _, closers, err := buildRegistryWithMCP(context.Background(), cfg, nil, nil, nil, nil)
		done <- buildResult{reg: reg, closers: closers, err: err}
	}()

	var res buildResult
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("buildRegistryWithMCP did not return within 5s with one server hanging — the bounded " +
			"handshake ctx (~1s AURA_MCP_MOUNT_TIMEOUT) must cap the WHOLE mount, not block on the hung server (D-06/MCPH-04)")
	}
	elapsed := time.Since(start)
	if res.err != nil {
		t.Fatalf("buildRegistryWithMCP: %v", res.err)
	}
	defer func() { _ = closeMCPServers(res.closers) }()

	if elapsed > 3*time.Second {
		t.Fatalf("buildRegistryWithMCP took %v, want bounded near the 1s AURA_MCP_MOUNT_TIMEOUT despite the hung server", elapsed)
	}

	if _, ok := res.reg.Get("hung__calculate"); ok {
		t.Fatal("the hung server must be dropped, not mounted")
	}
	calc, ok := res.reg.Get("calculator__calculate")
	if !ok {
		t.Fatal("the healthy server must mount despite a sibling server hanging")
	}

	// Pitfall #2 regression guard: wait past the mount-timeout deadline and prove the
	// healthy server's subprocess is STILL alive and answering calls.
	time.Sleep(2 * time.Second)
	callCtx := tools.WithToolCallContext(context.Background(), "sess", "call-1", t.TempDir(), 2048)
	execRes, err := calc.Execute(callCtx, json.RawMessage(`{"expression":"2+2"}`))
	if err != nil {
		t.Fatalf("healthy server Execute after the mount timeout elapsed: %v (want alive, not killed by Pitfall #2)", err)
	}
	if execRes.Preview != "4" {
		t.Fatalf("healthy server result = %q, want 4", execRes.Preview)
	}
}

// TestCloseMCPServers_ConcurrentBoundedShutdown is the D-11 (MCPH-06 shutdown
// half) regression guard: N closers that each take a noticeable, sub-deadline
// time must complete in roughly ONE closer's duration (concurrent fan-out), not
// N times that duration (the old sequential reverse-order loop).
func TestCloseMCPServers_ConcurrentBoundedShutdown(t *testing.T) {
	t.Setenv("AURA_MCP_SHUTDOWN_TIMEOUT", "2")

	const n = 5
	var calls atomic.Int32
	closers := make([]func() error, n)
	for i := range closers {
		closers[i] = func() error {
			calls.Add(1)
			time.Sleep(300 * time.Millisecond)
			return nil
		}
	}

	start := time.Now()
	if err := closeMCPServers(closers); err != nil {
		t.Fatalf("closeMCPServers: %v", err)
	}
	elapsed := time.Since(start)
	if got := calls.Load(); got != n {
		t.Fatalf("want all %d closers invoked, got %d", n, got)
	}
	if elapsed > 900*time.Millisecond {
		t.Fatalf("closeMCPServers took %v, want concurrent fan-out (~300ms for %d closers), not %d*300ms sequential", elapsed, n, n)
	}

	// Settle window before any goleak assertion at TestMain teardown (Pitfall #8).
	time.Sleep(50 * time.Millisecond)
}

// TestCloseMCPServers_ZeroClosersReturnsImmediately covers the empty-slice fast
// path: no goroutine is spun up, no aggregate deadline is even started.
func TestCloseMCPServers_ZeroClosersReturnsImmediately(t *testing.T) {
	start := time.Now()
	if err := closeMCPServers(nil); err != nil {
		t.Fatalf("closeMCPServers(nil): %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("closeMCPServers(nil) took %v, want an immediate return", elapsed)
	}
}

// TestCloseMCPServers_AggregateDeadlineAbandonsStragglers proves the aggregate
// deadline actually bounds total wall-clock even when a closer never returns: the
// call returns (with an error) around the AURA_MCP_SHUTDOWN_TIMEOUT deadline, not
// blocked forever behind the stuck closer.
func TestCloseMCPServers_AggregateDeadlineAbandonsStragglers(t *testing.T) {
	t.Setenv("AURA_MCP_SHUTDOWN_TIMEOUT", "1")
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) }) // release the straggler so it settles before goleak's teardown check

	closers := []func() error{
		func() error { <-blocked; return nil },
		func() error { return nil },
	}

	start := time.Now()
	err := closeMCPServers(closers)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want the aggregate-deadline error when a closer never returns")
	}
	if elapsed < 900*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("closeMCPServers took %v, want ~1s (the aggregate AURA_MCP_SHUTDOWN_TIMEOUT), not blocked indefinitely", elapsed)
	}
}

// TestBuildBaseRegistryValidatesWithSwarmSpawn proves the Pitfall 6 ordering: the
// Deferred:true swarm_spawn is registered by buildBaseRegistry AND reg.Validate()
// still returns nil — because the non-deferred built-ins (not swarm_spawn) satisfy
// the >=1-non-deferred guard. The check exercises Validate AFTER swarm_spawn is
// present (buildBaseRegistry calls Validate internally and would os.Exit on a
// failure), then re-asserts the tool is registered with Deferred==true so a future
// refactor that flips the flag or drops the registration trips this test.
func TestBuildBaseRegistryValidatesWithSwarmSpawn(t *testing.T) {
	cfg := &config.Config{MaxSwarmGoals: 8}
	reg := buildBaseRegistry(cfg, nil) // os.Exits if Validate fails — reaching here proves it passed

	if err := reg.Validate(); err != nil {
		t.Fatalf("registry must stay valid with the Deferred swarm_spawn registered: %v", err)
	}

	tool, ok := reg.Get("swarm_spawn")
	if !ok {
		t.Fatal("buildBaseRegistry must register swarm_spawn into the parent registry (D-08/D-10)")
	}
	if !tool.Spec().Deferred {
		t.Fatal("swarm_spawn must be Deferred:true so it does not satisfy the non-deferred guard (Pitfall 6)")
	}
}
