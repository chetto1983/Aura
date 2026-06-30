// config_validate.go is the operator-facing `aura config validate [--profile <p>]
// [--json]` subcommand (PROF-01), slotting beside show|get|set in runConfig. It is a
// THIN presenter over the internal/config per-profile validator (plan 04): every
// validation rule lives in internal/config — this layer only parses flags, loads
// the effective config, renders the aggregated violations (human table or JSON), and
// maps "any Fatal violation" to a non-zero exit (fail-closed, T-33-11). It NEVER reads
// or prints a knob VALUE: the renderer is a pure function of []config.Violation, whose
// messages are value-free, so no secret can reach stdout/CI logs (T-33-10, the
// redactedAPIKey-style guarantee from config.go).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/chetto1983/aura/internal/config"
)

// configValidate is the os.Exit entry point wired into runConfig. The real logic lives
// in the testable runConfigValidate so a test can assert the exit code directly
// (cmd/aura has no subprocess harness — PATTERNS §"No Analog Found").
func configValidate(args []string) {
	os.Exit(runConfigValidate(args, os.Stdout))
}

// runConfigValidate is the testable core: it returns the process exit code instead of
// calling os.Exit. 0 = no Fatal violation; 1 = at least one Fatal (or a config-load
// failure); 2 = flag/usage error. out receives the rendered report (table or --json);
// usage + load errors go to os.Stderr.
func runConfigValidate(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("config validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	profileFlag := fs.String("profile", "", "runtime profile to lint, overriding AURA_PROFILE for this run (dev|local_trusted|single_user_hardened|server_production)")
	jsonOut := fs.Bool("json", false, "emit violations as JSON (CI-parseable) instead of a human table")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: aura config validate [--profile <p>] [--json]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2 // ContinueOnError already printed the error + usage to stderr
	}

	cfg, err := config.LoadServe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config validate: load:", err)
		return 1
	}

	// D-02: --profile overrides AURA_PROFILE for this run so an operator can lint ANY
	// profile without mutating their environment. ParseProfile is total (unknown → dev).
	p := cfg.Profile
	if *profileFlag != "" {
		p = config.ParseProfile(*profileFlag)
	}

	violations := cfg.ValidateProfile(p)

	if *jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(violations); err != nil {
			fmt.Fprintln(os.Stderr, "config validate: encode:", err)
			return 1
		}
	} else {
		renderViolations(out, p, violations)
	}

	if anyFatal(violations) {
		return 1 // fail-closed: any Fatal ⇒ non-zero (T-33-11)
	}
	return 0
}

// renderViolations prints a human table of the aggregated violations. It is a pure
// function of []config.Violation — it NEVER reads or prints a knob VALUE, only the
// knob NAME, severity and the (value-free) message. That IS the redaction guarantee
// (T-33-10): no secret value can reach stdout/CI logs because no value is ever passed
// in. A clean config prints a single "ok" line.
func renderViolations(out io.Writer, p config.RuntimeProfile, violations []config.Violation) {
	if len(violations) == 0 {
		fmt.Fprintf(out, "ok: profile %s — no configuration violations\n", p)
		return
	}
	var fatal, warn int
	for _, v := range violations {
		if v.Sev == config.Fatal {
			fatal++
		} else {
			warn++
		}
	}
	fmt.Fprintf(out, "profile %s — %d violation(s): %d fatal, %d warn\n", p, len(violations), fatal, warn)
	for _, v := range violations {
		fmt.Fprintf(out, "  %-5s %s: %s\n", severityLabel(v.Sev), v.Knob, v.Msg)
	}
}

// severityLabel renders a Severity as an operator-facing token.
func severityLabel(s config.Severity) string {
	if s == config.Fatal {
		return "FATAL"
	}
	return "WARN"
}

// anyFatal reports whether the aggregated set holds at least one Fatal violation — the
// single decision behind the fail-closed exit code.
func anyFatal(violations []config.Violation) bool {
	for _, v := range violations {
		if v.Sev == config.Fatal {
			return true
		}
	}
	return false
}
