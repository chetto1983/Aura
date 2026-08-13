// The box probe: what it composes, what it does with the answer, and what it does when
// there is no answer.
//
// Two tiers here, and the second is the one that matters. The fake-backend tests pin the
// decode and the fail-open paths; they build the blob from the token they read out of the
// command, so they are self-consistent by construction and can never witness the PRODUCER
// emitting something else (the trap documented at tools' shPrintfEscapes). The
// real-`/bin/sh` test closes that gap: it runs the composed command, unmodified, over a
// real directory tree, so a shell-syntax or framing mistake fails here rather than in a
// container nobody runs in CI.
package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// boxFS is the fake box filesystem a probeBackend answers from: absolute POSIX paths to
// content ("" for an existence-only probe), plus the directories carrying a .git entry.
type boxFS struct {
	files map[string]string
	gits  []string
	home  string
	tmp   string
}

var probeTokenPattern = regexp.MustCompile(`^\{ t='([^']+)';`)

// blob renders exactly what the command's shell would print for this filesystem.
func (fs boxFS) blob(t *testing.T, cmd string, chain []string) []byte {
	t.Helper()
	m := probeTokenPattern.FindStringSubmatch(cmd)
	if m == nil {
		t.Fatalf("probe command does not open by binding its token: %q", cmd)
	}
	separator := "\x00" + m[1] + "\x00"

	var b strings.Builder
	for _, dir := range chain {
		for _, probe := range projectProbeFiles {
			b.WriteString(boolFlag(fs.has(path.Join(dir, probe))))
		}
		b.WriteString(boolFlag(slices.Contains(fs.gits, dir)))
		b.WriteString("\n")
	}
	b.WriteString(separator + fs.home + separator + fs.tmp)
	for _, dir := range chain {
		for _, name := range projectContentFiles {
			b.WriteString(separator + fs.files[path.Join(dir, name)])
		}
	}
	return []byte(b.String())
}

func (fs boxFS) has(p string) bool {
	_, ok := fs.files[p]
	return ok
}

func boolFlag(set bool) string {
	if set {
		return "1"
	}
	return "0"
}

// probeBackend is a usersandbox.Backend that answers the probe from a scripted responder,
// so the real *SandboxRouter — Route, handle, Exec — is what the detector drives.
type probeBackend struct {
	respond   func(cmd string) []byte
	resolveE  error
	execE     error
	execs     []string
	deadlines []time.Time
	block     bool
}

func (b *probeBackend) Resolve(_ context.Context, spec usersandbox.SandboxSpec) (usersandbox.BoxHandle, error) {
	if b.resolveE != nil {
		return usersandbox.BoxHandle{}, b.resolveE
	}
	return usersandbox.BoxHandle{ContainerID: "box-" + spec.IdentityID, IdentityID: spec.IdentityID}, nil
}

func (b *probeBackend) Exec(ctx context.Context, _ usersandbox.BoxHandle, req usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	b.execs = append(b.execs, req.Command)
	if deadline, ok := ctx.Deadline(); ok {
		b.deadlines = append(b.deadlines, deadline)
	} else {
		b.deadlines = append(b.deadlines, time.Time{})
	}
	if b.block {
		<-ctx.Done()
		return usersandbox.ExecResult{}, ctx.Err()
	}
	if b.execE != nil {
		return usersandbox.ExecResult{}, b.execE
	}
	if b.respond == nil {
		return usersandbox.ExecResult{}, nil
	}
	return usersandbox.ExecResult{Stdout: b.respond(req.Command)}, nil
}

func (b *probeBackend) Suspend(context.Context, usersandbox.BoxHandle) error { return nil }
func (b *probeBackend) Resume(context.Context, usersandbox.BoxHandle) error  { return nil }
func (b *probeBackend) Stop(context.Context, usersandbox.BoxHandle) error    { return nil }

