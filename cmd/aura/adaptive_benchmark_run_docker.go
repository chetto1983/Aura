package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var (
	adaptiveBenchmarkContainerNamePattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`,
	)
	adaptiveBenchmarkLlamaVersionPattern = regexp.MustCompile(
		`(?m)^version: ([0-9]+) \(([0-9a-f]{7,40})\)$`,
	)
)

type adaptiveBenchmarkCommandRunner interface {
	CombinedOutput(
		context.Context,
		string,
		...string,
	) ([]byte, error)
}

type adaptiveBenchmarkOSCommandRunner struct{}

func (adaptiveBenchmarkOSCommandRunner) CombinedOutput(
	ctx context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	// #nosec G204,G702 -- this unexported runner receives only preregistered Git and Docker probes.
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type dockerAdaptiveBenchmarkQwenInspector struct {
	commands adaptiveBenchmarkCommandRunner
}

func (inspector dockerAdaptiveBenchmarkQwenInspector) Inspect(
	ctx context.Context,
	modelPath string,
) (adaptiveBenchmarkQwenHostProvenance, error) {
	if inspector.commands == nil || modelPath == "" {
		return adaptiveBenchmarkQwenHostProvenance{}, errors.New(
			"qwen Docker inspector is unavailable",
		)
	}
	container, err := inspector.containerForPort(ctx)
	if err != nil {
		return adaptiveBenchmarkQwenHostProvenance{}, err
	}
	imageDigest, err := inspector.inspectConfiguration(
		ctx,
		container,
		modelPath,
	)
	if err != nil {
		return adaptiveBenchmarkQwenHostProvenance{}, err
	}
	artifactSHA256, err := inspector.inspectArtifact(
		ctx,
		container,
		modelPath,
	)
	if err != nil {
		return adaptiveBenchmarkQwenHostProvenance{}, err
	}
	serverVersion, serverRevision, err := inspector.inspectVersion(
		ctx,
		container,
	)
	if err != nil {
		return adaptiveBenchmarkQwenHostProvenance{}, err
	}
	return adaptiveBenchmarkQwenHostProvenance{
		ArtifactSHA256: artifactSHA256,
		ServerVersion:  serverVersion,
		ServerRevision: serverRevision,
		ImageDigest:    imageDigest,
	}, nil
}

func (inspector dockerAdaptiveBenchmarkQwenInspector) containerForPort(
	ctx context.Context,
) (string, error) {
	output, err := inspector.commands.CombinedOutput(
		ctx,
		"docker",
		"ps",
		"--filter",
		"publish=18081",
		"--format",
		"{{.Names}}",
	)
	if err != nil {
		return "", fmt.Errorf("locate Qwen container: %w", err)
	}
	names := strings.Fields(string(output))
	if len(names) != 1 ||
		!adaptiveBenchmarkContainerNamePattern.MatchString(names[0]) {
		return "", errors.New(
			"qwen container mapping is not unique",
		)
	}
	return names[0], nil
}

func (inspector dockerAdaptiveBenchmarkQwenInspector) inspectConfiguration(
	ctx context.Context,
	container string,
	modelPath string,
) (string, error) {
	output, err := inspector.commands.CombinedOutput(
		ctx,
		"docker",
		"inspect",
		container,
		"--format",
		"{{.Image}}\n{{json .Config.Cmd}}",
	)
	if err != nil {
		return "", fmt.Errorf("inspect Qwen container: %w", err)
	}
	lines := strings.SplitN(
		strings.TrimSpace(strings.ReplaceAll(string(output), "\r", "")),
		"\n",
		2,
	)
	if len(lines) != 2 ||
		!strings.HasPrefix(lines[0], "sha256:") ||
		!adaptiveBenchmarkSHA256Pattern.MatchString(
			strings.TrimPrefix(lines[0], "sha256:"),
		) {
		return "", errors.New("qwen container image identity is invalid")
	}
	var command []string
	if err := json.Unmarshal([]byte(lines[1]), &command); err != nil ||
		!adaptiveBenchmarkCommandHasModel(command, modelPath) {
		return "", errors.New(
			"qwen container model binding drifted",
		)
	}
	return lines[0], nil
}

func adaptiveBenchmarkCommandHasModel(
	command []string,
	modelPath string,
) bool {
	matches := 0
	for index := 0; index < len(command); index++ {
		if command[index] != "-m" &&
			command[index] != "--model" {
			continue
		}
		if index+1 >= len(command) ||
			command[index+1] != modelPath {
			return false
		}
		matches++
		index++
	}
	return matches == 1
}

func (inspector dockerAdaptiveBenchmarkQwenInspector) inspectArtifact(
	ctx context.Context,
	container string,
	modelPath string,
) (string, error) {
	output, err := inspector.commands.CombinedOutput(
		ctx,
		"docker",
		"exec",
		container,
		"sha256sum",
		modelPath,
	)
	if err != nil {
		return "", fmt.Errorf("hash Qwen artifact: %w", err)
	}
	fields := strings.Fields(string(output))
	if len(fields) != 2 ||
		fields[1] != modelPath ||
		!adaptiveBenchmarkSHA256Pattern.MatchString(fields[0]) {
		return "", errors.New("qwen artifact hash output is invalid")
	}
	return fields[0], nil
}

func (inspector dockerAdaptiveBenchmarkQwenInspector) inspectVersion(
	ctx context.Context,
	container string,
) (string, string, error) {
	output, err := inspector.commands.CombinedOutput(
		ctx,
		"docker",
		"exec",
		container,
		"/app/llama-server",
		"--version",
	)
	if err != nil {
		return "", "", fmt.Errorf("read llama.cpp version: %w", err)
	}
	match := adaptiveBenchmarkLlamaVersionPattern.FindStringSubmatch(
		strings.ReplaceAll(string(output), "\r", ""),
	)
	if len(match) != 3 {
		return "", "", errors.New("llama.cpp version output is invalid")
	}
	return "b" + match[1], match[2], nil
}
