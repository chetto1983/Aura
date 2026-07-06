//go:build docker_integration

// lifecycle_integration_test.go proves the SBX-03 legs against a live dockerd: the suspend-
// retain / resume / delete lifecycle (D-08, Pitfall 5), storage-enforced cross-identity
// volume deny (spike 078 — never app-prefix scoping), and the D-10 materialize-at-resolve
// call site (skills land at the SnippetSandboxPath root; a /workspace artifact round-trips
// via CopyArtifactsOut). Skips locally, t.Fatal under $CI (skipUnlessDockerd, no-skip-as-green).

package usersandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/skills"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

// uniqueIdentity builds a docker-name-safe unique identity id for a test box so parallel /
// repeated runs never collide on the aura-box-<id> container+volume name.
func uniqueIdentity(t *testing.T, prefix string) string {
	t.Helper()
	raw := prefix + "-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	return strings.NewReplacer(".", "", "_", "-").Replace(raw)
}

// volumeExists reports whether a named volume is present (VolumeInspect NotFound => absent).
func volumeExists(t *testing.T, cli *client.Client, name string) bool {
	t.Helper()
	_, err := cli.VolumeInspect(context.Background(), name, client.VolumeInspectOptions{})
	if err == nil {
		return true
	}
	if cerrdefs.IsNotFound(err) {
		return false
	}
	t.Fatalf("volume inspect %q: %v", name, err)
	return false
}

// containerExists reports whether a container id is still present (Inspect NotFound => gone).
func containerExists(t *testing.T, cli *client.Client, id string) bool {
	t.Helper()
	_, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
	if err == nil {
		return true
	}
	if cerrdefs.IsNotFound(err) {
		return false
	}
	t.Fatalf("container inspect %q: %v", id, err)
	return false
}

// TestLifecycle_SuspendResumeDelete proves D-08: Suspend retains the box+volume, Resume
// reuses the SAME container against the SAME volume (marker survives), and Stop(Delete)
// removes the container AND the per-identity volume while the shared uv-cache survives.
func TestLifecycle_SuspendResumeDelete(t *testing.T) {
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)
	backend := NewDockerBackend(cli, testBoxImage(), testLimits())
	ctx := context.Background()

	id := uniqueIdentity(t, "life")
	vol := boxName(id)
	spec := SandboxSpec{IdentityID: id}

	h, err := backend.Resolve(ctx, spec)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	cleaned := false
	t.Cleanup(func() {
		if !cleaned {
			_ = backend.Stop(context.Background(), h)
		}
	})

	// Seed a marker on the workspace volume.
	if _, code := rawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", "echo retained > /workspace/marker.txt"}); code != 0 {
		t.Fatalf("seed marker: exit %d", code)
	}
	if !volumeExists(t, cli, vol) {
		t.Fatalf("workspace volume %q should exist after resolve", vol)
	}
	if !volumeExists(t, cli, uvCacheVolume) {
		t.Fatalf("shared uv-cache volume should exist after resolve")
	}

	// Suspend retains the volume (and the container).
	if err := backend.Suspend(ctx, h); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if !volumeExists(t, cli, vol) {
		t.Fatalf("volume must survive suspend (retain), %q gone", vol)
	}

	// Resume via Resolve returns the SAME container; the marker persisted on the volume.
	h2, err := backend.Resolve(ctx, spec)
	if err != nil {
		t.Fatalf("resolve after suspend: %v", err)
	}
	if h2.ContainerID != h.ContainerID {
		t.Fatalf("resume created a new container: %q != %q", h2.ContainerID, h.ContainerID)
	}
	out, code := rawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", "cat /workspace/marker.txt"})
	if code != 0 || !strings.Contains(out, "retained") {
		t.Fatalf("marker lost across suspend/resume: code=%d out=%q", code, out)
	}

	// Stop(Delete) removes the container AND the per-identity volume; uv-cache survives.
	if err := backend.Stop(ctx, h); err != nil {
		t.Fatalf("stop: %v", err)
	}
	cleaned = true
	if containerExists(t, cli, h.ContainerID) {
		t.Fatalf("container must be gone after Stop(Delete)")
	}
	if volumeExists(t, cli, vol) {
		t.Fatalf("per-identity volume %q must be gone after Stop(Delete)", vol)
	}
	if !volumeExists(t, cli, uvCacheVolume) {
		t.Fatalf("shared uv-cache volume must NOT be deleted by Stop")
	}
}

