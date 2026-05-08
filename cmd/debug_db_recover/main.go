package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aura/aura/internal/dbrecovery"
)

func main() {
	src := flag.String("src", "", "source SQLite database to recover")
	dst := flag.String("dst", "", "destination SQLite database; defaults to <src>.recovered")
	backup := flag.String("backup", "", "backup path when replacing; defaults to aura.corrupt-<timestamp>.db")
	timeout := flag.Duration("timeout", 2*time.Minute, "recovery timeout")
	replace := flag.Bool("replace", false, "replace source database after destination integrity check passes")
	allowPartial := flag.Bool("allow-partial", false, "keep recovered database even if one or more canonical tables fail to copy")
	jsonOut := flag.Bool("json", false, "print JSON report")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	report, err := dbrecovery.Recover(ctx, dbrecovery.Options{
		SourcePath:   *src,
		DestPath:     *dst,
		Replace:      *replace,
		AllowPartial: *allowPartial,
		BackupPath:   *backup,
	})
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		fmt.Printf("recovered rows=%d integrity=%s dest=%s\n", report.RowsCopied, report.IntegrityStatus, report.DestPath)
		for _, table := range report.Tables {
			switch {
			case table.Skipped:
				fmt.Printf("- %s skipped: %s\n", table.Name, table.SkipReason)
			case table.Error != "":
				fmt.Printf("- %s failed after %d rows: %s\n", table.Name, table.Rows, table.Error)
			default:
				fmt.Printf("- %s rows=%d\n", table.Name, table.Rows)
			}
		}
		if report.Replaced {
			fmt.Printf("replaced source; corrupt backup=%s\n", report.BackupPath)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: recover database: %v\n", err)
		os.Exit(2)
	}
}