func probeRouter(be usersandbox.Backend) *usersandbox.SandboxRouter {
	return usersandbox.NewSandboxRouter(be, config.ProfileSingleUserHardened, config.SandboxConfig{
		Image: "aura-sandbox:latest", CPULimit: 1, MemoryLimit: 1 << 30, PidsLimit: 128, IdleTTLSec: 1800,
	})
}

// answering returns a backend that reports fs for whatever cwd the test asks about.
func answering(t *testing.T, fs boxFS, cwd string) *probeBackend {
	t.Helper()
	be := &probeBackend{}
	be.respond = func(cmd string) []byte { return fs.blob(t, cmd, ancestors(cwd)) }
	return be
}

const (
	probeIdentity = "11111111-1111-1111-1111-111111111111"
	probeCWD      = "/workspace/api/internal"
)

func goProjectFS() boxFS {
	return boxFS{
		files: map[string]string{
			"/workspace/api/go.mod":   "",
			"/workspace/api/Makefile": "test:\n\tgo test ./...\n",
		},
		home: "/root",
		tmp:  "/tmp",
	}
}

func TestBoxDetectorReadsTheProjectFromTheBox(t *testing.T) {
	be := answering(t, goProjectFS(), probeCWD)
	detector := NewBoxProjectDetector(probeRouter(be)).ForIdentity(probeIdentity)

	facts := detector.ProjectFactsFor(probeCWD)
	if !facts.Found || facts.Root != "/workspace/api" {
		t.Fatalf("facts = %+v, want the box's /workspace/api", facts)
	}
	if !slices.Equal(facts.VerifyCommands, []string{"make test"}) {
		t.Fatalf("commands = %v, want [make test] read from the box's Makefile", facts.VerifyCommands)
	}
	if len(be.execs) != 1 {
		t.Fatalf("%d execs, want exactly 1 — the whole chain is probed in one command", len(be.execs))
	}
	// Every path in the command is single-quoted: these come from the model's own tool
	// arguments, so an unquoted one would be a shell injection into the box.
	if !strings.Contains(be.execs[0], "'"+probeCWD+"'") || !strings.Contains(be.execs[0], "'go.mod'") {
		t.Fatalf("probe does not quote its paths: %q", be.execs[0])
	}
}

func TestBoxDetectorProbesEachDirectoryOnce(t *testing.T) {
	be := answering(t, goProjectFS(), probeCWD)
	shared := NewBoxProjectDetector(probeRouter(be))
	detector := shared.ForIdentity(probeIdentity)

	// A turn end asks at least twice (the gate's snapshot, then its status read), and a
	// turn that ends twice asks again. One box exec has to serve all of them.
	for range 3 {
		if facts := detector.ProjectFactsFor(probeCWD); !facts.Found {
			t.Fatalf("facts = %+v, want the memoized project", facts)
		}
	}
	if len(be.execs) != 1 {
		t.Fatalf("%d execs for the same directory, want 1", len(be.execs))
	}

	// A different identity is a different BOX, so it must never read the first one's
	// answer: that would be a cross-tenant fact.
	if facts := shared.ForIdentity("22222222-2222-2222-2222-222222222222").
		ProjectFactsFor(probeCWD); !facts.Found {
		t.Fatalf("facts = %+v for the second identity", facts)
	}
	if len(be.execs) != 2 {
		t.Fatalf("%d execs, want 2 — the memo must be keyed on identity as well as cwd", len(be.execs))
	}
}

func TestBoxDetectorSaysNothingWhenTheBoxCannotAnswer(t *testing.T) {
	cases := map[string]*probeBackend{
		"box cannot be resolved": {resolveE: errors.New("dockerd down")},
		"exec fails in the box":  {execE: errors.New("exec refused")},
		"empty output":           {respond: func(string) []byte { return nil }},
		"output is not the probe's": {respond: func(string) []byte {
			return []byte("bash: syntax error near unexpected token\n")
		}},
		"flag block is short": {respond: func(string) []byte { return []byte("01\n") }},
	}
	for name, be := range cases {
		t.Run(name, func(t *testing.T) {
			detector := NewBoxProjectDetector(probeRouter(be)).ForIdentity(probeIdentity)
			if facts := detector.ProjectFactsFor(probeCWD); facts.Found {
				t.Fatalf("facts = %+v, want Found=false: an unanswerable box must leave the "+
					"policy with nothing to say, never a guess about the host", facts)
			}
		})
	}
}

