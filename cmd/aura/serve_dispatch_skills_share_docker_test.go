//go:build docker_integration

// serve_dispatch_skills_share_docker_test.go is acceptance criterion 5 of amendment #214
// asserted where it is actually decided: INSIDE the container. A grant must put ONE of A's
// skills in B's box, revoking it must take that skill back out at the NEXT RESUME, and the
// skill A never shared must be absent throughout.
//
// The two stores are stubbed (stubGrants/stubCatalog, skills_shared_test.go) and everything
// downstream of them is real: the production sandboxMaterializeSources, the real
// MaterializeIn tar+clear, and a real daemon. That split is deliberate — the live grant is
// the db_integration tier's sentence, and this file exists to say what the DAEMON lands,
// which no amount of Postgres can prove.
//
// It gates on a reachable daemon exactly like serve_dispatch_egress_integration_test.go
// (skip locally, t.Fatal under $CI — no-skip-as-green) and reuses that file's helpers.

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moby/moby/client"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/skills"
)

// shareITBackend builds a DockerBackend whose materialize sources come from the production
// resolver over the supplied SharedReader.
func shareITBackend(t *testing.T, cli *client.Client, cfg *config.Config, shared *skills.SharedReader) *usersandbox.DockerBackend {
	t.Helper()
	return usersandbox.NewDockerBackend(cli, cfg.Sandbox.Image, limitsFrom(cfg.Sandbox),
		usersandbox.WithMaterializeSources(sandboxMaterializeSources(cfg, shared)))
}

// TestSharedSkillEntersTheBoxAndLeavesOnRevoke is criterion 5's box half, both directions.
func TestSharedSkillEntersTheBoxAndLeavesOnRevoke(t *testing.T) {
	egressITDockerdOrGate(t)

	base := t.TempDir()
	boxImage := egressITBoxImage()
	reader := egressITIdentity(t)
	owner := reader[:len(reader)-len("-owner")] + "-owner"

	cfg := &config.Config{
		Profile:           config.ProfileSingleUserHardened,
		SkillsDir:         filepath.Join(base, "skills"),
		SkillsIdentityDir: filepath.Join(base, "skills-identities"),
		SkillExportDir:    filepath.Join(base, "export"),
		Sandbox: config.SandboxConfig{
			Image:       boxImage,
			CPULimit:    1,
			MemoryLimit: 256 << 20,
			PidsLimit:   128,
		},
	}
	ownerRoots, err := skillLayout(cfg).For(owner)
	if err != nil {
		t.Fatalf("layout for the owner: %v", err)
	}
	writeExportedSkill(t, cfg.SkillExportDir, "house-skill", "house body")
	writeExportedSkill(t, ownerRoots.Export, "granted-skill", "granted body")
	writeExportedSkill(t, ownerRoots.Export, "ungranted-skill", "ungranted body")

	row := skills.CatalogRow{ID: "share-it-1", OwnerID: owner, Name: "granted-skill"}
	granted := skills.NewSharedReader(stubGrants{ids: []string{row.ID}}, stubCatalog{rows: []skills.CatalogRow{row}},
		skillLayout(cfg))

	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	ctx := context.Background()
	spec := usersandbox.SandboxSpec{IdentityID: reader, Image: boxImage, Limits: limitsFrom(cfg.Sandbox)}
	h, err := shareITBackend(t, cli, cfg, granted).Resolve(ctx, spec)
	if err != nil {
		t.Fatalf("resolve with the grant standing: %v", err)
	}
	t.Cleanup(func() {
		_ = usersandbox.NewDockerBackend(cli, boxImage, limitsFrom(cfg.Sandbox)).Stop(context.Background(), h)
	})

	listing, code := egressITRawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", "find /skills"})
	if code != 0 {
		t.Fatalf("find /skills exited %d: %s", code, listing)
	}
	if !strings.Contains(listing, "granted-skill") {
		t.Fatalf("the granted skill is NOT in the box — criterion 5's box half is open:\n%s", listing)
	}
	if !strings.Contains(listing, "house-skill") {
		t.Fatalf("the house library is gone from the box — the shared source displaced the overlay:\n%s", listing)
	}
	if strings.Contains(listing, "ungranted-skill") {
		t.Fatalf("A SKILL THE OWNER NEVER SHARED IS IN THIS BOX (#214 criterion 2, #215):\n%s", listing)
	}
	// Names can be absent while bytes are present, so the body is the real question.
	leaked, _ := egressITRawExec(t, cli, h.ContainerID,
		[]string{"/bin/sh", "-c", "grep -rl 'ungranted body' /skills 2>/dev/null || true"})
	if strings.TrimSpace(leaked) != "" {
		t.Fatalf("an unshared body is readable in this box:\n%s", leaked)
	}
	body, code := egressITRawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", "cat /skills/granted-skill/SKILL.md"})
	if code != 0 || !strings.Contains(body, "granted body") {
		t.Fatalf("the shared body did not land (exit %d):\n%s", code, body)
	}

	// THE REVOKE. Nothing on the host moved: the owner still has the skill, the export tree
	// is untouched. The only change is that the resolver no longer names it — which is
	// exactly what a revoked grant does — and the mirror must do the rest at the next resume.
	revoked := skills.NewSharedReader(stubGrants{}, stubCatalog{}, skillLayout(cfg))
	if _, err := shareITBackend(t, cli, cfg, revoked).Resolve(ctx, spec); err != nil {
		t.Fatalf("resume after the revoke: %v", err)
	}
	after, code := egressITRawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", "find /skills"})
	if code != 0 {
		t.Fatalf("find /skills after the revoke exited %d: %s", code, after)
	}
	if strings.Contains(after, "granted-skill") {
		t.Fatalf("A REVOKED SKILL SURVIVED THE RESUME (#214 criterion 5):\n%s", after)
	}
	stale, _ := egressITRawExec(t, cli, h.ContainerID,
		[]string{"/bin/sh", "-c", "grep -rl 'granted body' /skills 2>/dev/null || true"})
	if strings.TrimSpace(stale) != "" {
		t.Fatalf("the revoked BODY is still readable in this box:\n%s", stale)
	}
	if !strings.Contains(after, "house-skill") {
		t.Fatalf("the revoke took the house library with it:\n%s", after)
	}
}

