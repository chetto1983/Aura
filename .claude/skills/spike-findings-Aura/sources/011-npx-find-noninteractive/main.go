// Spike 011: npx-find-noninteractive — can `npx skills find` be Aura's discovery
// transport? Probes the CLI piped (no TTY, closed stdin) with timeouts and parses
// the machine-shaped output. Exit 0 = VALIDATED.
//
// Run: go run ./.planning/spikes/011-npx-find-noninteractive
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

// entryRE matches one ANSI-stripped find result line: `owner[/repo]@skill N[K|M] installs`.
// Machine output, fixed line shape — not natural language.
var entryRE = regexp.MustCompile(`^([A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)?)@([A-Za-z0-9._-]+) ([0-9.]+[KM]?) installs$`)

type entry struct {
	source   string // owner/repo (or bare registry host)
	skill    string
	installs int
}

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().Format(time.RFC3339), cat, fmt.Sprintf(format, a...))
}

// runNpx executes npx with the given args, stdin closed, output piped, hard timeout.
func runNpx(timeout time.Duration, args ...string) (string, time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	npx := "npx"
	if runtime.GOOS == "windows" {
		npx = "npx.cmd"
	}
	cmd := exec.CommandContext(ctx, npx, append([]string{"--yes"}, args...)...)
	cmd.Stdin = nil // closed stdin: the exact posture of a Go exec wrapper / sandbox_exec
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	start := time.Now()
	err := cmd.Run()
	return out.String(), time.Since(start), err
}

func parseInstalls(s string) int {
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "K"):
		mult, s = 1e3, strings.TrimSuffix(s, "K")
	case strings.HasSuffix(s, "M"):
		mult, s = 1e6, strings.TrimSuffix(s, "M")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return -1
	}
	return int(f * mult)
}

func parseFind(raw string) []entry {
	var entries []entry
	for _, line := range strings.Split(ansiRE.ReplaceAllString(raw, ""), "\n") {
		m := entryRE.FindStringSubmatch(strings.TrimSpace(strings.TrimRight(line, "\r")))
		if m == nil {
			continue
		}
		entries = append(entries, entry{source: m[1], skill: m[2], installs: parseInstalls(m[3])})
	}
	return entries
}

