package config

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed defaults/AGENT.md defaults/skills/*/SKILL.md
var defaultRuntimeFS embed.FS

type LayoutConfig struct {
	RuntimeWorkspacePath string
	DBPath               string
	LogDir               string
	WikiPath             string
	SourcesPath          string
	SkillsPath           string
	MCPServersPath       string
	PromptOverlayPath    string
}

func EnsureLayout(cfg LayoutConfig) error {
	runtimeWorkspacePath := strings.TrimSpace(cfg.RuntimeWorkspacePath)
	if runtimeWorkspacePath == "" {
		runtimeWorkspacePath = "."
	}

	dirs := []string{
		runtimeWorkspacePath,
		filepath.Join(runtimeWorkspacePath, "wiki"),
		filepath.Join(runtimeWorkspacePath, "wiki", "graph"),
		filepath.Join(runtimeWorkspacePath, "sources"),
		filepath.Join(runtimeWorkspacePath, "skills"),
		filepath.Join(runtimeWorkspacePath, "inbox"),
		cfg.LogDir,
		cfg.SkillsPath,
		cfg.PromptOverlayPath,
		parentDir(cfg.DBPath),
		parentDir(cfg.MCPServersPath),
	}
	if strings.TrimSpace(cfg.WikiPath) != "" {
		dirs = append(dirs, cfg.WikiPath)
	}
	if strings.TrimSpace(cfg.SourcesPath) != "" {
		dirs = append(dirs, cfg.SourcesPath)
	}
	for _, dir := range dirs {
		if err := mkdir(dir); err != nil {
			return err
		}
	}

	if err := createAgent(runtimeWorkspacePath); err != nil {
		return err
	}
	defaultSkillsPath := strings.TrimSpace(cfg.SkillsPath)
	if defaultSkillsPath == "" {
		defaultSkillsPath = filepath.Join(runtimeWorkspacePath, "skills")
	}
	if err := createDefaultSkills(defaultSkillsPath); err != nil {
		return err
	}
	files := map[string]string{
		filepath.Join(runtimeWorkspacePath, "HEARTBEAT.md"):     heartbeatTemplate,
		filepath.Join(runtimeWorkspacePath, "mcp.json"):         "{}\n",
		filepath.Join(runtimeWorkspacePath, "wiki", "index.md"): "# Wiki Index\n\n",
		filepath.Join(runtimeWorkspacePath, "wiki", "log.md"):   "# Wiki Log\n\n",
		cfg.MCPServersPath: "{}\n",
	}
	if strings.TrimSpace(cfg.WikiPath) != "" {
		files[filepath.Join(cfg.WikiPath, "index.md")] = "# Wiki Index\n\n"
		files[filepath.Join(cfg.WikiPath, "log.md")] = "# Wiki Log\n\n"
	}
	for path, content := range files {
		if err := createFileIfMissing(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func mkdir(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || path == "." {
		return nil
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	return nil
}

func parentDir(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Dir(path)
}

func createAgent(runtimeWorkspacePath string) error {
	path := filepath.Join(runtimeWorkspacePath, "AGENT.md")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	content, err := defaultFile("defaults/AGENT.md", []byte(minimalAgentTemplate))
	if err != nil {
		return err
	}
	return createFileIfMissing(path, content, 0o644)
}

func createDefaultSkills(skillsPath string) error {
	skillsPath = strings.TrimSpace(skillsPath)
	if skillsPath == "" {
		return nil
	}
	entries, err := defaultRuntimeFS.ReadDir("defaults/skills")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read embedded default skills: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		content, err := defaultRuntimeFS.ReadFile(filepath.ToSlash(filepath.Join("defaults", "skills", name, "SKILL.md")))
		if err != nil {
			return fmt.Errorf("read embedded default skill %s: %w", name, err)
		}
		target := filepath.Join(skillsPath, name, "SKILL.md")
		if err := createFileIfMissing(target, content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func defaultFile(path string, fallback []byte) ([]byte, error) {
	content, err := defaultRuntimeFS.ReadFile(filepath.ToSlash(path))
	if err == nil {
		return content, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return fallback, nil
	}
	return nil, fmt.Errorf("read embedded default %s: %w", path, err)
}

func createFileIfMissing(path string, content []byte, perm os.FileMode) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := mkdir(parentDir(path)); err != nil {
		return err
	}
	if err := os.WriteFile(path, content, perm); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	return nil
}

const heartbeatTemplate = `# Heartbeat

Automated heartbeat is disabled until Aura enables a runtime scheduler for this workspace.
`

const minimalAgentTemplate = `# Aura Runtime Schema

This workspace is Aura's bounded runtime workspace.

- Keep compiled knowledge in wiki/.
- Keep immutable source evidence under wiki/raw/.
- Keep runtime skills under skills/.
- Use mcp.json for runtime MCP server configuration.
`
