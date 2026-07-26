package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	auraeval "github.com/chetto1983/aura/internal/eval"
)

func inspectAdaptiveBenchmarkGit(
	ctx context.Context,
	commands adaptiveBenchmarkCommandRunner,
	root string,
) (auraeval.AdaptiveBenchmarkGit, error) {
	if commands == nil ||
		root == "" ||
		!filepath.IsAbs(root) ||
		filepath.Clean(root) != root {
		return auraeval.AdaptiveBenchmarkGit{}, errors.New(
			"adaptive benchmark Git inspector is unavailable",
		)
	}
	revisionBytes, err := commands.CombinedOutput(
		ctx,
		"git",
		"-C",
		root,
		"rev-parse",
		"HEAD",
	)
	if err != nil {
		return auraeval.AdaptiveBenchmarkGit{}, fmt.Errorf(
			"read adaptive benchmark Git revision: %w",
			err,
		)
	}
	revision := strings.TrimSpace(string(revisionBytes))
	if !validAdaptiveBenchmarkGitRevision(revision) {
		return auraeval.AdaptiveBenchmarkGit{}, errors.New(
			"adaptive benchmark Git revision is invalid",
		)
	}
	nameCommands := [][]string{
		{"diff", "--name-only", "-z"},
		{"diff", "--cached", "--name-only", "-z"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	}
	changed := make(map[string]struct{})
	for _, arguments := range nameCommands {
		output, commandErr := commands.CombinedOutput(
			ctx,
			"git",
			append([]string{"-C", root}, arguments...)...,
		)
		if commandErr != nil {
			return auraeval.AdaptiveBenchmarkGit{}, fmt.Errorf(
				"read adaptive benchmark Git changes: %w",
				commandErr,
			)
		}
		for _, changedPath := range splitAdaptiveBenchmarkNUL(output) {
			if !validAdaptiveBenchmarkGitPath(changedPath) {
				return auraeval.AdaptiveBenchmarkGit{}, errors.New(
					"adaptive benchmark Git changed path is unsafe",
				)
			}
			changed[changedPath] = struct{}{}
		}
	}
	paths := make([]string, 0, len(changed))
	for changedPath := range changed {
		paths = append(paths, changedPath)
	}
	slices.Sort(paths)
	files := make(
		[]auraeval.AdaptiveBenchmarkChangedFile,
		0,
		len(paths),
	)
	for _, changedPath := range paths {
		digest, hashErr := hashAdaptiveBenchmarkChangedFile(
			ctx,
			commands,
			root,
			changedPath,
		)
		if hashErr != nil {
			return auraeval.AdaptiveBenchmarkGit{}, hashErr
		}
		files = append(files, auraeval.AdaptiveBenchmarkChangedFile{
			Path: changedPath, SHA256: digest,
		})
	}
	return auraeval.AdaptiveBenchmarkGit{
		Revision:     revision,
		Dirty:        len(files) != 0,
		ChangedFiles: files,
	}, nil
}

func splitAdaptiveBenchmarkNUL(payload []byte) []string {
	if len(payload) == 0 {
		return nil
	}
	values := strings.Split(string(payload), "\x00")
	if values[len(values)-1] == "" {
		values = values[:len(values)-1]
	}
	return values
}

func validAdaptiveBenchmarkGitRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func validAdaptiveBenchmarkGitPath(value string) bool {
	return value != "" &&
		utf8.ValidString(value) &&
		!strings.Contains(value, "\\") &&
		!strings.ContainsRune(value, 0) &&
		!strings.HasPrefix(value, "/") &&
		path.Clean(value) == value &&
		value != "." &&
		!strings.HasPrefix(value, "../")
}

func hashAdaptiveBenchmarkChangedFile(
	ctx context.Context,
	commands adaptiveBenchmarkCommandRunner,
	root string,
	changedPath string,
) (string, error) {
	absolute := filepath.Join(root, filepath.FromSlash(changedPath))
	info, err := os.Lstat(absolute)
	if errors.Is(err, os.ErrNotExist) {
		diff, diffErr := commands.CombinedOutput(
			ctx,
			"git",
			"-C",
			root,
			"diff",
			"--binary",
			"HEAD",
			"--",
			changedPath,
		)
		if diffErr != nil || len(diff) == 0 {
			return "", errors.New(
				"hash deleted adaptive benchmark Git change",
			)
		}
		sum := sha256.Sum256(diff)
		return hex.EncodeToString(sum[:]), nil
	}
	if err != nil || info.IsDir() {
		return "", errors.New(
			"adaptive benchmark Git changed file is unavailable",
		)
	}
	hash := sha256.New()
	if info.Mode()&os.ModeSymlink != 0 {
		target, readErr := os.Readlink(absolute)
		if readErr != nil {
			return "", errors.New(
				"read adaptive benchmark Git symlink",
			)
		}
		_, _ = io.WriteString(hash, target)
		return hex.EncodeToString(hash.Sum(nil)), nil
	}
	// #nosec G304 -- changedPath is a validated canonical Git-relative path below root.
	file, err := os.Open(absolute)
	if err != nil {
		return "", errors.New(
			"open adaptive benchmark Git changed file",
		)
	}
	defer func() { _ = file.Close() }()
	if _, err := io.Copy(hash, file); err != nil {
		return "", errors.New(
			"hash adaptive benchmark Git changed file",
		)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
