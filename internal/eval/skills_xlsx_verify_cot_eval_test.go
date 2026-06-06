//go:build cot_eval

// skills_xlsx_verify_cot_eval_test.go carries the artifact ground-truth half of the
// xlsx North-Star E2E (split from skills_cot_eval_test.go per the 600-LOC cap):
// the sandbox-side openpyxl read-back that proves the produced workbook exists,
// opens, and contains today's data (artifact-not-reply, D-35).
package eval

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/sandboxagent"
)

// verifyXlsxArtifact is the artifact-not-reply ground truth (D-35): it locates the
// FRESH .xlsx the run produced in the sandbox workspace (newer than runStart, the
// spike-012a freshXlsxInSandbox discipline) and re-opens it via a sandbox_exec openpyxl
// read-back, asserting the workbook parses AND carries today's date. It NEVER inspects
// the model's prose — the file on disk is the truth.
func verifyXlsxArtifact(t *testing.T, ctx context.Context, sandboxClient *sandboxagent.Client, exp *skillsExpect, runStart time.Time, res *skillsResult) {
	t.Helper()
	// Today's date in every encoding a live artifact legitimately uses. The
	// scenario prompt is Italian, so the produced sheet writes 05/06/2026 (or the
	// spelled-out month) at least as often as ISO — the assertion's INTENT is
	// "contains today's data", not "contains today's date in ISO 8601". Run-5
	// proved the single-format scan rejects a genuinely-fresh artifact.
	now := time.Now()
	itMonths := []string{"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno",
		"luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"}
	todayForms := []string{
		now.Format("2006-01-02"), // ISO
		now.Format("02/01/2006"), // IT dd/mm/yyyy
		fmt.Sprintf("%d/%d/%d", now.Day(), int(now.Month()), now.Year()), // IT d/m/yyyy
		now.Format("02-01-2006"), // IT dd-mm-yyyy
		fmt.Sprintf("%d %s %d", now.Day(), itMonths[now.Month()-1], now.Year()), // IT spelled-out
		now.Format("January 2, 2006"),                                           // EN spelled-out
	}
	formsPy := make([]string, len(todayForms))
	for i, f := range todayForms {
		formsPy[i] = "'" + strings.ReplaceAll(f, "'", "") + "'"
	}
	today := strings.Join(todayForms, " | ")
	const marker = "AURA-XLSX-VERIFIED"
	// runStart as a Unix epoch float: the freshness floor. A .xlsx whose mtime is
	// before the run started is a stale artifact from a prior run, not THIS run's
	// output (spike-012a -newermt run-start discipline).
	startEpoch := runStart.Unix()
	// One python read-back: find the newest *.xlsx under /workspace produced AFTER the
	// run started, open it with openpyxl, scan every cell's string form for ANY
	// accepted encoding of today's date, and print a marker + the path. Exit 0 +
	// marker = the file exists, is fresh, opens, and contains today's data.
	py := fmt.Sprintf(`
import glob, os, sys
START = %d
files = sorted(glob.glob('/workspace/**/*%s', recursive=True), key=os.path.getmtime)
if not files:
    print('NO_XLSX'); sys.exit(0)
path = files[-1]
print('XLSX_PATH=' + path)
if os.path.getmtime(path) >= START:
    print('XLSX_FRESH')
else:
    print('XLSX_STALE')
try:
    import %s
    wb = %s.load_workbook(path, read_only=True, data_only=True)
except Exception as e:
    print('OPEN_FAIL=' + str(e)); sys.exit(0)
print('XLSX_OPENS')
dates = [%s]
found = False
for ws in wb.worksheets:
    for row in ws.iter_rows(values_only=True):
        for cell in row:
            if cell is not None and any(d in str(cell) for d in dates):
                found = True
                break
        if found: break
    if found: break
print('TODAY_FOUND' if found else 'TODAY_MISSING')
print('%s')
`, startEpoch, exp.xlsxExt, exp.readBackImport, exp.readBackImport, strings.Join(formsPy, ", "), marker)

	run, err := sandboxClient.Run(ctx, sandboxagent.RunRequest{Command: "python3", Args: []string{"-c", py}})
	if err != nil {
		res.notes = append(res.notes, "xlsx read-back exec error: "+err.Error())
		t.Errorf("xlsx read-back exec: %v", err)
		return
	}
	out := run.Stdout
	if strings.Contains(out, "NO_XLSX") {
		res.notes = append(res.notes, "no .xlsx found in /workspace")
		return
	}
	if i := strings.Index(out, "XLSX_PATH="); i >= 0 {
		rest := out[i+len("XLSX_PATH="):]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			res.xlsxPath = strings.TrimSpace(rest[:nl])
		}
	}
	res.xlsxFresh = strings.Contains(out, "XLSX_FRESH")
	// xlsxExists asserts the run PRODUCED the artifact — a stale .xlsx from a prior run
	// must not satisfy the hard floor (spike-012a freshness discipline).
	res.xlsxExists = res.xlsxFresh
	res.xlsxOpens = strings.Contains(out, "XLSX_OPENS")
	res.xlsxToday = strings.Contains(out, "TODAY_FOUND")
	if !res.xlsxFresh {
		res.notes = append(res.notes, "newest .xlsx is older than the run start (stale, not this run's output): "+oneLine(out))
	}
	if !res.xlsxOpens {
		res.notes = append(res.notes, "xlsx did not open: "+oneLine(out))
	}
	if !res.xlsxToday {
		res.notes = append(res.notes, "xlsx missing today's date ("+today+")")
	}
}