// TestVolume_CrossIdentityDeny proves the spike-078 non-negotiable: two identities get two
// named volumes, and B's box cannot read A's /workspace file — isolation is storage-enforced,
// never app-prefix scoping.
func TestVolume_CrossIdentityDeny(t *testing.T) {
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)
	backend := NewDockerBackend(cli, testBoxImage(), testLimits())
	ctx := context.Background()

	specA := SandboxSpec{IdentityID: uniqueIdentity(t, "deny-a")}
	specB := SandboxSpec{IdentityID: uniqueIdentity(t, "deny-b")}

	hA, err := backend.Resolve(ctx, specA)
	if err != nil {
		t.Fatalf("resolve A: %v", err)
	}
	t.Cleanup(func() { _ = backend.Stop(context.Background(), hA) })
	hB, err := backend.Resolve(ctx, specB)
	if err != nil {
		t.Fatalf("resolve B: %v", err)
	}
	t.Cleanup(func() { _ = backend.Stop(context.Background(), hB) })

	// A writes a secret into ITS /workspace.
	if _, code := rawExec(t, cli, hA.ContainerID, []string{"/bin/sh", "-c", "echo top-secret-A > /workspace/secret.txt"}); code != 0 {
		t.Fatalf("A write secret: exit %d", code)
	}

	// B cannot read it — a different named volume backs B's /workspace.
	out, code := rawExec(t, cli, hB.ContainerID, []string{"/bin/sh", "-c", "cat /workspace/secret.txt 2>&1"})
	if code == 0 {
		t.Fatalf("cross-identity read MUST fail, but B read A's secret: %q", out)
	}
	if strings.Contains(out, "top-secret-A") {
		t.Fatalf("cross-identity leak: B saw A's secret content: %q", out)
	}
}

// TestResolve_MaterializesInputs proves D-10 is delivered (MaterializeIn is NOT dead code):
// Resolve materializes skills / Agent.md / pyscripts at create AND at resume, landing skills
// at the SnippetSandboxPath root, and a /workspace artifact round-trips out via
// CopyArtifactsOut.
func TestResolve_MaterializesInputs(t *testing.T) {
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)
	ctx := context.Background()

	// Host fixtures: a skill, an Agent.md, and a pyscript.
	skillsRoot := t.TempDir()
	writeFixture(t, filepath.Join(skillsRoot, "calc", "calc.py"), "print('calc')\n")
	agentsRoot := t.TempDir()
	writeFixture(t, filepath.Join(agentsRoot, "Agent.md"), "# Agent profile\n")
	pyRoot := t.TempDir()
	writeFixture(t, filepath.Join(pyRoot, "hello.py"), "print('hi')\n")

	sources := func(string) []MaterializeSource {
		return []MaterializeSource{
			{HostDir: skillsRoot, Dest: "/skills"},
			{HostDir: agentsRoot, Dest: "/root/.aura/agents"},
			{HostDir: pyRoot, Dest: "/root/.aura/pyscripts"},
		}
	}
	backend := NewDockerBackend(cli, testBoxImage(), testLimits(), WithMaterializeSources(sources))

	spec := SandboxSpec{IdentityID: uniqueIdentity(t, "mat")}
	h, err := backend.Resolve(ctx, spec)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Cleanup(func() { _ = backend.Stop(context.Background(), h) })

	// The skill lands at the SAME path SnippetSandboxPath renders (agreement by construction).
	skillPath, err := skills.SnippetSandboxPath("calc", skills.LangPython)
	if err != nil {
		t.Fatalf("SnippetSandboxPath: %v", err)
	}
	assertBoxFile(t, cli, h.ContainerID, skillPath, "calc")
	assertBoxFile(t, cli, h.ContainerID, "/root/.aura/agents/Agent.md", "Agent profile")
	assertBoxFile(t, cli, h.ContainerID, "/root/.aura/pyscripts/hello.py", "hi")

	// Remove the materialized skill from the box rootfs, then Suspend + Resolve (resume):
	// materialize MUST run again on resume and restore it (not merely rely on volume persistence).
	if _, code := rawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", "rm -f " + skillPath}); code != 0 {
		t.Fatalf("rm skill: exit %d", code)
	}
	if err := backend.Suspend(ctx, h); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if _, err := backend.Resolve(ctx, spec); err != nil {
		t.Fatalf("resolve (resume): %v", err)
	}
	assertBoxFile(t, cli, h.ContainerID, skillPath, "calc")

	// A /workspace artifact round-trips OUT via CopyArtifactsOut (the send_file seam, 37-07).
	if _, code := rawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", "echo artifact-body > /workspace/out.txt"}); code != 0 {
		t.Fatalf("write artifact: exit %d", code)
	}
	rc, err := CopyArtifactsOut(ctx, cli, h, "/workspace/out.txt")
	if err != nil {
		t.Fatalf("copy artifacts out: %v", err)
	}
	defer rc.Close()
	if got := readTarEntry(t, rc, "out.txt"); !strings.Contains(got, "artifact-body") {
		t.Fatalf("artifact content = %q, want to contain %q", got, "artifact-body")
	}
}

// writeFixture writes a host fixture file, creating parent dirs.
func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// assertBoxFile cats a file inside the box and asserts it contains want.
func assertBoxFile(t *testing.T, cli *client.Client, containerID, boxPath, want string) {
	t.Helper()
	out, code := rawExec(t, cli, containerID, []string{"/bin/sh", "-c", "cat " + boxPath})
	if code != 0 {
		t.Fatalf("cat %q: exit %d (out=%q)", boxPath, code, out)
	}
	if !strings.Contains(out, want) {
		t.Fatalf("box file %q = %q, want to contain %q", boxPath, out, want)
	}
}
