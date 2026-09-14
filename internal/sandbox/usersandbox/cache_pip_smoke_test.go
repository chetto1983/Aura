//go:build docker_integration && smoke

package usersandbox

import (
	"context"
	"strings"
	"testing"
)

// Run with AURA_SANDBOX_TEST_IMAGE set to the deployed Aura sandbox image.
func TestPipCacheIsolation(t *testing.T) {
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)
	b := NewDockerBackend(cli, testBoxImage(), testLimits())
	ctx := context.Background()
	boxes := make([]BoxHandle, 2)
	for i, origin := range []string{"clean-A", "changed-by-B"} {
		h, err := b.Resolve(ctx, SandboxSpec{IdentityID: uniqueIdentity(t, "pip-cache")})
		if err != nil {
			t.Fatal(err)
		}
		boxes[i] = h
		t.Cleanup(func() { _ = b.Stop(context.Background(), h) })
		out, code := rawExec(t, cli, h.ContainerID, []string{"python3", "-c", pipCacheSourceFixture, origin})
		if code != 0 {
			t.Fatalf("create source fixture: exit=%d out=%q", code, out)
		}
	}
	install := func(box BoxHandle, target string) string {
		t.Helper()
		out, code := rawExec(t, cli, box.ContainerID, []string{
			"python3", "-m", "pip", "install", "--no-index", "--find-links", "/tmp", "--no-deps",
			"--no-build-isolation", "--disable-pip-version-check", "--target", target, "aura-cache-probe==0.0.1",
		})
		if code != 0 {
			t.Fatalf("pip install: exit=%d out=%q", code, out)
		}
		return out
	}
	readOrigin := func(box BoxHandle, target, want string) {
		t.Helper()
		out, code := rawExec(t, cli, box.ContainerID, []string{"python3", "-c",
			"import sys; sys.path.insert(0, sys.argv[1]); import aura_cache_probe; print(aura_cache_probe.origin())", target})
		if code != 0 || strings.TrimSpace(out) != want {
			t.Fatalf("installed code origin: exit=%d got=%q want=%q", code, out, want)
		}
	}
	install(boxes[0], "/tmp/first")
	readOrigin(boxes[0], "/tmp/first", "clean-A")
	out, code := rawExec(t, cli, boxes[1].ContainerID, []string{"python3", "-c",
		"from pathlib import Path; print(len(list(Path('/root/.cache/pip').rglob('aura_cache_probe-*.whl'))))"})
	if code != 0 || strings.TrimSpace(out) != "0" {
		t.Fatalf("B can see A's cached wheel: exit=%d out=%q", code, out)
	}
	install(boxes[1], "/tmp/first")
	readOrigin(boxes[1], "/tmp/first", "changed-by-B")
	if out := install(boxes[0], "/tmp/second"); !strings.Contains(out, "Using cached") {
		t.Fatalf("the repeat install did not exercise cache reuse: %q", out)
	}
	readOrigin(boxes[0], "/tmp/second", "clean-A")
	t.Log("A reuses its own wheel; B cannot read or replace it with its same-name/version wheel")
}

const pipCacheSourceFixture = `import io, sys, tarfile
files = {
    'setup.py': 'from setuptools import setup\nsetup(name="aura-cache-probe", version="0.0.1", py_modules=["aura_cache_probe"])\n',
    'aura_cache_probe.py': 'def origin():\n    return ' + repr(sys.argv[1]) + '\n',
}
with tarfile.open('/tmp/aura-cache-probe-0.0.1.tar.gz', 'w:gz') as tar:
    for name, content in files.items():
        data = content.encode()
        entry = tarfile.TarInfo('aura-cache-probe-0.0.1/' + name)
        entry.size = len(data)
        tar.addfile(entry, io.BytesIO(data))
`