// TestBoxIsMirroredWhenEveryHostSourceIsGone is the corner the clear plan exists for. A
// reader with no library of their own, in a deployment whose export dir does not exist on
// disk, whose ONLY /skills content came from a share: when the grant goes, no source is left
// to land, and under a clear claimed by the first landing source nothing would remove the
// body. The dest is mirrored to empty instead.
func TestBoxIsMirroredWhenEveryHostSourceIsGone(t *testing.T) {
	egressITDockerdOrGate(t)

	base := t.TempDir()
	boxImage := egressITBoxImage()
	reader := egressITIdentity(t)
	owner := reader[:len(reader)-len("-owner")] + "-owner"

	cfg := &config.Config{
		Profile:           config.ProfileSingleUserHardened,
		SkillsDir:         filepath.Join(base, "skills"),
		SkillsIdentityDir: filepath.Join(base, "skills-identities"),
		// Configured and deliberately never created: a deployment that has published no
		// skill of its own has no export tree on disk.
		SkillExportDir: filepath.Join(base, "export"),
		Sandbox: config.SandboxConfig{
			Image:       boxImage,
			CPULimit:    1,
			MemoryLimit: 256 << 20,
			PidsLimit:   128,
		},
	}
	ownerRoots, err := skillLayout(cfg).For(owner)
	if err != nil {
		t.Fatalf("layout for the owner: %v", err)
	}
	writeExportedSkill(t, ownerRoots.Export, "only-shared", "only shared body")
	row := skills.CatalogRow{ID: "share-it-2", OwnerID: owner, Name: "only-shared"}

	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	ctx := context.Background()
	spec := usersandbox.SandboxSpec{IdentityID: reader, Image: boxImage, Limits: limitsFrom(cfg.Sandbox)}
	granted := skills.NewSharedReader(stubGrants{ids: []string{row.ID}}, stubCatalog{rows: []skills.CatalogRow{row}},
		skillLayout(cfg))
	h, err := shareITBackend(t, cli, cfg, granted).Resolve(ctx, spec)
	if err != nil {
		t.Fatalf("resolve with the grant standing: %v", err)
	}
	t.Cleanup(func() {
		_ = usersandbox.NewDockerBackend(cli, boxImage, limitsFrom(cfg.Sandbox)).Stop(context.Background(), h)
	})
	if body, code := egressITRawExec(t, cli, h.ContainerID,
		[]string{"/bin/sh", "-c", "cat /skills/only-shared/SKILL.md"}); code != 0 || !strings.Contains(body, "only shared body") {
		t.Fatalf("the only source did not land (exit %d):\n%s", code, body)
	}

	revoked := skills.NewSharedReader(stubGrants{}, stubCatalog{}, skillLayout(cfg))
	if _, err := shareITBackend(t, cli, cfg, revoked).Resolve(ctx, spec); err != nil {
		t.Fatalf("resume after the revoke: %v", err)
	}
	after, _ := egressITRawExec(t, cli, h.ContainerID,
		[]string{"/bin/sh", "-c", "grep -rl 'only shared body' /skills 2>/dev/null || true"})
	if strings.TrimSpace(after) != "" {
		t.Fatalf("with every host source gone the box kept the revoked body:\n%s", after)
	}
}