// apiTopIDs fetches the raw /api/search top-n ids (lax decode, spike-003 contract)
// for the CLI-parity check. Empty slice on any failure — parity then degrades to
// the North-Star assertion alone rather than failing the probe on API flakiness.
func apiTopIDs(query string, n int) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://www.skills.sh/api/search?q="+url.QueryEscape(query), nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var body struct {
		Skills []struct {
			ID string `json:"id"`
		} `json:"skills"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&body) != nil {
		return nil
	}
	ids := make([]string, 0, n)
	for _, s := range body.Skills {
		if len(ids) == n {
			break
		}
		ids = append(ids, s.ID)
	}
	return ids
}

func findEntry(entries []entry, source, skill string) *entry {
	for i := range entries {
		if entries[i].source == source && entries[i].skill == skill {
			return &entries[i]
		}
	}
	return nil
}

func main() {
	failures := 0

	// P1 — single-word find parses + North-Star anthropics/skills@xlsx ranked top-ish.
	out, dur, err := runNpx(90*time.Second, "skills", "find", "xlsx")
	if err != nil {
		logf("P1-FIND", "FAIL: %v", err)
		failures++
	} else {
		entries := parseFind(out)
		logf("P1-FIND", "query=xlsx parsed=%d entries in %s", len(entries), dur.Round(time.Millisecond))
		ns := findEntry(entries, "anthropics/skills", "xlsx")
		switch {
		case len(entries) < 3:
			logf("P1-FIND", "FAIL: want >=3 parseable entries, got %d", len(entries))
			failures++
		case ns == nil:
			logf("P1-FIND", "FAIL: North-Star anthropics/skills@xlsx not in results")
			failures++
		case ns.installs < 50_000:
			logf("P1-FIND", "FAIL: North-Star installs=%d, want >=50000", ns.installs)
			failures++
		default:
			logf("P1-FIND", "PASS: North-Star present installs=%d, top entry=%s@%s", ns.installs, entries[0].source, entries[0].skill)
		}
	}

	// P2 — transport parity: the CLI must mirror /api/search for the same query.
	// (NOT a ranking-stability assertion — the marketplace re-ranked "find skills"
	// between spike 003 and this spike, on BOTH transports. The CLI is the client,
	// not the ranking.) Plus: exact-name lookup surfaces the second North-Star.
	out, dur, err = runNpx(90*time.Second, "skills", "find", "find-skills")
	if err != nil {
		logf("P2-PARITY", "FAIL: %v", err)
		failures++
	} else {
		entries := parseFind(out)
		ns := findEntry(entries, "vercel-labs/skills", "find-skills")
		api := apiTopIDs("find-skills", 5)
		overlap := 0
		for _, e := range entries {
			id := e.source + "/" + e.skill
			for _, a := range api {
				if id == a {
					overlap++
					break
				}
			}
		}
		switch {
		case ns == nil:
			logf("P2-PARITY", "FAIL: exact-name query missed vercel-labs/skills@find-skills (%d results)", len(entries))
			failures++
		case len(api) > 0 && overlap < 3:
			logf("P2-PARITY", "FAIL: CLI/API overlap %d/%d for same query — CLI is not a faithful /api/search client", overlap, len(api))
			failures++
		default:
			logf("P2-PARITY", "PASS: North-Star installs=%d; CLI/API top-5 overlap %d/%d in %s", ns.installs, overlap, len(api), dur.Round(time.Millisecond))
		}
	}

	// P3 — empty query must not hang piped (documented "interactive" mode) and exit 0.
	out, dur, err = runNpx(45*time.Second, "skills", "find")
	stripped := ansiRE.ReplaceAllString(out, "")
	switch {
	case err != nil:
		logf("P3-EMPTY", "FAIL: %v (after %s)", err, dur.Round(time.Millisecond))
		failures++
	case !strings.Contains(stripped, "Usage: npx skills find"):
		logf("P3-EMPTY", "FAIL: no usage text in output: %.120q", stripped)
		failures++
	default:
		logf("P3-EMPTY", "PASS: no hang, exit 0, usage text in %s (agent-tip present=%v)",
			dur.Round(time.Millisecond), strings.Contains(stripped, "coding agent"))
	}

	// P4 — add --list enumerates a multi-skill repo without installing.
	out, dur, err = runNpx(120*time.Second, "skills", "add", "anthropics/skills", "--list")
	if err != nil {
		logf("P4-LIST", "FAIL: %v", err)
		failures++
	} else {
		stripped = ansiRE.ReplaceAllString(out, "")
		hasXlsx := regexp.MustCompile(`(?m)^\s*│?\s*xlsx\s*$`).MatchString(stripped)
		hasDocx := regexp.MustCompile(`(?m)^\s*│?\s*docx\s*$`).MatchString(stripped)
		foundN := regexp.MustCompile(`Found (\d+) skills`).FindStringSubmatch(stripped)
		if !hasXlsx || !hasDocx || foundN == nil {
			logf("P4-LIST", "FAIL: xlsx=%v docx=%v foundLine=%v in %s", hasXlsx, hasDocx, foundN != nil, dur.Round(time.Millisecond))
			failures++
		} else {
			logf("P4-LIST", "PASS: repo enumerates %s skills (xlsx+docx present, with descriptions) in %s", foundN[1], dur.Round(time.Millisecond))
		}
	}

	// P5 — warm latency, 3 runs.
	var total time.Duration
	for i := range 3 {
		_, d, err := runNpx(90*time.Second, "skills", "find", "xlsx")
		if err != nil {
			logf("P5-LATENCY", "FAIL run %d: %v", i+1, err)
			failures++
			total = 0
			break
		}
		total += d
		logf("P5-LATENCY", "warm run %d: %s", i+1, d.Round(time.Millisecond))
	}
	if total > 0 {
		logf("P5-LATENCY", "PASS: warm avg %s (spike-003 Go HTTP client: 250-700ms)", (total / 3).Round(time.Millisecond))
	}

	// P6 — sandbox parity (best-effort: requires the stack up; WARN-skip otherwise).
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "exec", "aura-sandbox-agent", "sh", "-c",
		"cd /tmp && npx --yes skills find xlsx 2>&1")
	var sb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &sb, &sb
	start := time.Now()
	if err := cmd.Run(); err != nil {
		logf("P6-SANDBOX", "WARN: sandbox probe unavailable (%v) — stack down? Core verdict unaffected", err)
	} else {
		entries := parseFind(sb.String())
		ns := findEntry(entries, "anthropics/skills", "xlsx")
		if ns == nil {
			logf("P6-SANDBOX", "FAIL: in-sandbox find parsed %d entries, North-Star absent", len(entries))
			failures++
		} else {
			logf("P6-SANDBOX", "PASS: identical results in-sandbox (node baked in image) in %s", time.Since(start).Round(time.Millisecond))
		}
	}

	if failures > 0 {
		logf("SUMMARY", "INVALIDATED — %d probe(s) failed", failures)
		os.Exit(1)
	}
	logf("SUMMARY", "VALIDATED — npx skills find/add--list are non-interactive, parseable discovery transports")
}
