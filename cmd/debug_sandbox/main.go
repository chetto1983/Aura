// debug_sandbox is the repeatable smoke harness for Aura's bundled sandbox runtime.
//
//	go run ./cmd/debug_sandbox --smoke
//	go run ./cmd/debug_sandbox --smoke --runtime-dir runtime/pyodide --runner runtime/pyodide/runner/aura-pyodide-runner.cmd
//
// It validates the local Pyodide bundle, starts the runner, and executes the
// offline office/data smoke profile without requiring LLM or Telegram services.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/internal/sandbox"
)

func main() {
	var (
		smoke      = flag.Bool("smoke", false, "run the offline Pyodide package smoke")
		runtimeDir = flag.String("runtime-dir", envDefault("SANDBOX_RUNTIME_DIR", "runtime/pyodide"), "Pyodide runtime directory")
		runnerPath = flag.String("runner", envDefault("SANDBOX_PYODIDE_RUNNER", ""), "Pyodide runner executable/script path")
		timeout    = flag.Duration("timeout", 2*time.Minute, "per-scenario timeout")
	)
	flag.Parse()

	if !*smoke {
		fmt.Fprintln(os.Stderr, "debug_sandbox: pass --smoke to run the offline Pyodide package smoke")
		os.Exit(2)
	}
	if strings.TrimSpace(*runnerPath) == "" {
		*runnerPath = defaultDebugRunnerPath(*runtimeDir)
	}

	runner, err := sandbox.NewPyodideRunner(sandbox.PyodideRunnerConfig{
		RuntimeDir:  *runtimeDir,
		RunnerPath:  *runnerPath,
		Timeout:     *timeout,
		Environment: os.Environ(),
	})
	if err != nil {
		fail("configure runner: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout*time.Duration(len(sandbox.PyodideSmokeScenarios())+1))
	defer cancel()

	fmt.Printf("Aura sandbox Pyodide smoke\n")
	fmt.Printf("runtime_dir=%s\n", *runtimeDir)
	fmt.Printf("runner=%s\n", *runnerPath)
	fmt.Printf("timeout=%s\n\n", timeout.String())

	report := sandbox.RunPyodideSmoke(ctx, runner)
	fmt.Printf("availability: kind=%s available=%v detail=%s\n\n", report.Availability.Kind, report.Availability.Available, report.Availability.Detail)
	if !report.Availability.Available {
		fmt.Printf("FAIL: %s\n", report.Error)
		os.Exit(1)
	}

	for _, result := range report.Scenarios {
		status := "PASS"
		if !result.OK {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s", status, result.Name)
		if result.ElapsedMs > 0 {
			fmt.Printf(" (%dms)", result.ElapsedMs)
		}
		fmt.Println()
		if result.Error != "" {
			fmt.Printf("  error: %s\n", singleLine(result.Error, 300))
		}
		if strings.TrimSpace(result.Stdout) != "" {
			fmt.Printf("  stdout: %s\n", singleLine(result.Stdout, 300))
		}
		if strings.TrimSpace(result.Stderr) != "" {
			fmt.Printf("  stderr: %s\n", singleLine(result.Stderr, 300))
		}
	}
	fmt.Printf("\nelapsed=%dms\n", report.ElapsedMs)
	if !report.OK {
		os.Exit(1)
	}
}

func defaultDebugRunnerPath(runtimeDir string) string {
	name := "aura-pyodide-runner"
	if strings.EqualFold(filepath.Ext(os.Args[0]), ".exe") || os.PathSeparator == '\\' {
		if _, err := os.Stat(filepath.Join(runtimeDir, "runner", name+".cmd")); err == nil {
			return filepath.Join(runtimeDir, "runner", name+".cmd")
		}
		name += ".exe"
	}
	return filepath.Join(runtimeDir, "runner", name)
}

func envDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func singleLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "debug_sandbox: "+format+"\n", args...)
	os.Exit(1)
}
