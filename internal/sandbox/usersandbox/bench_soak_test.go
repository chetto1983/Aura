//go:build docker_integration

// bench_soak_test.go is the D-14 concurrency-soak harness (SBX-05 success-criterion 5): it
// resolves N (10–20) per-identity boxes CONCURRENTLY against a live dockerd and measures the
// pass envelope — aggregate RAM within the 32 GB budget + headroom, Resolve p95 < ~2 s,
// Resume-from-suspend p95 < ~1 s, and a starvation probe proving the per-box cgroup caps
// (2 CPU / 2 GB / 512 pids start) keep a CPU-hungry box from starving its co-tenants.
//
// IT IS REAL-32GB-HOST ONLY. Dev WSL Docker is capped at 15.47 GiB (spike) — insufficient to
// validate the 10–20-box envelope — and an ordinary CI runner would false-pass or false-fail on
// its own memory cap (threat T-37-08-FALSEBENCH). So the pass/fail assertions run ONLY when the
// operator sets AURA_SANDBOX_SOAK_REALHOST=1 on the 32 GB host; without it the test SKIPS with a
// real-host-only message (a sanctioned skip — this benchmark is deliberately excluded from the
// CI functional tier, unlike the docker_integration correctness tests which t.Fatal under $CI).
// The recorded run fills the 37-VALIDATION.md Manual-Only D-14 row as the Gate-3 evidence.

package usersandbox

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// soakRealHostEnv gates the pass/fail assertions on an explicit real-32GB-host opt-in
	// (T-37-08-FALSEBENCH). Unset ⇒ the test skips; "1" ⇒ it runs and asserts the D-14 envelope.
	soakRealHostEnv = "AURA_SANDBOX_SOAK_REALHOST"

	// D-14 defaults. N is the concurrent-box count (10–20 is the documented envelope); the
	// per-box cgroup caps mirror config.SandboxConfig's D-14 starting points. All are overridable
	// via the same AURA_SANDBOX_* knobs the daemon reads, so the harness measures the deployed caps.
	soakDefaultN          = 10
	soakDefaultCPU        = 2              // AURA_SANDBOX_CPU_LIMIT — CPUs per box
	soakDefaultMemBytes   = int64(2) << 30 // AURA_SANDBOX_MEMORY_LIMIT — 2 GiB per box
	soakDefaultPids       = int64(512)     // AURA_SANDBOX_PIDS_LIMIT — 512 pids per box
	soakDefaultHeadroom   = int64(2) << 30 // AURA_SANDBOX_SOAK_HEADROOM_BYTES — min free RAM after N boxes
	soakResolveP95BoundMs = 2000           // D-14 Resolve p95 < ~2 s
	soakResumeP95BoundMs  = 1000           // D-14 Resume-from-suspend p95 < ~1 s
	soakCoTenantBoundMs   = 2000           // co-tenant exec must stay responsive under a hog (no starvation)
	soakCoTenantSamples   = 20             // co-tenant exec-latency samples for the starvation p95
)

// soakConfig is the resolved D-14 harness configuration (env-overridable).
type soakConfig struct {
	n             int
	limits        Resources
	headroomBytes int64
}

// loadSoakConfig reads the D-14 knobs with their documented defaults. N is clamped to a sane
// [2, 64] band (the envelope is 10–20; the band tolerates a deliberate stress run) and warns if
// pushed outside the documented 10–20 range.
func loadSoakConfig(t *testing.T) soakConfig {
	t.Helper()
	n := soakEnvInt(t, "AURA_SANDBOX_SOAK_N", soakDefaultN)
	if n < 2 {
		n = 2
	}
	if n > 64 {
		n = 64
	}
	if n < 10 || n > 20 {
		t.Logf("WARNING: N=%d is outside the documented D-14 envelope (10–20); the pass criterion is the 32GB fit, not a scale SLA", n)
	}
	cpu := soakEnvInt(t, "AURA_SANDBOX_CPU_LIMIT", soakDefaultCPU)
	mem := soakEnvInt64(t, "AURA_SANDBOX_MEMORY_LIMIT", soakDefaultMemBytes)
	pids := soakEnvInt64(t, "AURA_SANDBOX_PIDS_LIMIT", soakDefaultPids)
	return soakConfig{
		n:             n,
		limits:        Resources{NanoCPUs: int64(cpu) * 1_000_000_000, MemoryBytes: mem, PidsLimit: pids},
		headroomBytes: soakEnvInt64(t, "AURA_SANDBOX_SOAK_HEADROOM_BYTES", soakDefaultHeadroom),
	}
}

