// skills subcommand dispatcher for `aura skills {list|info|create|update|delete|
// always|snippet|audit}` (Slice 7c governance, plan 11-05). Hand-rolled switch tree
// mirroring runIdentity/runTask (cmd/aura/identity.go), NOT cobra — go.mod has no
// spf13/cobra and the codebase uses nested switch dispatchers; CLAUDE.md mandates
// following existing patterns. The install/catalog legs were removed in plan 11-09
// (amendment #51 / D-40): discovery+install is the find-skills always-on skill
// driving `npx skills` in the sandbox, not a CLI/tool leg.
//
// This is the operator-side governance channel. create/update/delete apply
// immediately (amendment #97 — there is no approve command, because there is nothing
// to approve); `audit` lists the append-only aura.skill_audit rows. The audit purge
// path SURFACES the role error (aura_app holds SELECT+INSERT only — no
// DELETE/TRUNCATE), proving the append-only ledger at the CLI boundary (SC#2).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"text/tabwriter"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/jackc/pgx/v5/pgxpool"
)

const skillsUsage = "usage: aura skills {list|info <name>|create <name> --desc <d> --body <b> [--always]|" +
	"update <name> --desc <d> --body <b> [--always]|delete <name>|" +
	"always <name> {on|off}|snippet {save <name> --lang <l> --code <c> [--desc <d>] [--needs-network]|exec <name> [args...]}|" +
	"audit [--skill <name>] [--since <RFC3339>]}"

// skillsEnv bundles the booted skills-CLI dependencies: the config (for the skill
// dirs + blocklist), the open pool, the Writer, and the AuditStore.
type skillsEnv struct {
	cfg   *config.Config
	pool  *pgxpool.Pool
	w     *skills.Writer
	audit *skills.AuditStore
}

func (e *skillsEnv) close() {
	if e.pool != nil {
		e.pool.Close()
	}
}

// newSkillWriter builds the live Writer from config: the active root is SkillsDir,
// with archived/ as a sibling (the loader scans SkillsDir; the writer's dirs stay in
// agreement), and the export dir is SkillExportDir (the /skills mount source). It is
// the SAME wiring the composition root uses so the CLI and the model path operate on
// one set of dirs.
func newSkillWriter(cfg *config.Config, pool *pgxpool.Pool) *skills.Writer {
	return skills.NewWriter(skills.WriterConfig{
		Pool:         pool,
		ActiveDir:    cfg.SkillsDir,
		ExportDir:    cfg.SkillExportDir,
		ArchiveDir:   filepath.Join(cfg.SkillsDir, "archived"),
		Blocklist:    cfg.SkillInjectionBlocklist,
		BodyCapBytes: cfg.SkillBodyCapBytes,
	})
}

func bootSkills(ctx context.Context) *skillsEnv {
	cfg := config.LoadDB() // skills CLI is DB-only — no OPENROUTER_API_KEY required
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return &skillsEnv{cfg: cfg, pool: pool, w: newSkillWriter(cfg, pool), audit: skills.NewAuditStore(pool)}
}

func runSkills(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, skillsUsage)
		os.Exit(1)
	}
	ctx := context.Background()
	switch args[0] {
	case "list":
		skillsList(ctx, args[1:])
	case "info":
		skillsInfo(ctx, args[1:])
	case "create":
		skillsWrite(ctx, args[1:], skillsActionCreate)
	case "update":
		skillsWrite(ctx, args[1:], skillsActionUpdate)
	case "delete":
		skillsDelete(ctx, args[1:])
	case "always":
		skillsAlways(ctx, args[1:])
	case "snippet":
		skillsSnippet(ctx, args[1:])
	case "audit":
		skillsAudit(ctx, args[1:])
	default:
		fmt.Fprintln(os.Stderr, skillsUsage)
		os.Exit(1)
	}
}

// skillsActionCreate/Update name the two write actions the CLI authors.
const (
	skillsActionCreate = "create"
	skillsActionUpdate = "update"
)

// skillsList prints the active skills the loader sees (name + description). It
// scans the persistent-install root + the active SkillsDir and applies the load-time
// blocklist, exactly like the model path (amendment #51 / D-40), so `aura skills list`
// reflects what the model would see.
func skillsList(_ context.Context, _ []string) {
	cfg := config.LoadDB()
	loader := skills.NewLoader(skills.Config{Roots: skillLoaderRoots(cfg), BodyCapBytes: cfg.SkillBodyCapBytes, Blocklist: cfg.SkillInjectionBlocklist})
	loaded := loader.List()
	if len(loaded) == 0 {
		fmt.Println("ok: no skills")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tALWAYS\tTYPE\tDESCRIPTION")
	for _, s := range loaded {
		_, _ = fmt.Fprintf(w, "%s\t%v\t%s\t%s\n", s.Name, s.Always, s.Type, s.Description)
	}
	_ = w.Flush()
}

// skillsInfo prints a single skill's body for inspection.
func skillsInfo(_ context.Context, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aura skills info <name>")
		os.Exit(1)
	}
	cfg := config.LoadDB()
	loader := skills.NewLoader(skills.Config{Roots: skillLoaderRoots(cfg), BodyCapBytes: cfg.SkillBodyCapBytes, Blocklist: cfg.SkillInjectionBlocklist})
	s, ok := loader.Get(args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "skill %q not found\n", args[0])
		os.Exit(1)
	}
	fmt.Printf("name: %s\ndescription: %s\nalways: %v\ntype: %s\n\n%s\n", s.Name, s.Description, s.Always, s.Type, s.Body)
}

