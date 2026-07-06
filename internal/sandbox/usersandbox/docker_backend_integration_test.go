//go:build docker_integration

// docker_backend_integration_test.go is the Wave-0 SDK-binding proof (37-RESEARCH Pitfall 7):
// the FIRST full moby/moby/client round-trip against a live dockerd — pull, named volume,
// create (AutoRemove:false), exec, cp in, cp out, suspend, resume, exec again, stop. The
// spikes proved the box MODEL via the docker CLI; this proves the Go SDK BINDING the runtime
// actually uses. It skips locally when dockerd is unreachable (t.Fatal under $CI — no-skip-as-
// green) via the shared skipUnlessDockerd gate.

package usersandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// testBoxImage is the image the docker_integration tier pulls for round-trip/lifecycle
// tests. It defaults to a tiny, universally-pullable image and is overridable so a live run
// can exercise the real fat aura-sandbox image. Any image with a POSIX /bin/sh works because
// createBox overrides CMD with the portable keepAliveCmd.
func testBoxImage() string {
	if v := strings.TrimSpace(os.Getenv("AURA_SANDBOX_TEST_IMAGE")); v != "" {
		return v
	}
	return "busybox:stable"
}

// testLimits is a modest cgroup cap set for the integration tier (0.5 CPU / 256 MiB / 128
// pids) — enough to run a shell, small enough to co-tenant many test boxes.
func testLimits() Resources {
	return Resources{NanoCPUs: 500_000_000, MemoryBytes: 256 << 20, PidsLimit: 128}
}

// newTestDockerClient builds a moby client from the ambient DOCKER_HOST with API-version
// negotiation. It is the first real client binding in the repo.
func newTestDockerClient(t *testing.T) *client.Client {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

// rawExec runs a command in a running container straight through the moby SDK
// (ExecCreate/ExecAttach/stdcopy demux/ExecInspect) — the raw round-trip primitive the
// higher-level DockerBackend.Exec (37-04 Task 2) is built on. It returns demuxed stdout and
// the exit code.
func rawExec(t *testing.T, cli *client.Client, containerID string, cmd []string) (string, int) {
	t.Helper()
	ctx := context.Background()
	ec, err := cli.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		Cmd: cmd, AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		t.Fatalf("exec create: %v", err)
	}
	att, err := cli.ExecAttach(ctx, ec.ID, client.ExecAttachOptions{})
	if err != nil {
		t.Fatalf("exec attach: %v", err)
	}
	defer att.Close()
	var outBuf, errBuf bytes.Buffer
	if _, err := stdcopy.StdCopy(&outBuf, &errBuf, att.Reader); err != nil && err != io.EOF {
		t.Fatalf("exec stream: %v", err)
	}
	ins, err := cli.ExecInspect(ctx, ec.ID, client.ExecInspectOptions{})
	if err != nil {
		t.Fatalf("exec inspect: %v", err)
	}
	return outBuf.String(), ins.ExitCode
}

// tarFile returns a single-entry tar stream (name -> content), the docker cp input format.
func tarFile(t *testing.T, name, content string) io.Reader {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	return &buf
}

// readTarEntry scans a docker-cp tar stream for entryName and returns its content.
func readTarEntry(t *testing.T, r io.Reader, entryName string) string {
	t.Helper()
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if hdr.Name == entryName {
			b, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("tar read: %v", err)
			}
			return string(b)
		}
	}
	t.Fatalf("tar entry %q not found", entryName)
	return ""
}

// TestDockerBackend_RoundTrip is the full SDK round-trip smoke: Resolve (pull + volume +
// create AutoRemove:false + start), a second idempotent Resolve, raw exec, cp a file in, cp
// it back out, Suspend (stop-retain), Resume (same container), exec again, Stop (remove +
// volume) — every leg against a live dockerd. This retires the "the SDK code works" risk the
// CLI-only spikes never touched (Pitfall 7).
func TestDockerBackend_RoundTrip(t *testing.T) {
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)
	backend := NewDockerBackend(cli, testBoxImage(), testLimits())

	ctx := context.Background()
	id := "rt-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000")
	id = strings.ReplaceAll(id, ".", "")
	spec := SandboxSpec{IdentityID: id}

	h, err := backend.Resolve(ctx, spec)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Cleanup(func() { _ = backend.Stop(context.Background(), h) })

	// Idempotency: a second Resolve returns the SAME container, never a duplicate.
	h2, err := backend.Resolve(ctx, spec)
	if err != nil {
		t.Fatalf("resolve (2nd): %v", err)
	}
	if h2.ContainerID != h.ContainerID {
		t.Fatalf("resolve not idempotent: %q != %q", h2.ContainerID, h.ContainerID)
	}

	// AutoRemove must NEVER be set — --rm would destroy a suspendable box (Pitfall 5).
	ins, err := cli.ContainerInspect(ctx, h.ContainerID, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if ins.Container.HostConfig == nil || ins.Container.HostConfig.AutoRemove {
		t.Fatalf("AutoRemove must be false, got %+v", ins.Container.HostConfig)
	}

	// Exec: echo round-trips through the multiplexed exec stream.
	out, code := rawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", "echo hello-roundtrip"})
	if code != 0 || !strings.Contains(out, "hello-roundtrip") {
		t.Fatalf("exec echo: code=%d out=%q", code, out)
	}

	// cp a file IN (tar stream) then read it back via exec — the workspace volume persists it.
	const probeBody = "probe-payload-42"
	if _, err := cli.CopyToContainer(ctx, h.ContainerID, client.CopyToContainerOptions{
		DestinationPath: workspaceTarget,
		Content:         tarFile(t, "probe.txt", probeBody),
	}); err != nil {
		t.Fatalf("cp in: %v", err)
	}
	catOut, catCode := rawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", "cat /workspace/probe.txt"})
	if catCode != 0 || !strings.Contains(catOut, probeBody) {
		t.Fatalf("cat probe: code=%d out=%q", catCode, catOut)
	}

	// cp the same file OUT (tar stream) and assert the content survives the round-trip.
	res, err := cli.CopyFromContainer(ctx, h.ContainerID, client.CopyFromContainerOptions{
		SourcePath: workspaceTarget + "/probe.txt",
	})
	if err != nil {
		t.Fatalf("cp out: %v", err)
	}
	defer res.Content.Close()
	if got := readTarEntry(t, res.Content, "probe.txt"); got != probeBody {
		t.Fatalf("cp out content = %q, want %q", got, probeBody)
	}

	// Suspend stops but retains the box.
	if err := backend.Suspend(ctx, h); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	sins, err := cli.ContainerInspect(ctx, h.ContainerID, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("inspect after suspend: %v", err)
	}
	if sins.Container.State == nil || sins.Container.State.Running {
		t.Fatalf("box should be stopped after suspend, state=%+v", sins.Container.State)
	}

	// Resume restarts the SAME container.
	if err := backend.Resume(ctx, h); err != nil {
		t.Fatalf("resume: %v", err)
	}
	rins, err := cli.ContainerInspect(ctx, h.ContainerID, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("inspect after resume: %v", err)
	}
	if rins.Container.State == nil || !rins.Container.State.Running {
		t.Fatalf("box should be running after resume, state=%+v", rins.Container.State)
	}

	// Exec again post-resume — the retained file is still there (volume survived suspend).
	out2, code2 := rawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", "cat /workspace/probe.txt"})
	if code2 != 0 || !strings.Contains(out2, probeBody) {
		t.Fatalf("exec after resume: code=%d out=%q", code2, out2)
	}
}
