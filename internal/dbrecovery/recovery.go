// Package dbrecovery copies canonical Aura SQLite tables out of a damaged
// database into a freshly migrated database.
package dbrecovery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
)

// Options controls a recovery run.
type Options struct {
	SourcePath   string
	DestPath     string
	Replace      bool
	AllowPartial bool
	BackupPath   string
	Now          func() time.Time
}

// Report describes the recovery result.
type Report struct {
	SourcePath      string        `json:"source_path"`
	DestPath        string        `json:"dest_path"`
	BackupPath      string        `json:"backup_path,omitempty"`
	Replaced        bool          `json:"replaced"`
	Partial         bool          `json:"partial"`
	IntegrityStatus string        `json:"integrity_status"`
	Tables          []TableReport `json:"tables"`
	RowsCopied      int           `json:"rows_copied"`
}

// TableReport describes one table copy attempt.
type TableReport struct {
	Name       string `json:"name"`
	Rows       int    `json:"rows"`
	Skipped    bool   `json:"skipped,omitempty"`
	SkipReason string `json:"skip_reason,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Recover copies canonical rows from SourcePath into DestPath. Derived FTS
// tables are intentionally skipped because Aura rebuilds them from wiki files.
func Recover(ctx context.Context, opts Options) (Report, error) {
	if strings.TrimSpace(opts.SourcePath) == "" {
		return Report{}, errors.New("source path is required")
	}
	opts.SourcePath = filepath.Clean(opts.SourcePath)
	if strings.TrimSpace(opts.DestPath) == "" {
		opts.DestPath = opts.SourcePath + ".recovered"
	}
	opts.DestPath = filepath.Clean(opts.DestPath)
	if samePath(opts.SourcePath, opts.DestPath) {
		return Report{}, errors.New("destination must differ from source")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	report := Report{SourcePath: opts.SourcePath, DestPath: opts.DestPath}
	if err := removeSQLiteFiles(opts.DestPath); err != nil {
		return report, err
	}

	src, err := auradb.OpenReadOnly(opts.SourcePath)
	if err != nil {
		return report, fmt.Errorf("open source: %w", err)
	}
	dst, err := auradb.Open(opts.DestPath)
	if err != nil {
		_ = src.Close()
		return report, fmt.Errorf("open destination: %w", err)
	}

	copyErr := copyCanonicalTables(ctx, src, dst, &report)
	integrityStatus := ""
	if copyErr == nil || opts.AllowPartial {
		integrityStatus, err = auradb.CheckIntegrity(ctx, dst)
		if err != nil {
			copyErr = errors.Join(copyErr, fmt.Errorf("destination integrity check: %w", err))
		} else if integrityStatus != "ok" {
			copyErr = errors.Join(copyErr, fmt.Errorf("destination integrity check = %q", integrityStatus))
		}
	}
	report.IntegrityStatus = integrityStatus

	closeErr := errors.Join(src.Close(), dst.Close())
	if closeErr != nil {
		copyErr = errors.Join(copyErr, closeErr)
	}
	if copyErr != nil && !opts.AllowPartial {
		return report, copyErr
	}
	if copyErr != nil {
		report.Partial = true
	}
	if opts.Replace {
		backupPath, err := replaceSourceWithRecovered(opts, report.DestPath)
		if err != nil {
			return report, errors.Join(copyErr, err)
		}
		report.BackupPath = backupPath
		report.Replaced = true
		report.SourcePath = opts.SourcePath
		report.DestPath = opts.SourcePath
	}
	return report, copyErr
}

func copyCanonicalTables(ctx context.Context, src, dst *sql.DB, report *Report) error {
	if err := migrations.Run(ctx, dst); err != nil {
		return fmt.Errorf("run migrations on destination: %w", err)
	}
	if _, err := dst.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable destination foreign keys: %w", err)
	}

	tables, err := destinationTables(ctx, dst)
	if err != nil {
		return err
	}
	var failures []error
	for _, table := range tables {
		if skip, reason := skipTable(table); skip {
			report.Tables = append(report.Tables, TableReport{Name: table, Skipped: true, SkipReason: reason})
			continue
		}
		exists, err := tableExists(ctx, src, table)
		if err != nil {
			tr := TableReport{Name: table, Error: err.Error()}
			report.Tables = append(report.Tables, tr)
			failures = append(failures, fmt.Errorf("%s: %w", table, err))
			continue
		}
		if !exists {
			report.Tables = append(report.Tables, TableReport{Name: table, Skipped: true, SkipReason: "missing in source"})
			continue
		}
		rows, err := copyTable(ctx, src, dst, table)
		tr := TableReport{Name: table, Rows: rows}
		if err != nil {
			tr.Error = err.Error()
			failures = append(failures, fmt.Errorf("%s: %w", table, err))
		}
		report.Tables = append(report.Tables, tr)
		report.RowsCopied += rows
	}
	if _, err := dst.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		failures = append(failures, fmt.Errorf("enable destination foreign keys: %w", err))
	}
	return errors.Join(failures...)
}

func destinationTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
SELECT name
FROM sqlite_master
WHERE type = 'table'
ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list destination tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, fmt.Errorf("scan destination table: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate destination tables: %w", err)
	}
	sort.Strings(tables)
	return tables, nil
}

func tableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var name string
	err := db.QueryRowContext(ctx, `
SELECT name FROM sqlite_master
WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check table exists: %w", err)
	}
	return true, nil
}