// skillsWrite applies a create/update mutation. The CLI is operator-authored: it
// validates at the write boundary and the skill is live when the command returns
// (amendment #97). --always marks an always-on skill.
func skillsWrite(ctx context.Context, args []string, action string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: aura skills %s <name> --desc <d> --body <b> [--always]\n", action)
		os.Exit(1)
	}
	name := args[0]
	desc, _ := flagValue(args[1:], "--desc")
	body, _ := flagValue(args[1:], "--body")
	always := hasFlag(args[1:], "--always")
	if desc == "" || body == "" {
		fmt.Fprintln(os.Stderr, "skills "+action+": --desc and --body are required")
		os.Exit(1)
	}

	env := bootSkills(ctx)
	defer env.close()

	status, err := env.w.WriteMutationCLI(ctx, action, name, desc, body, always)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("ok: %s %q %s\n", action, name, status)
}

// skillsDelete removes a skill (operator-authored): it de-materializes it from the
// /skills mount, removes the active dir, and records the cli audit row.
func skillsDelete(ctx context.Context, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aura skills delete <name>")
		os.Exit(1)
	}
	env := bootSkills(ctx)
	defer env.close()
	if _, err := env.w.Delete(ctx, args[0], skills.AuditActor{ActorID: "cli"}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("ok: deleted %q\n", args[0])
}

// skillsAlways re-enables (on) or disables (off) the always:true flag on an ACTIVE
// skill (D-10): the operator-only path that re-enables the always-on flag the
// installer stripped unconditionally.
func skillsAlways(ctx context.Context, args []string) {
	if len(args) < 2 || (args[1] != "on" && args[1] != "off") {
		fmt.Fprintln(os.Stderr, "usage: aura skills always <name> {on|off}")
		os.Exit(1)
	}
	name := args[0]
	always := args[1] == "on"
	env := bootSkills(ctx)
	defer env.close()
	if err := env.w.SetAlways(ctx, name, always, skills.AuditActor{ActorID: "cli"}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("ok: %q always=%v\n", name, always)
}

// skillsAudit lists the append-only audit rows, optionally filtered by --skill and
// --since. A `purge` argument surfaces the role error (aura_app cannot DELETE/
// TRUNCATE the append-only ledger — SC#2): it attempts a DELETE and prints the
// permission-denied error rather than swallowing it.
func skillsAudit(ctx context.Context, args []string) {
	env := bootSkills(ctx)
	defer env.close()

	if len(args) >= 1 && args[0] == "purge" {
		skillsAuditPurge(ctx, env)
		return
	}

	var f skills.AuditFilter
	if v, ok := flagValue(args, "--skill"); ok {
		f.SkillName = v
	}
	if v, ok := flagValue(args, "--since"); ok {
		ts, err := time.Parse(time.RFC3339, v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skills audit: --since must be RFC-3339: %v\n", err)
			os.Exit(1)
		}
		f.Since = ts
	}
	rows, err := env.audit.List(ctx, f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(rows) == 0 {
		fmt.Println("ok: no audit rows")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "CREATED\tSKILL\tACTION\tSOURCE\tGATE_TAKEN\tHASH")
	for _, r := range rows {
		// A NULL source is a pre-#97 row whose approval gate was never exercised; it is
		// rendered as "-" rather than a status word, because "pending" no longer names
		// anything a reader could act on.
		src := string(r.ApprovalSource)
		if src == "" {
			src = "-"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\t%s\n",
			r.CreatedAt.UTC().Format(time.RFC3339), r.SkillName, r.Action, src, r.GateTaken, shortHash(r.ContentHash))
	}
	_ = w.Flush()
}

// skillsAuditPurge attempts to DELETE the audit ledger as aura_app and SURFACES the
// permission-denied role error (SC#2: the append-only ledger refuses the purge).
func skillsAuditPurge(ctx context.Context, env *skillsEnv) {
	_, err := env.pool.Exec(ctx, "DELETE FROM aura.skill_audit")
	if err != nil {
		fmt.Fprintf(os.Stderr, "skills audit purge denied (append-only ledger): %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "skills audit purge: unexpectedly succeeded — the append-only guarantee is broken")
	os.Exit(1)
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// hasFlag reports whether a bare flag (e.g. --always) is present in args.
func hasFlag(args []string, flag string) bool {
	return slices.Contains(args, flag)
}
