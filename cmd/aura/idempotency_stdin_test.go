package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestExecuteCLIChildPreservesStdin(t *testing.T) {
	if os.Getenv("AURA_TEST_CLI_STDIN") == "1" {
		os.Exit(executeCLIChild(t.Context(), []string{"-test.run=^TestCLIStdinEchoProcess$"}, os.Stdout, os.Stderr))
	}
	t.Parallel()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), executable, "-test.run=^TestExecuteCLIChildPreservesStdin$")
	command.Env = append(os.Environ(), "AURA_TEST_CLI_STDIN=1")
	const input = "sandbox probe\n/exit\n"
	command.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("CLI subprocess: %v; stderr: %s", err, stderr.String())
	}
	if stdout.String() != input {
		t.Fatalf("subprocess stdin = %q, want %q", stdout.String(), input)
	}
}

func TestCLIStdinEchoProcess(t *testing.T) {
	if os.Getenv("AURA_TEST_CLI_STDIN") != "1" || os.Getenv(cliIdempotencyChildEnv) != "1" {
		return
	}
	if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
