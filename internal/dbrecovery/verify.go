package dbrecovery

import (
	"context"
	"fmt"
	"strings"

	auradb "github.com/aura/aura/internal/db"
)

// VerifyReport summarizes a backup verification pass.
type VerifyReport struct {
	IntegrityStatus string         `json:"integrity_status"`
	RowCounts       map[string]int `json:"row_counts"`
}

// VerifyBackup opens the backup at backupPath read-only, runs PRAGMA
// integrity_check, and asserts each table in expectMinRows has at least the
// specified row count. Returns a non-nil error if any check fails.
func VerifyBackup(ctx context.Context, backupPath string, expectMinRows map[string]int) (VerifyReport, error) {
	if strings.TrimSpace(backupPath) == "" {
		return VerifyReport{}, fmt.Errorf("backup path is required")
	}
	db, err := auradb.OpenReadOnly(backupPath)
	if err != nil {
		return VerifyReport{}, fmt.Errorf("open backup: %w", err)
	}
	defer db.Close()

	status, err := auradb.CheckIntegrity(ctx, db)
	if err != nil {
		return VerifyReport{}, fmt.Errorf("integrity_check: %w", err)
	}
	if status != "ok" {
		return VerifyReport{IntegrityStatus: status}, fmt.Errorf("integrity_check = %q", status)
	}

	report := VerifyReport{
		IntegrityStatus: status,
		RowCounts:       make(map[string]int, len(expectMinRows)),
	}

	var rowErrors []string
	for table, minRows := range expectMinRows {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+quoteIdent(table)).Scan(&count); err != nil {
			rowErrors = append(rowErrors, fmt.Sprintf("%s: %v", table, err))
			continue
		}
		report.RowCounts[table] = count
		if count < minRows {
			rowErrors = append(rowErrors, fmt.Sprintf("%s: got %d rows, want >= %d", table, count, minRows))
		}
	}

	if len(rowErrors) > 0 {
		return report, fmt.Errorf("row count assertions: %s", strings.Join(rowErrors, "; "))
	}
	return report, nil
}
