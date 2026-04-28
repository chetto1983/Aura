//go:build linux

package skill

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"
)

const cloneNewNet = 0x40000000

// buildCommand creates an exec.Cmd with sandbox constraints applied.
// On Linux, this applies network namespace isolation (CLONE_NEWNET) for
// skills without network access, and wraps commands with ulimit for
// CPU and memory limit enforcement.
func buildCommand(ctx context.Context, skill *Skill, c Constraints, logger *slog.Logger) *exec.Cmd {
	var cmd *exec.Cmd

	// Wrap with ulimit for CPU/memory constraints
	if c.CPULimit > 0 || c.MemoryMB > 0 {
		script := buildUlimitScript(skill, c)
		cmd = exec.CommandContext(ctx, "sh", "-c", script)
		logger.Info("applied resource limits via ulimit",
			"skill", skill.Name,
			"cpu_limit", c.CPULimit,
			"memory_mb", c.MemoryMB,
		)
	} else {
		cmd = exec.CommandContext(ctx, skill.Command, skill.Args...)
	}

	// Apply network namespace isolation for skills without network access
	if !c.Network {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags: cloneNewNet,
		}
		logger.Info("applied network namespace isolation", "skill", skill.Name)
	}

	return cmd
}

// buildUlimitScript constructs a shell script that sets ulimits before
// executing the skill command.
func buildUlimitScript(skill *Skill, c Constraints) string {
	var parts []string

	if c.CPULimit > 0 {
		parts = append(parts, fmt.Sprintf("ulimit -t %d", c.CPULimit))
	}
	if c.MemoryMB > 0 {
		memKB := c.MemoryMB * 1024
		parts = append(parts, fmt.Sprintf("ulimit -v %d", memKB))
	}

	// Build exec command with proper shell quoting
	execCmd := shellQuote(skill.Command)
	for _, arg := range skill.Args {
		execCmd += " " + shellQuote(arg)
	}
	parts = append(parts, "exec "+execCmd)

	return strings.Join(parts, "; ")
}

// shellQuote wraps a string in single quotes, escaping any embedded quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
