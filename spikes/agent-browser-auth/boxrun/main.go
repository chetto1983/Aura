// boxrun drives ONE disposable identity box through Aura's own usersandbox.DockerBackend,
// so the agent-browser spike runs under the production box spec (volumes, tmpfs scratch,
// cgroup caps, egress sidecar floor) instead of a hand-rolled `docker run`.
//
//	go run ./spikes/agent-browser-auth/boxrun resolve
//	go run ./spikes/agent-browser-auth/boxrun exec 'agent-browser --version'   # env from BOX_ENV_* vars
//	go run ./spikes/agent-browser-auth/boxrun put <box path> <local file>        # CopyFileIn, mode 0600
//	go run ./spikes/agent-browser-auth/boxrun suspend | resume | destroy
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/moby/moby/client"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

const identity = "spike-agent-browser"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: boxrun resolve|exec <cmd>|put <path> <file>|suspend|resume|destroy")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cli, err := client.New(client.FromEnv)
	must(err)
	backend := usersandbox.NewDockerBackend(cli, envOr("BOX_IMAGE", "aura-sandbox:spike"),
		usersandbox.Resources{NanoCPUs: 2e9, MemoryBytes: 2 << 30, PidsLimit: 512},
		usersandbox.WithEgress(envOr("BOX_EGRESS_IMAGE", "aura-egress:spike")))
	spec := usersandbox.SandboxSpec{IdentityID: identity, Egress: usersandbox.EgressPolicy{Floor: true}}

	h, err := backend.Resolve(ctx, spec)
	must(err)
	switch os.Args[1] {
	case "resolve":
		fmt.Println("box", h.ContainerID[:12])
	case "exec":
		start := time.Now()
		res, err := backend.Exec(ctx, h, usersandbox.ExecRequest{Command: os.Args[2], Dir: "/workspace", Env: boxEnv()})
		must(err)
		_, _ = os.Stdout.Write(res.Stdout)
		_, _ = os.Stderr.Write(res.Stderr)
		fmt.Fprintf(os.Stderr, "[boxrun] exit=%d in %s\n", res.ExitCode, time.Since(start).Round(time.Millisecond))
		os.Exit(res.ExitCode)
	case "put":
		content, err := os.ReadFile(os.Args[3]) // #nosec G703 -- the operator's own CLI argument, read on the operator's host
		must(err)
		must(backend.CopyFileIn(ctx, h, os.Args[2], content, 0o600))
		fmt.Println("put", os.Args[2])
	case "suspend":
		must(backend.Suspend(ctx, h))
		fmt.Println("suspended")
	case "resume":
		must(backend.Resume(ctx, h))
		fmt.Println("resumed")
	case "destroy":
		must(backend.Stop(ctx, h))
		fmt.Println("destroyed")
	default:
		fmt.Fprintln(os.Stderr, "unknown verb", os.Args[1])
		os.Exit(2)
	}
}

// boxEnv forwards BOX_ENV_<NAME>=v from the caller as NAME=v into the exec.
func boxEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if rest, ok := strings.CutPrefix(kv, "BOX_ENV_"); ok {
			env = append(env, rest)
		}
	}
	return env
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "boxrun:", err)
		os.Exit(1)
	}
}
