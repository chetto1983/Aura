package ingestsupervisor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/chetto1983/aura/internal/procgroup"
)

// ExecLauncher starts the production Python CocoIndex application.
type ExecLauncher struct {
	Command string
	Args    []string
	Env     []string
	Stdout  io.Writer
	Stderr  io.Writer
}

// NewExecLauncher returns the production python -m ingest.app launcher.
func NewExecLauncher() *ExecLauncher {
	return &ExecLauncher{
		Command: "python", Args: []string{"-m", "ingest.app"}, Env: os.Environ(),
		Stdout: os.Stdout, Stderr: os.Stderr,
	}
}

// Start launches one process without exposing its private binding on argv.
func (l *ExecLauncher) Start(ctx context.Context, spec ProcessSpec) (Process, error) {
	if err := os.MkdirAll(filepath.Dir(spec.StateDB), 0o700); err != nil {
		return nil, fmt.Errorf("create identity state directory: %w", err)
	}
	processCtx, cancel := context.WithCancel(ctx)
	command := l.Command
	if command == "" {
		command = "python"
	}
	args := l.Args
	if len(args) == 0 {
		args = []string{"-m", "ingest.app"}
	}
	cmd := exec.CommandContext(processCtx, command, args...) //nolint:gosec // fixed deployment command, private values stay in env.
	procgroup.SetProcessGroup(cmd)
	cmd.Env = spec.Environment(l.Env)
	cmd.Stdout = l.Stdout
	cmd.Stderr = l.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	process := &execProcess{cmd: cmd, cancel: cancel, done: make(chan error, 1)}
	go func() {
		process.done <- cmd.Wait()
		close(process.done)
	}()
	return process, nil
}

type execProcess struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan error
}

func (p *execProcess) Done() <-chan error { return p.done }

func (p *execProcess) Stop(ctx context.Context) error {
	p.cancel()
	_ = procgroup.KillProcessGroup(p.cmd)
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
