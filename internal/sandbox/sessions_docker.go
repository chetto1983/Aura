package sandbox

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

// ErrDockerCLIAbsent is returned by dockerCLI methods when the docker binary is not
// on PATH. The SessionManager surfaces it from Acquire so a docker-less host fails
// loudly at session-create rather than silently degrading.
var ErrDockerCLIAbsent = errors.New("docker CLI not found on PATH")

// ErrPrivacyModeEgressDenied is the D-10 fail-fast: under AURA_PRIVACY_MODE=local-only
// a non-empty AURA_SANDBOX_NETWORK_ALLOW_HOSTS is an operator misconfiguration
// (silently honoring the allowlist would make "local-only" a hope, not a guarantee
// — D00.5). Acquire returns it so the operator fixes the boundary. Defined here
// (a NEW session-control-plane file) rather than in the 08-02-owned errors.go to
// keep parallel-wave files conflict-free; it is errors.Is-friendly all the same.
var ErrPrivacyModeEgressDenied = errors.New("sandbox egress allowlist set under privacy-mode local-only")

// dockerCLI is the production dockerClient: it shells docker run/stop/rm/ps via
// exec.CommandContext, GATED on exec.LookPath("docker"), with a FIXED argv and
// NEVER any reference to /var/run/docker.sock (D-05, extends T-05-03-EOP-SOCKET).
// The lifecycle carve-out is contained to these four verbs; execution stays HTTP.
type dockerCLI struct{}

var _ dockerClient = dockerCLI{}

func (dockerCLI) run(ctx context.Context, _ string, argv []string) (string, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return "", ErrDockerCLIAbsent
	}
	cmd := exec.CommandContext(ctx, "docker", argv...) //nolint:gosec // fixed argv, no socket
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

func (dockerCLI) stop(ctx context.Context, id string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return ErrDockerCLIAbsent
	}
	cmd := exec.CommandContext(ctx, "docker", "stop", id) //nolint:gosec // fixed argv, no socket
	return cmd.Run()
}

func (dockerCLI) remove(ctx context.Context, id string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return ErrDockerCLIAbsent
	}
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", id) //nolint:gosec // fixed argv, no socket
	return cmd.Run()
}

// listStray returns the IDs of leftover aura-sandbox-sess-* containers (a crashed
// prior Aura's orphans). docker ps -aq --filter name=<prefix> lists stopped ones
// too so the boot sweep can rm them.
func (dockerCLI) listStray(ctx context.Context) ([]string, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, ErrDockerCLIAbsent
	}
	cmd := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "name="+containerNamePrefix) //nolint:gosec // fixed argv, no socket
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(out.String(), "\n") {
		if id := strings.TrimSpace(line); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// NewDockerCLI returns the production dockerClient for the composition root
// (main.go, 08-08) to inject into NewSessionManager.
func NewDockerCLI() dockerClient { return dockerCLI{} }