func TestBoxDetectorDoesNotMemoizeAFailure(t *testing.T) {
	// A dockerd hiccup must not pin "not a project" onto a workspace for the life of the
	// process: the box comes back, and the next turn must see it.
	be := &probeBackend{execE: errors.New("exec refused")}
	detector := NewBoxProjectDetector(probeRouter(be)).ForIdentity(probeIdentity)

	if facts := detector.ProjectFactsFor(probeCWD); facts.Found {
		t.Fatalf("facts = %+v, want Found=false", facts)
	}
	be.execE = nil
	be.respond = func(cmd string) []byte { return goProjectFS().blob(t, cmd, ancestors(probeCWD)) }
	if facts := detector.ProjectFactsFor(probeCWD); !facts.Found {
		t.Fatal("the recovered box was never asked again: a transient failure was cached")
	}
}

func TestBoxDetectorRefusesWithoutABoxToAsk(t *testing.T) {
	// A nil router is the deployment with no sandbox runtime. It must find nothing --
	// falling back to this process's filesystem would answer about paths the BOX named
	// using a filesystem it cannot see, which is a containment break, not a degradation.
	if facts := NewBoxProjectDetector(nil).ForIdentity(probeIdentity).ProjectFactsFor(probeCWD); facts.Found {
		t.Fatalf("facts = %+v for a nil router, want Found=false", facts)
	}
	// And an unscoped caller has no box of its own: routing anyway would answer from the
	// seeded `local` identity's filesystem, which is another tenant.
	be := answering(t, goProjectFS(), probeCWD)
	if facts := NewBoxProjectDetector(probeRouter(be)).ForIdentity("").ProjectFactsFor(probeCWD); facts.Found {
		t.Fatalf("facts = %+v for an unscoped caller, want Found=false", facts)
	}
	if len(be.execs) != 0 {
		t.Fatalf("an unscoped caller reached a box: %q", be.execs)
	}
}

func TestBoxProbeIsBounded(t *testing.T) {
	// The probe runs on the TURN's own goroutine as it tries to finish. Unbounded, a
	// wedged dockerd holds the turn open for as long as it takes to answer, which is
	// never -- the same hazard verificationReadTimeout exists for.
	be := &probeBackend{block: true}
	started := time.Now()
	facts := NewBoxProjectDetector(probeRouter(be)).ForIdentity(probeIdentity).ProjectFactsFor(probeCWD)

	if facts.Found {
		t.Fatalf("facts = %+v, want Found=false when the box never answered", facts)
	}
	if elapsed := time.Since(started); elapsed > projectProbeTimeout+5*time.Second {
		t.Fatalf("probe took %s: it is not bounded by %s", elapsed, projectProbeTimeout)
	}
	if len(be.deadlines) != 1 || be.deadlines[0].IsZero() {
		t.Fatalf("the probe exec carried no deadline: %v", be.deadlines)
	}
	if remaining := time.Until(be.deadlines[0]); remaining > projectProbeTimeout {
		t.Fatalf("deadline in %s, want at most %s", remaining, projectProbeTimeout)
	}
}