func soakEnvInt(t *testing.T, key string, fallback int) int {
	t.Helper()
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("%s=%q is not an int: %v", key, v, err)
	}
	return n
}

func soakEnvInt64(t *testing.T, key string, fallback int64) int64 {
	t.Helper()
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		t.Fatalf("%s=%q is not an int64: %v", key, v, err)
	}
	return n
}

// percentile returns the p (0..1) percentile of ds using the nearest-rank method. For the
// small N of a soak run (10–20) p95 is effectively the worst-but-one sample.
func percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), ds...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	rank := int(math.Ceil(p*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

// readMemInfoKiB parses /proc/meminfo for MemTotal and MemAvailable (both in KiB). ok is false
// on a non-Linux host (no /proc/meminfo) — the caller then skips (the soak is a Linux-host bench).
func readMemInfoKiB() (memTotal, memAvailable int64, ok bool) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	var gotTotal, gotAvail bool
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			memTotal, gotTotal = val, true
		case "MemAvailable:":
			memAvailable, gotAvail = val, true
		}
	}
	return memTotal, memAvailable, gotTotal && gotAvail
}

// soakBurners is the per-box CPU-hog count the starvation probe launches inside the "hog" box.
// Each is a detached busy loop reparented to the box PID 1, so it survives the launching exec and
// keeps burning until the box is Stopped — bounded by the box's own cgroup CPU quota.
const soakBurners = 4

