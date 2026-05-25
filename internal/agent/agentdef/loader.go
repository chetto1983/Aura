package agentdef

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	pathpkg "path"
	"sort"
	"strings"
)

type Loader struct {
	BuiltinFS fs.FS
	UserDir   string
	Logger    *slog.Logger
}

func (l *Loader) Load(ctx context.Context) ([]AgentDefinition, error) {
	if l == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var out []AgentDefinition
	seen := map[string]int{}
	add := func(def AgentDefinition) {
		id := strings.TrimSpace(def.ID)
		if id == "" {
			out = append(out, def)
			return
		}
		if idx, ok := seen[id]; ok {
			out[idx] = def
			return
		}
		seen[id] = len(out)
		out = append(out, def)
	}

	builtin, err := l.loadFS(ctx, "builtin", l.BuiltinFS)
	if err != nil {
		return nil, err
	}
	for _, def := range builtin {
		add(def)
	}

	userFS, ok, err := userDefinitionsFS(l.UserDir)
	if err != nil {
		return nil, err
	}
	if ok {
		user, err := l.loadFS(ctx, "user", userFS)
		if err != nil {
			return nil, err
		}
		for _, def := range user {
			add(def)
		}
	}
	return out, nil
}

func userDefinitionsFS(dir string) (fs.FS, bool, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, false, nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("agentdef: stat user dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, false, fmt.Errorf("agentdef: user dir %q is not a directory", dir)
	}
	return os.DirFS(dir), true, nil
}

func (l *Loader) loadFS(ctx context.Context, label string, root fs.FS) ([]AgentDefinition, error) {
	if root == nil {
		return nil, nil
	}
	var paths []string
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d == nil || d.IsDir() || pathpkg.Base(p) != "agent.json" {
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("agentdef: walk %s definitions: %w", label, err)
	}
	sort.Strings(paths)

	defs := make([]AgentDefinition, 0, len(paths))
	for _, p := range paths {
		def, err := l.loadDefinition(root, label, p)
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	return defs, nil
}

func (l *Loader) loadDefinition(root fs.FS, label, p string) (AgentDefinition, error) {
	data, err := fs.ReadFile(root, p)
	if err != nil {
		return AgentDefinition{}, fmt.Errorf("agentdef: read %s:%s: %w", label, p, err)
	}
	var def AgentDefinition
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&def); err != nil {
		return AgentDefinition{}, jsonDecodeError(label, p, data, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err != nil {
			return AgentDefinition{}, jsonDecodeError(label, p, data, err)
		}
		return AgentDefinition{}, fmt.Errorf("agentdef: parse %s:%s: trailing JSON data", label, p)
	}
	def.Source = label + ":" + p
	if err := resolvePrompt(root, p, &def); err != nil {
		return AgentDefinition{}, err
	}
	applyDefaults(&def)
	return def, nil
}

func resolvePrompt(root fs.FS, sourcePath string, def *AgentDefinition) error {
	def.ID = strings.TrimSpace(def.ID)
	def.DisplayName = strings.TrimSpace(def.DisplayName)
	def.WhenToUse = strings.TrimSpace(def.WhenToUse)
	def.PromptPath = strings.TrimSpace(def.PromptPath)
	def.ModelHint = strings.TrimSpace(def.ModelHint)
	if def.PromptPath == "" {
		def.Prompt = strings.TrimSpace(def.PromptInline)
		return nil
	}
	cleanPromptPath := pathpkg.Clean(def.PromptPath)
	if !fs.ValidPath(cleanPromptPath) {
		return fmt.Errorf("agentdef: invalid prompt_path %q in %s", def.PromptPath, sourcePath)
	}
	promptPath := pathpkg.Clean(pathpkg.Join(pathpkg.Dir(sourcePath), cleanPromptPath))
	if !fs.ValidPath(promptPath) {
		return fmt.Errorf("agentdef: invalid prompt_path %q in %s", def.PromptPath, sourcePath)
	}
	data, err := fs.ReadFile(root, promptPath)
	if err != nil {
		return fmt.Errorf("agentdef: read prompt_path %s for %s: %w", promptPath, sourcePath, err)
	}
	def.Prompt = strings.TrimSpace(string(data))
	return nil
}

func applyDefaults(def *AgentDefinition) {
	if def.Tier == "" {
		def.Tier = TierWorker
	}
	if def.MaxIterations <= 0 {
		def.MaxIterations = DefaultMaxIterations
	}
	if def.MaxResultChars <= 0 {
		def.MaxResultChars = DefaultMaxResultChars
	}
}

func jsonDecodeError(label, p string, data []byte, err error) error {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		line, col := lineCol(data, syntaxErr.Offset)
		return fmt.Errorf("agentdef: parse %s:%s:%d:%d: %w", label, p, line, col, err)
	}
	return fmt.Errorf("agentdef: parse %s:%s: %w", label, p, err)
}

func lineCol(data []byte, offset int64) (int, int) {
	line, col := 1, 1
	limit := int(offset) - 1
	if limit > len(data) {
		limit = len(data)
	}
	for i := 0; i < limit; i++ {
		if data[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
}
