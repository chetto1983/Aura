package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
)

func withDoctorMCPStubs(t *testing.T, servers map[string]mcp.ManagedServer, scope func(context.Context) (context.Context, error)) {
	t.Helper()
	runtimeOrig, scopeOrig := doctorRuntimeMCPServers, doctorScopeToOperator
	doctorRuntimeMCPServers = func() (map[string]mcp.ManagedServer, error) { return servers, nil }
	doctorScopeToOperator = scope
	t.Cleanup(func() { doctorRuntimeMCPServers, doctorScopeToOperator = runtimeOrig, scopeOrig })
}

// An OAuth-protected sidecar answers 401 to a probe carrying no identity, and 401 is not
// "down". Measured 2026-08-31 on a stack whose daemon had all three sidecars MOUNTED and
// healthy: `aura doctor` reported "3/3 HTTP MCP servers unreachable" and exited FAIL, while
// a bare curl to the same sidecar answered 401 — reachable, unauthenticated. An operator
// finishing an install reads that as a broken machine, so the credential gap has to be named
// in the failure rather than disguised as a dead server.
func TestDoctorMCPProbeNamesAMissingOperatorCredential(t *testing.T) {
	unreachable := map[string]mcp.ManagedServer{
		"memory": {URL: "http://127.0.0.1:1/mcp/", Type: "streamable_http"},
	}
	withDoctorMCPStubs(t, unreachable, func(context.Context) (context.Context, error) {
		return nil, errors.New("no operator has enrolled yet")
	})

	_, err := defaultDoctorProbeMCPServers(context.Background(), &config.Config{})
	if err == nil {
		t.Fatal("an unreachable server must still fail the probe")
	}
	if !strings.Contains(err.Error(), "WITHOUT operator credentials") {
		t.Fatalf("err = %v; a probe that could not authenticate must say so", err)
	}
	if !strings.Contains(err.Error(), "no operator has enrolled yet") {
		t.Fatalf("err = %v; it must carry the reason it could not authenticate", err)
	}
}

// With a resolvable operator the probe carries credentials and the note is absent — a
// genuine failure must not be excused by a caveat that does not apply.
func TestDoctorMCPProbeOmitsTheCaveatWhenCredentialed(t *testing.T) {
	unreachable := map[string]mcp.ManagedServer{
		"memory": {URL: "http://127.0.0.1:1/mcp/", Type: "streamable_http"},
	}
	withDoctorMCPStubs(t, unreachable, func(ctx context.Context) (context.Context, error) { return ctx, nil })

	_, err := defaultDoctorProbeMCPServers(context.Background(), &config.Config{})
	if err == nil {
		t.Fatal("an unreachable server must fail the probe")
	}
	if strings.Contains(err.Error(), "WITHOUT operator credentials") {
		t.Fatalf("err = %v; the caveat must not appear when the probe WAS credentialed", err)
	}
}
