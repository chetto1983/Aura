package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/memoryquality"
	"github.com/aura/aura/internal/search"
	"github.com/aura/aura/internal/wiki"
)

func main() {
	wikiDir := flag.String("wiki", "", "wiki directory to audit; defaults to WIKI_PATH")
	dbPath := flag.String("db", "", "SQLite database to compare wiki_documents against; defaults to DB_PATH")
	timeout := flag.Duration("timeout", 30*time.Second, "audit timeout")
	jsonOut := flag.Bool("json", false, "print JSON report")
	noDB := flag.Bool("no-db", false, "skip SQLite index manifest checks")
	apply := flag.Bool("apply", false, "apply deterministic wiki memory hygiene before auditing")
	flag.Parse()

	if err := loadDotEnv(config.EnvPathFromEnvironment()); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "warning: could not load env file: %v\n", err)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: load config: %v\n", err)
		os.Exit(2)
	}
	if strings.TrimSpace(*wikiDir) == "" {
		*wikiDir = cfg.WikiPath
	}
	if strings.TrimSpace(*dbPath) == "" {
		*dbPath = cfg.DBPath
	}
	if *noDB {
		*dbPath = ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if *apply {
		cleanReport, err := applyWikiMemoryHygiene(ctx, *wikiDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: apply memory hygiene: %v\n", err)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "applied memory hygiene: pages=%d renamed=%d repaired=%d hubs=%d touched=%d\n",
			cleanReport.Pages,
			len(cleanReport.RenamedPages),
			len(cleanReport.RepairedLinks),
			len(cleanReport.CreatedHubs),
			len(cleanReport.TouchedPages),
		)
		if strings.TrimSpace(*dbPath) != "" {
			docs, pages, err := rebuildSQLiteWikiIndex(ctx, *wikiDir, *dbPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "FAIL: rebuild sqlite wiki index: %v\n", err)
				os.Exit(2)
			}
			fmt.Fprintf(os.Stderr, "rebuilt sqlite wiki index: docs=%d pages=%d\n", docs, pages)
		}
	}
	report, err := memoryquality.Audit(ctx, memoryquality.Options{WikiDir: *wikiDir, DBPath: *dbPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: memory closure audit: %v\n", err)
		os.Exit(2)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		fmt.Print(formatText(report))
	}
	if !report.OK {
		os.Exit(1)
	}
}

func applyWikiMemoryHygiene(ctx context.Context, wikiDir string) (*wiki.MemoryHygieneReport, error) {
	store, err := wiki.NewStore(wikiDir, nil)
	if err != nil {
		return nil, err
	}
	return store.CleanMemory(ctx, wiki.MemoryHygieneOptions{Apply: true})
}

func rebuildSQLiteWikiIndex(ctx context.Context, wikiDir, dbPath string) (int, int, error) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return search.RebuildSQLiteWikiDocuments(ctx, wikiDir, dbPath, logger)
}

func formatText(report memoryquality.Report) string {
	status := "FAIL"
	if report.OK {
		status = "PASS"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s memory_closure wiki=%s", status, report.WikiDir)
	if report.DBPath != "" {
		fmt.Fprintf(&sb, " db=%s", report.DBPath)
	}
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "manifest wiki_pages=%d expected_index_docs=%d actual_index_docs=%d\n",
		report.Manifest.WikiPages,
		report.Manifest.ExpectedIndexDocs,
		report.Manifest.ActualIndexDocs,
	)
	if len(report.Manifest.Categories) > 0 {
		fmt.Fprintf(&sb, "categories=%s\n", strings.Join(report.Manifest.Categories, ","))
	}
	if len(report.Issues) == 0 {
		sb.WriteString("issues=0\n")
		return sb.String()
	}
	fmt.Fprintf(&sb, "issues=%d\n", len(report.Issues))
	for _, issue := range report.Issues {
		fmt.Fprintf(&sb, "- severity=%s kind=%s", issue.Severity, issue.Kind)
		if issue.Ref != "" {
			fmt.Fprintf(&sb, " ref=%s", issue.Ref)
		}
		fmt.Fprintf(&sb, " message=%s\n", issue.Message)
	}
	return sb.String()
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}
