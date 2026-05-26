package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Migration describes a single database schema change.
type Migration struct {
	Version int
	Name    string
	Up      func(context.Context, *sql.Tx) error
}

var registered = []Migration{
	{Version: 1, Name: "create_current_schema", Up: createCurrentSchema},
	{Version: 2, Name: "backfill_current_columns", Up: backfillCurrentColumns},
	{Version: 3, Name: "add_api_token_expiry", Up: addAPITokenExpiry},
	{Version: 4, Name: "add_compact_memory_index", Up: addCompactMemoryIndex},
	{Version: 5, Name: "add_tool_index_state", Up: addToolIndexState},
	{Version: 6, Name: "add_run_event_foundation", Up: addRunEventFoundation},
	{Version: 7, Name: "add_identity_capability_grants", Up: addIdentityCapabilityGrants},
	{Version: 8, Name: "backfill_allowed_users_identity", Up: backfillAllowedUsersIdentity},
	{Version: 9, Name: "add_secrets_table", Up: addSecretsTable},
	{Version: 10, Name: "add_tool_attempts", Up: addToolAttempts},
	{Version: 11, Name: "add_chat_questions", Up: addChatQuestions},
	{Version: 12, Name: "add_projection_state", Up: addProjectionState},
	{Version: 13, Name: "add_compact_memory_freshness_columns", Up: addCompactMemoryFreshnessColumns},
	{Version: 14, Name: "add_operational_memory_proposal_columns", Up: addOperationalMemoryProposalColumns},
	{Version: 15, Name: "add_agent_notes", Up: addAgentNotes},
	{Version: 16, Name: "add_proposed_updates_actor_id", Up: addProposedUpdatesActorID},
	{Version: 17, Name: "add_compact_memory_source_span_columns", Up: addCompactMemorySourceSpanColumns},
	{Version: 18, Name: "migrate_mistral_api_key_to_secrets", Up: migrateMistralAPIKeyToSecrets},
	{Version: 19, Name: "add_tokenjuice_runs_columns", Up: addTokenJuiceRunsColumns},
	{Version: 20, Name: "add_run_event_origin", Up: addRunEventOrigin},
	{Version: 21, Name: "add_compact_memory_priority", Up: addCompactMemoryPriority},
	{Version: 22, Name: "add_compact_memory_recall_decay", Up: addCompactMemoryRecallDecay},
	{Version: 23, Name: "add_chat_settings_voice_mode", Up: addChatSettingsVoiceMode},
	{Version: 24, Name: "add_voice_dispatches", Up: addVoiceDispatches},
	{Version: 25, Name: "add_timestamp_defaults", Up: addTimestampDefaults},
	{Version: 26, Name: "embed_cache_output_dim", Up: addEmbedCacheOutputDim},
	{Version: 27, Name: "add_conversation_compactions", Up: addConversationCompactions},
	{Version: 28, Name: "add_conversations_channel", Up: addConversationsChannel},
	{Version: 29, Name: "add_prompt_health_views", Up: addPromptHealthViews},
}

type columnDef struct {
	Name string
	SQL  string
}

// Registered returns the migrations known to the runner.
func Registered() []Migration {
	out := make([]Migration, len(registered))
	copy(out, registered)
	return out
}

// Run applies all registered migrations that have not yet been recorded.
func Run(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("migrations: nil db")
	}
	if err := validateRegistered(registered); err != nil {
		return err
	}
	if err := ensureMigrationTable(ctx, db); err != nil {
		return err
	}
	applied, err := appliedMap(ctx, db)
	if err != nil {
		return err
	}
	for _, migration := range registered {
		if applied[migration.Version] {
			continue
		}
		if err := runOne(ctx, db, migration); err != nil {
			return err
		}
	}
	return nil
}

func validateRegistered(migrations []Migration) error {
	seen := make(map[int]struct{}, len(migrations))
	lastVersion := 0
	for i, migration := range migrations {
		if migration.Version <= 0 {
			return fmt.Errorf("migrations: migration %d has non-positive version %d", i, migration.Version)
		}
		if migration.Name == "" {
			return fmt.Errorf("migrations: migration %d has empty name", migration.Version)
		}
		if migration.Up == nil {
			return fmt.Errorf("migrations: migration %d has nil Up", migration.Version)
		}
		if _, ok := seen[migration.Version]; ok {
			return fmt.Errorf("migrations: duplicate version %d", migration.Version)
		}
		if migration.Version <= lastVersion {
			return fmt.Errorf("migrations: versions must be strictly increasing")
		}
		seen[migration.Version] = struct{}{}
		lastVersion = migration.Version
	}
	return nil
}

func ensureMigrationTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at TEXT NOT NULL
);`)
	if err != nil {
		return fmt.Errorf("migrations: ensure schema_migrations: %w", err)
	}
	return nil
}

func appliedMap(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrations: read applied versions: %w", err)
	}
	defer rows.Close()

	applied := map[int]bool{}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("migrations: scan applied version: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrations: iterate applied versions: %w", err)
	}
	return applied, nil
}

func runOne(ctx context.Context, db *sql.DB, migration Migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrations: begin migration %d: %w", migration.Version, err)
	}
	defer tx.Rollback() //nolint:errcheck

	if err := migration.Up(ctx, tx); err != nil {
		return fmt.Errorf("migrations: apply migration %d %q: %w", migration.Version, migration.Name, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		migration.Version,
		migration.Name,
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("migrations: record migration %d: %w", migration.Version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrations: commit migration %d: %w", migration.Version, err)
	}
	return nil
}

func txTableExists(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("migrations: txTableExists %s: %w", name, err)
	}
	return n > 0, nil
}

func parseStoredTime(raw string) (time.Time, error) {
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts, nil
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts, nil
	}
	if ts, err := time.Parse("2006-01-02 15:04:05", raw); err == nil {
		return ts.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp")
}

func addMissingColumns(ctx context.Context, tx *sql.Tx, table string, columns []columnDef) error {
	existing, err := txTableColumns(ctx, tx, table)
	if err != nil {
		return err
	}
	for _, column := range columns {
		if _, ok := existing[column.Name]; ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column.Name+` `+column.SQL); err != nil {
			return fmt.Errorf("migrations: add column %s.%s: %w", table, column.Name, err)
		}
	}
	return nil
}

func txTableColumns(ctx context.Context, tx *sql.Tx, table string) (map[string]struct{}, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, fmt.Errorf("migrations: table info %s: %w", table, err)
	}
	defer rows.Close()

	cols := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, fmt.Errorf("migrations: scan table info %s: %w", table, err)
		}
		cols[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrations: iterate table info %s: %w", table, err)
	}
	return cols, nil
}
