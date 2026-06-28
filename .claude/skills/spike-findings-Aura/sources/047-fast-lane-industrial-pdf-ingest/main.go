package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "get cwd: %v\n", err)
		os.Exit(1)
	}

	script := filepath.Join(wd, ".planning", "spikes", "047-fast-lane-industrial-pdf-ingest", "main.py")
	args := append([]string{script}, os.Args[1:]...)
	cmd := exec.Command("python", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "run python harness: %v\n", err)
		os.Exit(1)
	}
}
