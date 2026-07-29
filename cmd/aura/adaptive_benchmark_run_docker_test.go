package main

import (
	"context"
	"errors"
	"slices"
	"testing"

	auraeval "github.com/chetto1983/aura/internal/eval"
)

func TestDockerAdaptiveBenchmarkQwenInspectorVerifiesExactRuntime(
	t *testing.T,
) {
	t.Parallel()
	imageDigest := "sha256:" +
		"b57dce073940d6f347d59230d4d28fc947db8948f2eb162326008380b07bab77"
	commands := &adaptiveBenchmarkCommandRunnerFake{
		responses: []adaptiveBenchmarkCommandResponse{
			{output: "aura-qwen35\n"},
			{
				output: imageDigest + "\n" +
					`["-m","` + qwenModelPathFixture +
					`","--host","0.0.0.0","--port","8080"]` + "\n",
			},
			{
				output: auraeval.AdaptiveBenchmarkQwenArtifactSHA256 +
					"  " + qwenModelPathFixture + "\n",
			},
			{
				output: "version: 9994 (14d3ba45f)\n" +
					"built with GNU 14.2.0 for Linux x86_64\n",
			},
		},
	}
	inspector := dockerAdaptiveBenchmarkQwenInspector{commands: commands}

	got, err := inspector.Inspect(t.Context(), qwenModelPathFixture)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	want := adaptiveBenchmarkQwenHostProvenance{
		ArtifactSHA256: auraeval.AdaptiveBenchmarkQwenArtifactSHA256,
		ServerVersion:  "b9994",
		ServerRevision: "14d3ba45f",
		ImageDigest:    imageDigest,
	}
	if got != want {
		t.Fatalf("provenance = %#v, want %#v", got, want)
	}
	wantCommands := [][]string{
		{
			"docker", "ps", "--filter", "publish=18081",
			"--format", "{{.Names}}",
		},
		{
			"docker", "inspect", "aura-qwen35",
			"--format", "{{.Image}}\n{{json .Config.Cmd}}",
		},
		{
			"docker", "exec", "aura-qwen35",
			"sha256sum", qwenModelPathFixture,
		},
		{
			"docker", "exec", "aura-qwen35",
			"/app/llama-server", "--version",
		},
	}
	if !slices.EqualFunc(commands.calls, wantCommands, slices.Equal) {
		t.Fatalf("commands = %#v, want %#v", commands.calls, wantCommands)
	}
}

func TestDockerAdaptiveBenchmarkQwenInspectorRejectsRuntimeDrift(
	t *testing.T,
) {
	t.Parallel()
	validImage := "sha256:" +
		"b57dce073940d6f347d59230d4d28fc947db8948f2eb162326008380b07bab77"
	validInspect := validImage + "\n" +
		`["-m","` + qwenModelPathFixture + `"]` + "\n"
	validHash := auraeval.AdaptiveBenchmarkQwenArtifactSHA256 +
		"  " + qwenModelPathFixture + "\n"
	validVersion := "version: 9994 (14d3ba45f)\n"
	tests := []struct {
		name      string
		responses []adaptiveBenchmarkCommandResponse
	}{
		{
			name: "docker unavailable",
			responses: []adaptiveBenchmarkCommandResponse{{
				err: errors.New("docker unavailable"),
			}},
		},
		{
			name: "no container",
			responses: []adaptiveBenchmarkCommandResponse{{
				output: "",
			}},
		},
		{
			name: "multiple containers",
			responses: []adaptiveBenchmarkCommandResponse{{
				output: "one\ntwo\n",
			}},
		},
		{
			name: "unsafe container name",
			responses: []adaptiveBenchmarkCommandResponse{{
				output: "bad name\n",
			}},
		},
		{
			name: "configured path",
			responses: []adaptiveBenchmarkCommandResponse{
				{output: "aura-qwen35\n"},
				{
					output: validImage + "\n" +
						`["-m","/models/other.gguf"]` + "\n",
				},
			},
		},
		{
			name: "hash",
			responses: []adaptiveBenchmarkCommandResponse{
				{output: "aura-qwen35\n"},
				{output: validInspect},
				{output: "bad\n"},
			},
		},
		{
			name: "version",
			responses: []adaptiveBenchmarkCommandResponse{
				{output: "aura-qwen35\n"},
				{output: validInspect},
				{output: validHash},
				{output: "unknown\n"},
			},
		},
		{
			name: "command failure",
			responses: []adaptiveBenchmarkCommandResponse{
				{output: "aura-qwen35\n"},
				{output: validInspect},
				{output: validHash},
				{output: validVersion, err: errors.New("exit")},
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			inspector := dockerAdaptiveBenchmarkQwenInspector{
				commands: &adaptiveBenchmarkCommandRunnerFake{
					responses: testCase.responses,
				},
			}
			if got, err := inspector.Inspect(
				t.Context(),
				qwenModelPathFixture,
			); err == nil {
				t.Fatalf("accepted provenance %#v", got)
			}
		})
	}
}

type adaptiveBenchmarkCommandResponse struct {
	output string
	err    error
}

type adaptiveBenchmarkCommandRunnerFake struct {
	responses []adaptiveBenchmarkCommandResponse
	calls     [][]string
}

func (fake *adaptiveBenchmarkCommandRunnerFake) CombinedOutput(
	_ context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	call := append([]string{name}, args...)
	fake.calls = append(fake.calls, call)
	if len(fake.responses) == 0 {
		return nil, errors.New("unexpected command")
	}
	response := fake.responses[0]
	fake.responses = fake.responses[1:]
	return []byte(response.output), response.err
}