func TestBoxProbeRefusesAnOverCapContentFile(t *testing.T) {
	// Two ways the decode must refuse content: a field for a path the flags never reported
	// as a regular file (a desynced stream), and a file past maxFactFileBytes, which the
	// original refused to read at all rather than pull a 40 MB Makefile into a turn end.
	const root = "/workspace/api"
	fs := goProjectFS()
	fs.files[root+"/Makefile"] = "test:\n\t/bin/true\n" + strings.Repeat("#", maxFactFileBytes)
	be := answering(t, fs, probeCWD)

	facts := NewBoxProjectDetector(probeRouter(be)).ForIdentity(probeIdentity).ProjectFactsFor(probeCWD)
	if !facts.Found || facts.Root != root {
		t.Fatalf("facts = %+v, want the project (found via go.mod)", facts)
	}
	if len(facts.VerifyCommands) != 0 {
		t.Fatalf("commands = %v, want none: an over-cap Makefile is not read", facts.VerifyCommands)
	}
}

func TestBoxProbeTruncatedStreamKeepsWhatItDecoded(t *testing.T) {
	// The overall byte budget can cut the content fields short. What was decoded stands;
	// the rest simply has no content. Silence, not a wrong answer.
	be := &probeBackend{respond: func(cmd string) []byte {
		full := goProjectFS().blob(t, cmd, ancestors(probeCWD))
		return full[:len(full)-10]
	}}
	facts := NewBoxProjectDetector(probeRouter(be)).ForIdentity(probeIdentity).ProjectFactsFor(probeCWD)
	if !facts.Found || facts.Root != "/workspace/api" {
		t.Fatalf("facts = %+v, want the project the flag block still named", facts)
	}
}

// TestBoxProbeCommandRunsInARealShell is the producer half, and the only test here that
// can fail for a reason the fake cannot model: a dash-vs-bash syntax slip, a printf escape
// that does not mean what it reads as, a framing the decoder cannot split. It runs the
// command this code composes, unmodified, against a real tree.
func TestBoxProbeCommandRunsInARealShell(t *testing.T) {
	if runtime.GOOS != "linux" {
		// The box shell is POSIX /bin/sh. Aura runs on Linux containers only, and every
		// gate (CI, WSL) is Linux; a Windows shim would prove nothing about the box.
		t.Skip("the probe command is POSIX sh; run this tier on Linux (WSL or CI)")
	}
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("no POSIX sh on PATH: %v", err)
	}

	root := t.TempDir()
	// A quote in a directory name is the case the quoting rule exists for, and a nested
	// cwd proves the walk up.
	project := filepath.Join(root, "it's a project")
	nested := filepath.Join(project, "internal", "store")
	if err := os.MkdirAll(filepath.Join(nested, "deeper"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o750); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(project, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("go.mod", "module fixture\n")
	write("Makefile", "test:\n\tgo test ./...\nlint:\n\tgolangci-lint run\n")
	write("package.json", `{"scripts":{"build":"tsc"}}`)

	chain := ancestors(nested)
	token := "PROBETOKENFORTHISTEST"
	// #nosec G204 -- the command under test is composed by projectProbeCommand from
	// constants plus t.TempDir() paths; running it is the point of the test.
	out, err := exec.Command(shell, "-c", projectProbeCommand(token, chain)).Output()
	if err != nil {
		// The probe's last command is a `[ -f ]` on a file that is usually absent, so a
		// nonzero status is normal; only a failure to run at all is fatal here.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run probe: %v", err)
		}
	}

	snap, ok := parseProjectProbe(out, "\x00"+token+"\x00", chain)
	if !ok {
		t.Fatalf("the real shell's output did not decode: %q", out)
	}
	if snap.tempDir == "" || snap.homeDir == "" {
		t.Fatalf("snapshot did not carry the box's $HOME/$TMPDIR: %+v", snap)
	}

	facts := projectFactsFrom(snap, nested)
	if !facts.Found || facts.Root != project {
		t.Fatalf("facts = %+v, want the .git root %q", facts, project)
	}
	// npm because there is no lockfile; the Makefile targets follow in file order.
	want := []string{"npm run build", "make test", "make lint"}
	if !slices.Equal(facts.VerifyCommands, want) {
		t.Fatalf("commands = %v, want %v", facts.VerifyCommands, want)
	}
}
