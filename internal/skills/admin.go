package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// NPXInstaller installs skills from the skills.sh catalog by shelling
// out to `npx skills add <source> [--skill <id>]` from inside the
// configured skills directory. Slice 11c only invokes this behind the
// SKILLS_ADMIN gate; do not call it without that check upstream.
type NPXInstaller struct {
	dir string
}

// NewNPXInstaller pins the install command's working directory to dir
// (the configured SKILLS_PATH). The directory is expanded to an
// absolute path so a relative working dir at process start can't shift
// later if cwd changes.
func NewNPXInstaller(dir string) (*NPXInstaller, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "./skills"
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve skills dir: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create skills dir: %w", err)
	}
	return &NPXInstaller{dir: abs}, nil
}

// Install runs `npx skills add <source> [--skill <id>]` with cwd =
// installer.dir. Source is treated as a literal argv[1] (no shell), so
// callers only need to validate it against an allow-list (see
// catalogSourceRE in api/skills_write.go). Returns combined output for
// the dashboard.
func (i *NPXInstaller) Install(ctx context.Context, source, skillID string) (string, error) {
	if i == nil {
		return "", errors.New("installer not configured")
	}
	args := []string{"--yes", "skills", "add", source}
	if skillID != "" {
		args = append(args, "--skill", skillID)
	}
	cmd := exec.CommandContext(ctx, npxBinary(), args...)
	cmd.Dir = i.dir
	// Hide the inherited environment from the child as much as we can
	// without breaking npm/node lookup. Specifically: keep PATH and
	// HOME/USERPROFILE so npx can find the skills binary, drop the rest.
	cmd.Env = sanitizedEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// FSDeleter removes a single skill directory under the configured
// skills root. Path containment is enforced so a malicious name (e.g.
// `..`, an absolute path, or a symlink) can't reach outside dir.
type FSDeleter struct {
	dir string
}

// NewFSDeleter pins the deleter to dir.
func NewFSDeleter(dir string) (*FSDeleter, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "./skills"
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve skills dir: %w", err)
	}
	return &FSDeleter{dir: abs}, nil
}

// Delete removes the skills/<name> directory. The api layer validates
// name against skillNameRe before this is called; we re-check
// containment defensively because the regex doesn't catch every
// platform-specific edge (Windows path separators, etc.).
func (d *FSDeleter) Delete(name string) error {
	if d == nil {
		return errors.New("deleter not configured")
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("empty name")
	}
	target := filepath.Join(d.dir, name)
	// Re-check containment: filepath.Join collapses traversal segments
	// (e.g. "..") so the final path may escape the parent. Refuse if so.
	if rel, err := filepath.Rel(d.dir, target); err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("invalid skill path")
	}
	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return errSkillNotFoundSentinel
		}
		return err
	}
	// Refuse symlinks: a malicious skill could symlink elsewhere and
	// trick the deleter into removing unrelated files.
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("skill is a symlink — refusing to follow")
	}
	if !info.IsDir() {
		return errors.New("skill path is not a directory")
	}
	return os.RemoveAll(target)
}

// errSkillNotFoundSentinel matches api.ErrSkillNotFound by value so the
// api layer can convert errors.Is(err, api.ErrSkillNotFound) to a 404.
// We don't import the api package here (it would cause a cycle); the
// bot adapter bridges the two with errors.Is + a translate step.
var errSkillNotFoundSentinel = errors.New("skill not found")

// IsSkillNotFound reports whether err signals a missing skill, so the
// api adapter can map it to api.ErrSkillNotFound without importing this
// package's sentinel.
func IsSkillNotFound(err error) bool {
	return errors.Is(err, errSkillNotFoundSentinel)
}

// npxBinary returns the platform-specific npx executable name.
func npxBinary() string {
	if runtime.GOOS == "windows" {
		return "npx.cmd"
	}
	return "npx"
}

// sanitizedEnv keeps only the env vars npx needs to find Node and the
// user profile, dropping the rest so a misconfigured TELEGRAM_TOKEN or
// MISTRAL_API_KEY can't leak into the install subprocess's logs.
func sanitizedEnv(env []string) []string {
	keep := map[string]bool{
		"PATH":         true,
		"PATHEXT":      true,
		"HOME":         true,
		"USERPROFILE":  true,
		"APPDATA":      true,
		"LOCALAPPDATA": true,
		"TEMP":         true,
		"TMP":          true,
		"NODE_PATH":    true,
		"NPM_CONFIG_USERCONFIG": true,
		"NPM_CONFIG_PREFIX":     true,
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		if keep[kv[:eq]] {
			out = append(out, kv)
		}
	}
	return out
}
