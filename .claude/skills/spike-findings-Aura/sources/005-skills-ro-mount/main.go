// Spike 005 — skills-ro-mount: prove the Phase-11 D-17 design physically.
// A host export dir is bind-mounted read-only at /skills inside the
// aura-sandbox-agent container (see compose.skills-mount.yaml). The harness
// materializes a .py host-side, executes it BY PATH via the sandbox-agent
// /v1/processes/run API (the same wire sandbox_exec uses), asserts live
// host-edit visibility, in-container immutability, and de-materialization.
//
// Run (after applying the override — see compose.skills-mount.yaml header):
//
//	go run ./.planning/spikes/005-skills-ro-mount
//
// Exit 0 = VALIDATED, 1 = failure. Forensic [CATEGORY] log lines throughout.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/sandboxagent"
)

const (
	exportRel = ".planning/spikes/005-skills-ro-mount/export"
	inMount   = "/skills"
)

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), cat, fmt.Sprintf(format, a...))
}

func fail(format string, a ...any) {
	logf("SUMMARY", "INVALIDATED — "+format, a...)
	os.Exit(1)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root, err := os.Getwd()
	if err != nil {
		fail("getwd: %v", err)
	}
	exportDir := filepath.Join(root, filepath.FromSlash(exportRel))
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		fail("mkdir export: %v", err)
	}

	c := sandboxagent.New(sandboxagent.Config{BaseURL: envOr("AURA_SANDBOX_AGENT_URL", sandboxagent.DefaultBaseURL)})

	run := func(label, cmd string, args ...string) sandboxagent.RunResponse {
		resp, err := c.Run(ctx, sandboxagent.RunRequest{Command: cmd, Args: args})
		if err != nil {
			fail("%s: transport error: %v", label, err)
		}
		code := -1
		if resp.ExitCode != nil {
			code = *resp.ExitCode
		}
		logf("RUN", "%s: exit=%d stdout=%q stderr=%q", label, code, trim(resp.Stdout), trim(resp.Stderr))
		return resp
	}

	// 0. Mount present at all? ls /skills — fails fast if the override is not applied.
	mountLs := run("mount-visible", "ls", "-la", inMount)
	if mountLs.ExitCode == nil || *mountLs.ExitCode != 0 {
		fail("/skills not present in container — apply compose.skills-mount.yaml first (see header)")
	}

	// 1. Materialize host-side, execute by path in-container.
	marker1 := fmt.Sprintf("AURA-SPIKE-005-%d", time.Now().Unix())
	script := filepath.Join(exportDir, "hello.py")
	if err := os.WriteFile(script, []byte("print(\""+marker1+"\")\n"), 0o644); err != nil {
		fail("host write hello.py: %v", err)
	}
	logf("HOST", "materialized hello.py with marker %s", marker1)
	r1 := run("exec-by-path", "python3", inMount+"/hello.py")
	if r1.ExitCode == nil || *r1.ExitCode != 0 || !strings.Contains(r1.Stdout, marker1) {
		fail("by-path exec did not produce marker %s", marker1)
	}
	logf("PASS", "1/4 by-path execution of host-materialized file")

	// 2. Live visibility: rewrite host-side, re-run, expect the NEW marker.
	marker2 := marker1 + "-V2"
	if err := os.WriteFile(script, []byte("print(\""+marker2+"\")\n"), 0o644); err != nil {
		fail("host rewrite hello.py: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	visible := false
	for time.Now().Before(deadline) {
		r2 := run("live-visibility", "python3", inMount+"/hello.py")
		if r2.ExitCode != nil && *r2.ExitCode == 0 && strings.Contains(r2.Stdout, marker2) {
			visible = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !visible {
		fail("host edit not visible in-container within 15s (gRPC-FUSE cache?)")
	}
	logf("PASS", "2/4 live host-edit visibility")

	// 3. Immutability: in-container write to /skills must be refused.
	r3 := run("ro-enforced", "sh", "-c", "touch "+inMount+"/evil 2>&1")
	if r3.ExitCode != nil && *r3.ExitCode == 0 {
		fail("container wrote into /skills — mount is NOT read-only")
	}
	if !strings.Contains(strings.ToLower(r3.Stdout+r3.Stderr), "read-only") {
		logf("WARN", "write refused but without explicit read-only message (exit=%v)", r3.ExitCode)
	}
	logf("PASS", "3/4 read-only enforcement from inside")

	// 4. De-materialization: delete host-side, exec must now fail.
	if err := os.Remove(script); err != nil {
		fail("host remove hello.py: %v", err)
	}
	r4 := run("de-materialized", "python3", inMount+"/hello.py")
	if r4.ExitCode != nil && *r4.ExitCode == 0 {
		fail("deleted host file still executable in-container")
	}
	logf("PASS", "4/4 de-materialization (archive/delete lockstep)")

	logf("SUMMARY", "VALIDATED — ro bind mount works on this stack: by-path exec, live visibility, ro enforcement, de-materialization")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
