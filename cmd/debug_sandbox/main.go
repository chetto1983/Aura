// debug_sandbox is the repeatable smoke harness for Aura's bundled sandbox runtime.
//
//	go run ./cmd/debug_sandbox --smoke
//	go run ./cmd/debug_sandbox --smoke --runtime-dir runtime/pyodide --runner runtime/pyodide/runner/aura-pyodide-runner.cmd
//	go run ./cmd/debug_sandbox --tool-smoke
//	go run ./cmd/debug_sandbox --artifact-smoke
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
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/tools"
)

func main() {
	var (
		smoke         = flag.Bool("smoke", false, "run the offline Pyodide package smoke")
		toolSmoke     = flag.Bool("tool-smoke", false, "run the registered execute_code tool smoke")
		artifactSmoke = flag.Bool("artifact-smoke", false, "run the execute_code artifact egress smoke")
		runtimeDir    = flag.String("runtime-dir", envDefault("SANDBOX_RUNTIME_DIR", "runtime/pyodide"), "Pyodide runtime directory")
		runnerPath    = flag.String("runner", envDefault("SANDBOX_PYODIDE_RUNNER", ""), "Pyodide runner executable/script path")
		timeout       = flag.Duration("timeout", 2*time.Minute, "per-scenario timeout")
	)
	flag.Parse()

	if !*smoke && !*toolSmoke && !*artifactSmoke {
		fmt.Fprintln(os.Stderr, "debug_sandbox: pass --smoke, --tool-smoke, or --artifact-smoke")
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

	if *toolSmoke {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()

		fmt.Printf("Aura sandbox execute_code tool smoke\n")
		fmt.Printf("runtime_dir=%s\n", *runtimeDir)
		fmt.Printf("runner=%s\n", *runnerPath)
		fmt.Printf("timeout=%s\n\n", timeout.String())

		report := runExecuteCodeToolSmoke(ctx, runner)
		fmt.Printf("availability: kind=%s available=%v detail=%s\n\n", report.Availability.Kind, report.Availability.Available, report.Availability.Detail)
		if strings.TrimSpace(report.Output) != "" {
			fmt.Printf("output:\n%s\n", strings.TrimSpace(report.Output))
		}
		if !report.OK {
			fmt.Printf("FAIL: %s\n", report.Error)
			os.Exit(1)
		}
		fmt.Printf("PASS: execute_code returned 5050 through the registered tool boundary\n")
		return
	}

	if *artifactSmoke {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()

		fmt.Printf("Aura sandbox execute_code artifact smoke\n")
		fmt.Printf("runtime_dir=%s\n", *runtimeDir)
		fmt.Printf("runner=%s\n", *runnerPath)
		fmt.Printf("timeout=%s\n\n", timeout.String())

		report := runExecuteCodeArtifactSmoke(ctx, runner)
		fmt.Printf("availability: kind=%s available=%v detail=%s\n\n", report.Availability.Kind, report.Availability.Available, report.Availability.Detail)
		if strings.TrimSpace(report.Output) != "" {
			fmt.Printf("output:\n%s\n", strings.TrimSpace(report.Output))
		}
		if !report.OK {
			fmt.Printf("FAIL: %s\n", report.Error)
			os.Exit(1)
		}
		fmt.Printf("PASS: execute_code returned and persisted CSV + plot artifact metadata\n")
		return
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

type executeCodeToolSmokeReport struct {
	OK           bool
	Availability sandbox.Availability
	Output       string
	Error        string
}

func runExecuteCodeToolSmoke(ctx context.Context, rt sandbox.Runtime) executeCodeToolSmokeReport {
	report := executeCodeToolSmokeReport{}
	if rt == nil {
		report.Availability = sandbox.Availability{Available: false, Kind: sandbox.RuntimeKindUnavailable, Detail: "sandbox runtime unavailable"}
		report.Error = report.Availability.Detail
		return report
	}

	report.Availability = rt.CheckAvailability()
	if report.Availability.Kind == "" {
		report.Availability.Kind = rt.Kind()
	}
	if !report.Availability.Available {
		report.Error = report.Availability.Detail
		return report
	}

	manager, err := sandbox.NewManager(sandbox.Config{Runtime: rt})
	if err != nil {
		report.Error = err.Error()
		return report
	}
	tool := tools.NewExecuteCodeTool(manager)
	if tool == nil {
		report.Error = "execute_code tool did not register"
		return report
	}
	output, err := tool.Execute(ctx, map[string]any{
		"code":          executeCodeToolSmokeProgram(),
		"allow_network": false,
	})
	if err != nil {
		report.Error = err.Error()
		return report
	}
	report.Output = output
	if !strings.Contains(output, "5050") {
		report.Error = "execute_code output did not contain 5050"
		return report
	}
	report.OK = true
	return report
}

func executeCodeToolSmokeProgram() string {
	return "print(sum(range(1, 101)))"
}

func runExecuteCodeArtifactSmoke(ctx context.Context, rt sandbox.Runtime) executeCodeToolSmokeReport {
	report := runExecuteCodeToolSmokeWithProgram(ctx, rt, executeCodeArtifactSmokeProgram(), true)
	if !report.OK {
		return report
	}
	if !strings.Contains(report.Output, "artifacts:") ||
		!strings.Contains(report.Output, "aura_sales_summary.csv") ||
		!strings.Contains(report.Output, "aura_sales_plot.png") {
		report.OK = false
		report.Error = "execute_code output did not contain rich artifact metadata"
		return report
	}
	if strings.Count(report.Output, "persisted=true") < 2 || strings.Count(report.Output, "source_id=src_") < 2 {
		report.OK = false
		report.Error = "execute_code output did not contain persisted artifact source metadata"
		return report
	}
	return report
}

func executeCodeArtifactSmokeProgram() string {
	return strings.TrimSpace(`
import os
import pandas as pd
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

os.makedirs("/tmp/aura_out", exist_ok=True)

sales = pd.DataFrame({
    "month": ["Jan", "Feb", "Mar", "Apr"],
    "revenue": [1200, 1650, 1500, 2100],
    "cost": [700, 900, 850, 1200],
})
sales["profit"] = sales["revenue"] - sales["cost"]
summary = sales.assign(margin=(sales["profit"] / sales["revenue"]).round(3))
summary.to_csv("/tmp/aura_out/aura_sales_summary.csv", index=False)

fig, ax = plt.subplots(figsize=(6, 3.5))
ax.plot(sales["month"], sales["revenue"], marker="o", label="Revenue")
ax.bar(sales["month"], sales["profit"], alpha=0.45, label="Profit")
ax.set_title("Aura sandbox sales smoke")
ax.set_ylabel("EUR")
ax.legend()
fig.tight_layout()
fig.savefig("/tmp/aura_out/aura_sales_plot.png", dpi=140)
plt.close(fig)

print("wrote sales summary csv and plot png")
`)
}

func runExecuteCodeToolSmokeWithProgram(ctx context.Context, rt sandbox.Runtime, code string, persistArtifacts bool) executeCodeToolSmokeReport {
	report := executeCodeToolSmokeReport{}
	if rt == nil {
		report.Availability = sandbox.Availability{Available: false, Kind: sandbox.RuntimeKindUnavailable, Detail: "sandbox runtime unavailable"}
		report.Error = report.Availability.Detail
		return report
	}

	report.Availability = rt.CheckAvailability()
	if report.Availability.Kind == "" {
		report.Availability.Kind = rt.Kind()
	}
	if !report.Availability.Available {
		report.Error = report.Availability.Detail
		return report
	}

	manager, err := sandbox.NewManager(sandbox.Config{Runtime: rt})
	if err != nil {
		report.Error = err.Error()
		return report
	}
	var sourceStore *source.Store
	if persistArtifacts {
		wikiDir, err := os.MkdirTemp("", "aura-debug-sandbox-*")
		if err != nil {
			report.Error = err.Error()
			return report
		}
		defer os.RemoveAll(wikiDir)
		sourceStore, err = source.NewStore(wikiDir, nil)
		if err != nil {
			report.Error = err.Error()
			return report
		}
	}
	tool := tools.NewExecuteCodeToolWithStore(manager, nil, sourceStore)
	if tool == nil {
		report.Error = "execute_code tool did not register"
		return report
	}
	output, err := tool.Execute(ctx, map[string]any{
		"code":          code,
		"allow_network": false,
	})
	if err != nil {
		report.Error = err.Error()
		return report
	}
	report.Output = output
	report.OK = true
	return report
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
