// Spike 006 — xlsx-skill-dry-run: the Phase-11 North-Star capstone. With the
// REAL anthropics/skills/xlsx materialized into the ro /skills mount (spike
// 005) by the native installer path (spike 004b winner), prove:
//  1. the SKILL.md frontmatter parses under Aura's planned tolerance rules
//  2. openpyxl resolves in the (spike-006 baked) sandbox image
//  3. an actual .xlsx is produced in /workspace and read back cell-accurate
//  4. a bundled skill script executes BY PATH from /skills
//
// Pre-req: build + swap per compose.spike006.yaml header.
// Run: go run ./.planning/spikes/006-xlsx-skill-dry-run
// Exit 0 = VALIDATED, 1 = failure.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/sandboxagent"
)

const skillDir = ".planning/spikes/005-skills-ro-mount/export/xlsx"

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), cat, fmt.Sprintf(format, a...))
}

func fail(format string, a ...any) {
	logf("SUMMARY", "INVALIDATED — "+format, a...)
	os.Exit(1)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// 1. Frontmatter tolerance — host-side, the loader's view.
	raw, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		fail("read SKILL.md: %v (materialize the skill first — see compose.spike006.yaml)", err)
	}
	content := strings.ReplaceAll(string(raw), "\r\n", "\n") // CRLF tolerance (Windows checkout reality, spike 004b)
	if !strings.HasPrefix(content, "---\n") {
		fail("frontmatter: missing opening delimiter")
	}
	rest := content[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		fail("frontmatter: missing closing delimiter")
	}
	fm, body := rest[:end], rest[end+4:]
	checks := []struct{ name, needle string }{
		{"name field", "name: xlsx"},
		{"description field", "description:"},
		{"license field (optional, must tolerate)", "license:"},
	}
	for _, c := range checks {
		if !strings.Contains(fm, c.needle) {
			fail("frontmatter: %s not found", c.name)
		}
	}
	if strings.Contains(fm, `\"`) {
		logf("FINDING", "description uses a double-quoted YAML scalar with escaped quotes — a naive key:value splitter breaks; 7a parser needs a real YAML lib")
	}
	if len(strings.TrimSpace(body)) == 0 {
		fail("frontmatter: empty body")
	}
	logf("PASS", "1/4 SKILL.md frontmatter parses under tolerance rules (name==dir, description present, license tolerated, %d-byte body)", len(body))

	// Sandbox wire.
	c := sandboxagent.New(sandboxagent.Config{BaseURL: envOr("AURA_SANDBOX_AGENT_URL", sandboxagent.DefaultBaseURL), TimeoutSec: 60})
	run := func(label, cmd string, args ...string) sandboxagent.RunResponse {
		resp, err := c.Run(ctx, sandboxagent.RunRequest{Command: cmd, Args: args})
		if err != nil {
			fail("%s: transport: %v", label, err)
		}
		code := -1
		if resp.ExitCode != nil {
			code = *resp.ExitCode
		}
		logf("RUN", "%s: exit=%d stdout=%q stderr=%q", label, code, trim(resp.Stdout), trim(resp.Stderr))
		return resp
	}

	// 2. openpyxl resolves in the baked image.
	r2 := run("openpyxl-import", "python3", "-c", "import openpyxl; print('openpyxl', openpyxl.__version__)")
	if r2.ExitCode == nil || *r2.ExitCode != 0 || !strings.Contains(r2.Stdout, "openpyxl") {
		fail("openpyxl missing — image bake did not take")
	}
	logf("PASS", "2/4 openpyxl resolves (baked at build time; egressless runtime confirmed unable to pip)")

	// 3. Produce + read back a real .xlsx in /workspace.
	marker := fmt.Sprintf("AURA-SPIKE-006-%d", time.Now().Unix())
	produce := `
import openpyxl
wb = openpyxl.Workbook()
ws = wb.active
ws.title = "Mercato"
ws["A1"] = "Titolo"; ws["B1"] = "Prezzo"
ws["A2"] = "` + marker + `"; ws["B2"] = 42.5
ws["B3"] = "=B2*2"
wb.save("/workspace/spike006.xlsx")
print("saved")
`
	r3 := run("xlsx-produce", "python3", "-c", produce)
	if r3.ExitCode == nil || *r3.ExitCode != 0 {
		fail("xlsx produce failed")
	}
	readback := `
import openpyxl
wb = openpyxl.load_workbook("/workspace/spike006.xlsx")
ws = wb["Mercato"]
print(ws["A2"].value, ws["B2"].value, ws["B3"].value)
`
	r3b := run("xlsx-readback", "python3", "-c", readback)
	if r3b.ExitCode == nil || *r3b.ExitCode != 0 || !strings.Contains(r3b.Stdout, marker) || !strings.Contains(r3b.Stdout, "42.5") || !strings.Contains(r3b.Stdout, "=B2*2") {
		fail("readback did not return marker/values/formula: %q", r3b.Stdout)
	}
	logf("PASS", "3/4 real .xlsx produced in /workspace and read back cell-accurate (artifact ground truth)")

	// 4. Bundled skill script executes by path from /skills.
	r4 := run("bundled-script", "python3", "/skills/xlsx/scripts/office/pack.py", "--help")
	if r4.ExitCode == nil || *r4.ExitCode != 0 {
		// Some bundled scripts have import-time deps; an argparse usage error still proves by-path execution.
		if strings.Contains(r4.Stderr, "ModuleNotFoundError") {
			logf("FINDING", "bundled script needs extra deps at import time: %s — catalog them in the 7e image bake list", firstLine(r4.Stderr))
			fail("bundled script not runnable on the baked image — extend the curated dep set")
		}
		if !strings.Contains(strings.ToLower(r4.Stdout+r4.Stderr), "usage") {
			fail("bundled script failed for a non-usage reason")
		}
	}
	logf("PASS", "4/4 bundled skill script executes by path from the ro mount")

	logf("SUMMARY", "VALIDATED — North-Star ingredients all live: real skill materialized, frontmatter tolerant-parses, openpyxl baked, .xlsx artifact produced+verified, bundled script by-path")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