// TestSoak_ConcurrentIdentities is the D-14 concurrency-soak (real-32GB-host only). It resolves N
// per-identity boxes concurrently (Resolve p95), suspends then resumes them concurrently (Resume
// p95), samples the aggregate RAM footprint against the 32GB envelope, and runs a starvation probe
// (a CPU-hog box must not starve a co-tenant under the cgroup caps). All assertions are gated on
// AURA_SANDBOX_SOAK_REALHOST=1 (T-37-08-FALSEBENCH); the metrics are recorded for the Gate-3 table.
func TestSoak_ConcurrentIdentities(t *testing.T) {
	if os.Getenv(soakRealHostEnv) != "1" {
		t.Skipf("D-14 concurrency soak is REAL-32GB-HOST ONLY: dev WSL Docker caps at 15.47 GiB "+
			"(insufficient) and CI would false-pass/fail on its own cap (T-37-08-FALSEBENCH). "+
			"Set %s=1 on the 32GB host and re-run "+
			"(go test -tags docker_integration ./internal/sandbox/usersandbox/ -run TestSoak -v).", soakRealHostEnv)
	}
	skipUnlessDockerd(t)

	memTotal, memAvailBefore, ok := readMemInfoKiB()
	if !ok {
		t.Skip("D-14 soak requires a Linux host with /proc/meminfo (the 32GB appliance)")
	}

	cfg := loadSoakConfig(t)
	cli := newTestDockerClient(t)
	backend := NewDockerBackend(cli, testBoxImage(), cfg.limits)
	ctx := context.Background()

	// ---- Concurrent Resolve: N per-identity boxes, timing each. ----
	handles := make([]BoxHandle, cfg.n)
	resolveDur := make([]time.Duration, cfg.n)
	resolveErr := make([]error, cfg.n)
	ids := make([]string, cfg.n)
	stamp := time.Now().Format("150405")
	var wg sync.WaitGroup
	for i := 0; i < cfg.n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i] = fmt.Sprintf("soak-%s-%02d", stamp, i)
			start := time.Now()
			h, err := backend.Resolve(ctx, SandboxSpec{IdentityID: ids[i]})
			resolveDur[i] = time.Since(start)
			handles[i], resolveErr[i] = h, err
		}(i)
	}
	wg.Wait()

	// Best-effort teardown of every box we managed to create (goleak + no orphan).
	t.Cleanup(func() {
		for i := 0; i < cfg.n; i++ {
			if resolveErr[i] == nil {
				_ = backend.Stop(context.Background(), handles[i])
			}
		}
	})

	for i := 0; i < cfg.n; i++ {
		if resolveErr[i] != nil {
			t.Fatalf("Resolve box %d (%s) failed: %v", i, ids[i], resolveErr[i])
		}
	}

	// ---- Aggregate RAM footprint against the 32GB envelope. ----
	_, memAvailAfter, ok2 := readMemInfoKiB()
	if !ok2 {
		t.Fatal("re-read /proc/meminfo failed mid-run")
	}
	footprintKiB := memAvailBefore - memAvailAfter
	freeAfterBytes := memAvailAfter * 1024

	// ---- Concurrent Suspend (not timed) then Resume (timed) — the D-08 stop-retain path. ----
	for i := 0; i < cfg.n; i++ {
		if err := backend.Suspend(ctx, handles[i]); err != nil {
			t.Fatalf("Suspend box %d failed: %v", i, err)
		}
	}
	resumeDur := make([]time.Duration, cfg.n)
	resumeErr := make([]error, cfg.n)
	var rwg sync.WaitGroup
	for i := 0; i < cfg.n; i++ {
		rwg.Add(1)
		go func(i int) {
			defer rwg.Done()
			start := time.Now()
			resumeErr[i] = backend.Resume(ctx, handles[i])
			resumeDur[i] = time.Since(start)
		}(i)
	}
	rwg.Wait()
	for i := 0; i < cfg.n; i++ {
		if resumeErr[i] != nil {
			t.Fatalf("Resume box %d failed: %v", i, resumeErr[i])
		}
	}

	// ---- Starvation probe: box[0] is the hog, box[1] the co-tenant. ----
	// Launch detached CPU burners in the hog; each outer exec returns immediately and the burn
	// loop reparents to the box PID 1, bounded by the box's own cgroup CPU quota.
	hog := handles[0]
	burn := fmt.Sprintf("i=0; while [ $i -lt %d ]; do (while :; do :; done) & i=$((i+1)); done", soakBurners)
	if _, code := rawExec(t, cli, hog.ContainerID, []string{"/bin/sh", "-c", burn}); code != 0 {
		t.Fatalf("starvation probe: launching hog burners exited %d", code)
	}
	coTenant := handles[1]
	coDur := make([]time.Duration, 0, soakCoTenantSamples)
	for s := 0; s < soakCoTenantSamples; s++ {
		start := time.Now()
		if _, code := rawExec(t, cli, coTenant.ContainerID, []string{"/bin/sh", "-c", "echo ok"}); code != 0 {
			t.Fatalf("co-tenant exec sample %d exited %d", s, code)
		}
		coDur = append(coDur, time.Since(start))
	}

	// ---- Metrics + verdict. ----
	resolveP95 := percentile(resolveDur, 0.95)
	resumeP95 := percentile(resumeDur, 0.95)
	coP95 := percentile(coDur, 0.95)
	noStarvation := coP95 <= time.Duration(soakCoTenantBoundMs)*time.Millisecond

	report := strings.Join([]string{
		"",
		"================ D-14 concurrency-soak (Gate-3 evidence) ================",
		fmt.Sprintf(" host MemTotal        : %.2f GiB", float64(memTotal)/(1024*1024)),
		fmt.Sprintf(" concurrent boxes N   : %d  (caps: %d nanoCPU / %d MiB / %d pids)",
			cfg.n, cfg.limits.NanoCPUs, cfg.limits.MemoryBytes>>20, cfg.limits.PidsLimit),
		fmt.Sprintf(" aggregate footprint  : %.2f GiB (MemAvailable delta)", float64(footprintKiB)/(1024*1024)),
		fmt.Sprintf(" free RAM after N live : %.2f GiB (headroom floor %.2f GiB)",
			float64(freeAfterBytes)/(1<<30), float64(cfg.headroomBytes)/(1<<30)),
		fmt.Sprintf(" Resolve p95          : %v (bound %dms)", resolveP95, soakResolveP95BoundMs),
		fmt.Sprintf(" Resume p95           : %v (bound %dms)", resumeP95, soakResumeP95BoundMs),
		fmt.Sprintf(" co-tenant p95 (hog)  : %v (bound %dms) → starvation-free=%t", coP95, soakCoTenantBoundMs, noStarvation),
		"========================================================================",
		"",
	}, "\n")
	t.Log(report)
	fmt.Println(report)

	// ---- D-14 pass criteria (real-host asserted). ----
	if freeAfterBytes < cfg.headroomBytes {
		t.Errorf("RAM envelope FAIL: only %.2f GiB free after %d boxes, want ≥ %.2f GiB headroom (aggregate did not fit 32GB with headroom)",
			float64(freeAfterBytes)/(1<<30), cfg.n, float64(cfg.headroomBytes)/(1<<30))
	}
	if resolveP95 > time.Duration(soakResolveP95BoundMs)*time.Millisecond {
		t.Errorf("Resolve p95 FAIL: %v > %dms", resolveP95, soakResolveP95BoundMs)
	}
	if resumeP95 > time.Duration(soakResumeP95BoundMs)*time.Millisecond {
		t.Errorf("Resume p95 FAIL: %v > %dms", resumeP95, soakResumeP95BoundMs)
	}
	if !noStarvation {
		t.Errorf("starvation FAIL: co-tenant p95 %v > %dms under a CPU-hog co-tenant — cgroup caps did not prevent starvation",
			coP95, soakCoTenantBoundMs)
	}
}