// TestAMalformedShareDoesNotCostTheGranteeTheirBox is the daemon-side statement of the
// degrade-don't-deny rule (amendment #217, D-217-4 carried into the tar pass): a share whose
// tree the tar builder refuses — here a symlink, the sandbox-runtime escape guard — is skipped,
// and everything else still lands. Before this the whole MaterializeIn failed, so one person's
// malformed skill took away every grantee's box, and with it every tool they reach through it.
//
// The unit tier already asserts the skip and its asymmetry on tarSources; what only a daemon
// can say is that the box the grantee is left with is a WORKING one.
func TestAMalformedShareDoesNotCostTheGranteeTheirBox(t *testing.T) {
	egressITDockerdOrGate(t)

	base := t.TempDir()
	boxImage := egressITBoxImage()
	reader := egressITIdentity(t)
	owner := reader[:len(reader)-len("-owner")] + "-owner"

	cfg := &config.Config{
		Profile:           config.ProfileSingleUserHardened,
		SkillsDir:         filepath.Join(base, "skills"),
		SkillsIdentityDir: filepath.Join(base, "skills-identities"),
		SkillExportDir:    filepath.Join(base, "export"),
		Sandbox: config.SandboxConfig{
			Image:       boxImage,
			CPULimit:    1,
			MemoryLimit: 256 << 20,
			PidsLimit:   128,
		},
	}
	ownerRoots, err := skillLayout(cfg).For(owner)
	if err != nil {
		t.Fatalf("layout for the owner: %v", err)
	}
	writeExportedSkill(t, cfg.SkillExportDir, "house-skill", "house body")
	writeExportedSkill(t, ownerRoots.Export, "good-share", "good body")
	writeExportedSkill(t, ownerRoots.Export, "bad-share", "bad body")
	// The fault: a symlink inside the shared tree, which writeTarDir refuses by design.
	badDir := filepath.Join(ownerRoots.Export, "bad-share")
	if err := os.Symlink(filepath.Join(badDir, "SKILL.md"), filepath.Join(badDir, "link.md")); err != nil {
		t.Skipf("symlink unsupported on this host: %v", err)
	}

	rows := []skills.CatalogRow{
		{ID: "bad-1", OwnerID: owner, Name: "bad-share"},
		{ID: "good-1", OwnerID: owner, Name: "good-share"},
	}
	shared := skills.NewSharedReader(
		stubGrants{ids: []string{"bad-1", "good-1"}}, stubCatalog{rows: rows}, skillLayout(cfg))

	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	ctx := context.Background()
	spec := usersandbox.SandboxSpec{IdentityID: reader, Image: boxImage, Limits: limitsFrom(cfg.Sandbox)}
	h, err := shareITBackend(t, cli, cfg, shared).Resolve(ctx, spec)
	if err != nil {
		t.Fatalf("a malformed SHARE must not fail Resolve — the grantee keeps their box: %v", err)
	}
	t.Cleanup(func() {
		_ = usersandbox.NewDockerBackend(cli, boxImage, limitsFrom(cfg.Sandbox)).Stop(context.Background(), h)
	})

	listing, code := egressITRawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", "find /skills"})
	if code != 0 {
		t.Fatalf("find /skills exited %d: %s", code, listing)
	}
	if !strings.Contains(listing, "house-skill") || !strings.Contains(listing, "good-share") {
		t.Fatalf("the malformed share took the rest of the box with it:\n%s", listing)
	}
	if strings.Contains(listing, "bad-share") {
		t.Fatalf("the refused share landed anyway — the skip must drop it, not half-copy it:\n%s", listing)
	}
}