func copyTable(ctx context.Context, src, dst *sql.DB, table string) (int, error) {
	srcCols, err := tableColumns(ctx, src, table)
	if err != nil {
		return 0, fmt.Errorf("source columns: %w", err)
	}
	dstCols, err := tableColumns(ctx, dst, table)
	if err != nil {
		return 0, fmt.Errorf("destination columns: %w", err)
	}
	common := commonColumns(dstCols, srcCols)
	if len(common) == 0 {
		return 0, nil
	}

	selectSQL := fmt.Sprintf("SELECT %s FROM %s NOT INDEXED", quoteList(common), quoteIdent(table))
	rows, err := src.QueryContext(ctx, selectSQL)
	if err != nil {
		return 0, fmt.Errorf("read rows with NOT INDEXED: %w", err)
	}
	defer rows.Close()

	tx, err := dst.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin destination transaction: %w", err)
	}
	defer tx.Rollback()

	insertSQL := fmt.Sprintf("INSERT OR IGNORE INTO %s (%s) VALUES (%s)", quoteIdent(table), quoteList(common), placeholders(len(common)))
	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return 0, fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	count := 0
	values := make([]any, len(common))
	scan := make([]any, len(common))
	for i := range values {
		scan[i] = &values[i]
	}
	for rows.Next() {
		clear(values)
		if err := rows.Scan(scan...); err != nil {
			return count, fmt.Errorf("scan row: %w", err)
		}
		for i, value := range values {
			if bytes, ok := value.([]byte); ok {
				values[i] = append([]byte(nil), bytes...)
			}
		}
		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			return count, fmt.Errorf("insert row: %w", err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("iterate rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return count, fmt.Errorf("commit destination transaction: %w", err)
	}
	return count, nil
}

func tableColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+quoteIdent(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cols, nil
}

func commonColumns(dstCols, srcCols []string) []string {
	srcSet := make(map[string]struct{}, len(srcCols))
	for _, col := range srcCols {
		srcSet[col] = struct{}{}
	}
	common := make([]string, 0, len(dstCols))
	for _, col := range dstCols {
		if _, ok := srcSet[col]; ok {
			common = append(common, col)
		}
	}
	return common
}

func skipTable(table string) (bool, string) {
	switch {
	case table == "schema_migrations":
		return true, "fresh migrations are preserved"
	case table == "wiki_documents" || strings.HasPrefix(table, "wiki_documents_"):
		return true, "derived FTS index is rebuilt from wiki files"
	case strings.HasPrefix(table, "sqlite_"):
		return true, "sqlite internal table"
	default:
		return false, ""
	}
}

func replaceSourceWithRecovered(opts Options, recoveredPath string) (string, error) {
	backupPath := strings.TrimSpace(opts.BackupPath)
	if backupPath == "" {
		stamp := opts.Now().UTC().Format("20060102-150405")
		backupPath = strings.TrimSuffix(opts.SourcePath, filepath.Ext(opts.SourcePath)) + ".corrupt-" + stamp + filepath.Ext(opts.SourcePath)
	}
	if samePath(opts.SourcePath, backupPath) || samePath(recoveredPath, backupPath) {
		return "", errors.New("backup path must differ from source and destination")
	}
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	if err := removeSQLiteFiles(backupPath); err != nil {
		return "", err
	}
	if err := os.Rename(opts.SourcePath, backupPath); err != nil {
		return "", fmt.Errorf("backup source: %w", err)
	}
	if err := moveSQLiteSidecars(opts.SourcePath, backupPath); err != nil {
		_ = os.Rename(backupPath, opts.SourcePath)
		return "", err
	}
	if err := os.Rename(recoveredPath, opts.SourcePath); err != nil {
		_ = os.Rename(backupPath, opts.SourcePath)
		_ = moveSQLiteSidecars(backupPath, opts.SourcePath)
		return "", fmt.Errorf("replace source with recovered database: %w", err)
	}
	_ = removeSQLiteFiles(recoveredPath)
	return backupPath, nil
}

func moveSQLiteSidecars(srcPath, dstPath string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		src := srcPath + suffix
		dst := dstPath + suffix
		if err := os.Rename(src, dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("move %s to %s: %w", src, dst, err)
		}
	}
	return nil
}

func removeSQLiteFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", candidate, err)
		}
	}
	return nil
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		a, b = absA, absB
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func quoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func quoteList(idents []string) string {
	quoted := make([]string, len(idents))
	for i, ident := range idents {
		quoted[i] = quoteIdent(ident)
	}
	return strings.Join(quoted, ", ")
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}
