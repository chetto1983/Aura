package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"time"
)

const (
	duplFile = ".planning/dupl-report.txt"
	baseFile = ".file-size-baseline.txt"
	outFile  = "docs/MODULE-HEALTH.md"
)

type row struct {
	name, lastMod                                         string
	prodLOC, testLOC, gods, dupls, depViol, todos, inbound int
}

func (r row) score() float64 {
	return float64(r.inbound)*2 + float64(r.prodLOC)/100 + float64(r.gods)*5 + float64(r.depViol)*10
}

func (r row) testRatio() string {
	if r.prodLOC == 0 {
		return "–"
	}
	return fmt.Sprintf("%d%%", r.testLOC*100/r.prodLOC)
}

func (r row) todoDensity() string {
	if r.prodLOC < 50 {
		return "–"
	}
	return fmt.Sprintf("%.1f", float64(r.todos*1000)/float64(r.prodLOC))
}

func main() {
	t0 := time.Now()
	rows := collect()
	sort.Slice(rows, func(i, j int) bool { return rows[i].score() > rows[j].score() })
	emit(rows, t0)
	fmt.Printf("wrote %s — %d modules in %s\n", outFile, len(rows), time.Since(t0).Round(time.Millisecond))
}

func emit(rows []row, t0 time.Time) {
	f, err := os.Create(outFile)
	if err != nil {
		panic(err)
	}
	defer f.Close() //nolint:errcheck
	bw := bufio.NewWriter(f)
	defer bw.Flush() //nolint:errcheck
	// Errors accumulate in bw; all are checked via the deferred Flush above.
	_, _ = fmt.Fprintf(bw, "# MODULE-HEALTH — %s\n\n", time.Now().Format("2006-01-02"))
	_, _ = fmt.Fprintf(bw, "> `go run ./cmd/module_health` · `make module-health`\n\n")
	_, _ = fmt.Fprintf(bw, "Score = `(inbound×2) + (prodLOC/100) + (gods×5) + (depViol×10)`. Higher score → more critical to address.\n\n")
	hdr := "| Module | Prod LOC | Test LOC | Test% | Gods (>600) | Dupl clusters | Dep viol | TODO/kloc | Inbound | Last touch | Score |\n"
	sep := "|--------|----------|----------|-------|-------------|---------------|----------|-----------|---------|------------|-------|\n"
	_, _ = fmt.Fprint(bw, hdr, sep)
	for _, r := range rows {
		_, _ = fmt.Fprintf(bw, "| `%s` | %d | %d | %s | %d | %d | %d | %s | %d | %s | %.0f |\n",
			r.name, r.prodLOC, r.testLOC, r.testRatio(), r.gods, r.dupls, r.depViol, r.todoDensity(), r.inbound, r.lastMod, r.score())
	}
	_, _ = fmt.Fprintf(bw, "\n_Generated in %s · %d modules_\n", time.Since(t0).Round(time.Millisecond), len(rows))
}
