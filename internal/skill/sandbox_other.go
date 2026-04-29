//go:build !linux

package skill

import (
	"context"
	"log/slog"
	"os/exec"
)

// buildCommand creates an exec.Cmd with sandbox constraints applied.
// On non-Linux platforms, only timeout is enforced via context.
// Network isolation and CPU/memory limits are not available.
func buildCommand(ctx context.Context, skill *Skill, c Constraints, logger *slog.Logger) *exec.Cmd {
	cmd := exec.CommandContext(ctx, skill.Command, skill.Args...)

	if !netAllowed(c.Network) {
		logger.Warn("network isolation not available on this platform",
			"skill", skill.Name,
			"trust", skill.Trust.String(),
		)
	}
	if c.CPULimit > 0 || c.MemoryMB > 0 {
		logger.Warn("CPU/memory limits not enforced on this platform",
			"skill", skill.Name,
			"cpu_limit", c.CPULimit,
			"memory_mb", c.MemoryMB,
		)
	}

	return cmd
}
