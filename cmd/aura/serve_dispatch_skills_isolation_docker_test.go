//go:build docker_integration

// serve_dispatch_skills_isolation_docker_test.go is acceptance criterion 2 of amendment #214,
// asserted where it actually matters: INSIDE the container. A box created for identity B
// through the production buildSandboxRouter → Route path must hold B's skills and the
// deployment's, and must NOT hold A's — and the proof is a RECURSIVE walk of /skills in B's
// box plus a grep of the bodies, not an API answering about itself. Recursive because a
// leaked export arrives one directory down (/skills/<A-id>/<skill>), where a top-level
// listing does not look.
//
// It gates on a reachable daemon exactly like serve_dispatch_egress_integration_test.go
// (skip locally, t.Fatal under $CI — no-skip-as-green) and reuses that file's helpers, since
// both drive the same composition root against the same daemon.

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moby/moby/client"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/skills"
)

// writeExportedSkill lays a materialized skill tree down in an export dir, the shape
// skills.Materialize produces and the shape MaterializeIn tars into the box.
func writeExportedSkill(t *testing.T, exportDir, name, body string) {
	t.Helper()
	dir := filepath.Join(exportDir, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	md := "---\nname: " + name + "\ndescription: " + name + " description\ntype: instruction\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o600); err != nil {
		t.Fatalf("write %q: %v", name, err)
	}
}

// TestBoxHoldsOnlyItsOwnAndTheHouseSkills fills two identities' export roots and the
// deployment's, resolves a box for ONE of them through the production path, and reads /skills
// from inside it.
func TestBoxHoldsOnlyItsOwnAndTheHouseSkills(t *testing.T) {
	egressITDockerdOrGate(t)

	base := t.TempDir()
	exportBase := filepath.Join(base, "export")
	boxImage := egressITBoxImage()
	// Docker names the box and its volume after the identity, so the id has to be
	// name-safe; egressITIdentity is the same generator the egress proof uses.
	mine := egressITIdentity(t)
	// Appending to a generated id is how the cap gets breached a second time: mine is already
	// allowed to reach the limit, so the neighbour REPLACES the tail rather than extending it.
	theirs := mine[:len(mine)-len("-other")] + "-other"

	layout := skills.Layout{
		Global:     filepath.Join(base, "skills"),
		Identities: filepath.Join(base, "skills-identities"),
		Export:     exportBase,
	}
	// The per-identity export dirs come from the production layout, never from a join
	// written here: a fixture that decides for itself where an identity exports proves the
	// isolation of a directory tree the daemon does not use.
	mineRoots, err := layout.For(mine)
	if err != nil {
		t.Fatalf("layout for %q: %v", mine, err)
	}
	theirRoots, err := layout.For(theirs)
	if err != nil {
		t.Fatalf("layout for %q: %v", theirs, err)
	}
	writeExportedSkill(t, exportBase, "house-skill", "house body")
	writeExportedSkill(t, mineRoots.Export, "my-skill", "my body")
	writeExportedSkill(t, theirRoots.Export, "their-skill", "their body")

	cfg := &config.Config{
		Profile:           config.ProfileSingleUserHardened,
		SkillsDir:         layout.Global,
		SkillsIdentityDir: layout.Identities,
		SkillExportDir:    layout.Export,
		Sandbox: config.SandboxConfig{
			Image:       boxImage,
			CPULimit:    1,
			MemoryLimit: 256 << 20,
			PidsLimit:   128,
		},
	}

	router := buildSandboxRouter(cfg)
	if router == nil {
		t.Fatal("buildSandboxRouter returned nil — that would be a host-fallback door at the composition root")
	}
	ctx := identityctx.WithIdentityID(context.Background(), mine)
	h, err := router.Route(ctx)
	if err != nil {
		t.Fatalf("router.Route: %v", err)
	}
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	cleanup := usersandbox.NewDockerBackend(cli, boxImage,
		usersandbox.Resources{NanoCPUs: 1_000_000_000, MemoryBytes: 256 << 20, PidsLimit: 128})
	t.Cleanup(func() { _ = cleanup.Stop(context.Background(), h) })

	// The assertion is made from inside the box: what the daemon actually landed, not what
	// the resolver said it would — and RECURSIVELY. `ls -1 /skills` reads the top level only,
	// so it answers "is their-skill a child of /skills" when the question is "is their-skill
	// anywhere in this box": a leaked export lands one directory down, at
	// /skills/<their-id>/their-skill, and a top-level listing calls that clean.
	listing, code := egressITRawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", "find /skills"})
	if code != 0 {
		t.Fatalf("find /skills exited %d: %s", code, listing)
	}
	if !strings.Contains(listing, "my-skill") {
		t.Fatalf("the box does not carry its own identity's skill:\n%s", listing)
	}
	if !strings.Contains(listing, "house-skill") {
		t.Fatalf("the box does not carry the deployment's skills — the overlay lost the house library:\n%s", listing)
	}
	if strings.Contains(listing, "their-skill") {
		t.Fatalf("ANOTHER IDENTITY'S SKILL IS IN THIS BOX (#214 criterion 2):\n%s", listing)
	}
	if strings.Contains(listing, theirs) {
		t.Fatalf("another identity's export ROOT is in this box (#214 criterion 2):\n%s", listing)
	}
	// Names can be absent while bytes are present (a differently-named directory holding the
	// same file), so the bodies are searched too — this is the sentence that actually says
	// "no instruction of theirs can be read in here".
	leaked, _ := egressITRawExec(t, cli, h.ContainerID,
		[]string{"/bin/sh", "-c", "grep -rl 'their body' /skills 2>/dev/null || true"})
	if strings.TrimSpace(leaked) != "" {
		t.Fatalf("another identity's skill BODY is readable in this box (#214 criterion 2):\n%s", leaked)
	}

	// Bodies too, not just names: a directory with the right name and the wrong body would
	// pass the listing check and still run somebody else's instructions.
	body, code := egressITRawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", "cat /skills/my-skill/SKILL.md"})
	if code != 0 || !strings.Contains(body, "my body") {
		t.Fatalf("the materialized body is not this identity's (exit %d):\n%s", code, body)
	}
}
