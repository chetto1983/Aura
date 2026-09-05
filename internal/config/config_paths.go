// config_paths.go defines the per-user path-default + absolute-path-normalization
// helpers config.go's loadBase composes into RunDir/SkillsDir/SkillExportDir/
// WorkspaceDir. Split out of config.go so the root composite stays under the
// 600-LOC cap (CLAUDE.md NO GOD CLASS), mirroring the config_sandbox.go
// sub-struct precedent — this file holds free functions rather than a sub-struct
// because every field it backs is a plain string on the root Config, not a
// cohesive operator-facing knob group.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// absRunDir normalizes an AURA_RUN_DIR value to an absolute path (F-041) so
// sidecars resolve against a stable root, not the process cwd — a relative value
// would make tool-result and conversation sidecars unreadable after a restart
// from a different directory, and read_tool_output hard-fails on a relative root.
// filepath.Abs is idempotent on an already-absolute path (defaultRunDir always is)
// and only errors when the cwd is unobtainable; that error is returned for Validate
// to surface at boot rather than silently keeping a relative path. loadBase reuses
// it verbatim for AURA_WORKSPACE_DIR (Amendment #88) — same "cwd unobtainable" edge
// case, same normalize-not-silently-relative posture.
func absRunDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir, fmt.Errorf("AURA_RUN_DIR=%q could not be resolved to an absolute path: %w", dir, err)
	}
	return abs, nil
}

// defaultRunDir returns a sensible per-user run directory for sidecar tool
// outputs. Falls back to a tmp-based path if user cache is unavailable.
func defaultRunDir() string {
	if cache, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cache, "aura")
	}
	return filepath.Join(os.TempDir(), "aura")
}

// auraHomeDir returns the per-user ~/.aura base the skills tree lives under,
// falling back to a tmp-based path when the home dir is unavailable.
func auraHomeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".aura")
	}
	return filepath.Join(os.TempDir(), "aura")
}

// defaultSkillsDir is the active skill root (AURA_SKILLS_DIR default): ~/.aura/skills.
func defaultSkillsDir() string { return filepath.Join(auraHomeDir(), "skills") }

// defaultWorkspaceDir is the AURA_WORKSPACE_DIR code default: ~/.aura/workspace.
// The in-container deployment default (/workspace, same fixed path the strict-profile
// sandbox box mounts) is supplied by compose env, not code (Amendment #88 §2.1).
func defaultWorkspaceDir() string { return filepath.Join(auraHomeDir(), "workspace") }

// defaultSkillExportDir is the activation export dir (AURA_SKILL_EXPORT_DIR
// default): ~/.aura/skills/export — the ro /skills mount source (D-17).
func defaultSkillExportDir() string { return filepath.Join(defaultSkillsDir(), "export") }

// defaultMCPEnvDir is the AURA_MCP_ENV_DIR code default: ~/.aura/mcp-envs, the root a stdio
// MCP server's prepared environment is materialised under (amendment #211). The container
// default (/var/lib/aura/mcp-envs, on the durable aura-home volume) comes from compose, not
// from here — and it deliberately avoids the three cache mounts, since a named volume seeded
// once is exactly what made a build-time warm-up invisible to every later image.
func defaultMCPEnvDir() string { return filepath.Join(auraHomeDir(), "mcp-envs") }
