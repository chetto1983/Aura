package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/packs"
	"github.com/chetto1983/aura/internal/skills"
)

// packInstall installs a whole pack: its skills through the Installer the model
// and the cockpit already use, its connectors through the same managed-config
// write `aura mcp add` performs.
//
// The connectors land in ONE write, not one per server. A pack is a single
// decision by the operator, and fourteen separate writes would leave a partially
// installed pack behind on the first failure and fourteen rows in the audit for
// one intent.
//
// Every connector arrives TrustBlocked. That is not caution for its own sake: it
// is what makes the single `aura pack trust` the operator runs next meaningful,
// and a pack cannot grant itself the trust it wants.
func packInstall(ctx context.Context, pool *pgxpool.Pool, refArg string, out io.Writer) error {
	ref, err := packs.ParseRef(refArg)
	if err != nil {
		return err
	}
	if ref.Directory == "" {
		return fmt.Errorf("install needs a plugin: %s/<plugin> (use `aura pack list %s` to see them)", ref.Source, ref.Source)
	}
	found, err := packResolve(ctx, ref)
	if err != nil {
		return err
	}
	pack := found[0]

	doc, path, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	if doc.MCPServers == nil {
		doc.MCPServers = map[string]mcp.ManagedServer{}
	}

	installable, skipped := packs.InstallableServers(pack)
	pack.Servers = installable

	installer := packs.Installer{
		InstallSkill: packSkillInstaller(pool),
		AddServer: func(_ context.Context, name string, s packs.Server) error {
			return stagePackServer(&doc, ref, name, s)
		},
	}
	report, err := installer.Install(ctx, pack)
	if err != nil {
		return err
	}
	if len(report.Servers) > 0 {
		if err := mcpWriteManagedConfig(ctx, pool, path, doc, "pack-install", pack.Name, packs.SourceMarker(ref)); err != nil {
			return fmt.Errorf("write managed config: %w", err)
		}
	}

	packs.WriteReport(out, report)
	for _, s := range skipped {
		// Named, never silent: the operator asked for the pack, and a connector
		// that did not arrive has to be accounted for even when leaving it out
		// was the right call.
		_, _ = fmt.Fprintf(out, "  skipped %s — no endpoint declared (placeholder)\n", s.Name)
	}
	_, _ = fmt.Fprintf(out, "\nEvery connector is BLOCKED until you approve it:\n  aura pack trust %s --class <class> --reason \"...\"\n", ref.String())
	return nil
}

// stagePackServer adds one connector to the in-memory config. It refuses a name
// that is already taken rather than overwriting it: the existing server may be
// one the operator configured by hand, and a pack must not silently replace it.
func stagePackServer(doc *mcp.ManagedConfig, ref packs.Ref, name string, s packs.Server) error {
	if existing, taken := doc.MCPServers[name]; taken {
		if existing.Source == packs.SourceMarker(ref) {
			return fmt.Errorf("already installed from this pack")
		}
		return fmt.Errorf("name is taken by a server from %q", existing.Source)
	}
	enabled := true
	doc.MCPServers[name] = mcp.ManagedServer{
		Command: s.Command,
		Args:    s.Args,
		Env:     packEnvPairs(s.Env),
		URL:     s.URL,
		Type:    s.Type,
		Enabled: &enabled,
		Source:  packs.SourceMarker(ref),
		// A pack cannot grant itself trust; the operator does, once, for the
		// whole pack (amendment #126).
		Trust: mcp.ManagedTrust{Class: mcp.TrustBlocked},
	}
	ensureProfileMembership(doc, doc.ActiveProfileName(), name)
	return nil
}

// packEnvPairs converts the pack format's env object into the KEY=VALUE slice
// Aura's ManagedServer carries.
func packEnvPairs(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	pairs := make([]string, 0, len(env))
	for k, v := range env {
		pairs = append(pairs, k+"="+v)
	}
	// A map has no order; without this the same pack writes a differently
	// ordered config on every install and the file churns for no reason.
	sort.Strings(pairs)
	return pairs
}

// packSkillInstaller returns the skill half, or nil when there is no writer pool
// — the CLI then installs the connectors alone and says so, rather than failing
// the whole pack over a database the operator may not have running.
func packSkillInstaller(pool *pgxpool.Pool) func(context.Context, string) error {
	if pool == nil {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	installer := skills.NewInstaller(skills.InstallerConfig{
		Writer:       newSkillWriter(cfg, pool),
		Blocklist:    cfg.SkillInjectionBlocklist,
		BodyCapBytes: cfg.SkillBodyCapBytes,
		WorkDir:      cfg.RunDir,
	})
	actor := skills.AuditActor{ActorID: mcpAuditActor(), IdentityID: mcpAuditActor()}
	return func(ctx context.Context, source string) error {
		_, err := installer.Install(ctx, source, actor)
		return err
	}
}

// packTrust is the single approval a pack takes. It sweeps every server the pack
// installed — matched on the Source marker, which is why that marker exists —
// and writes them in one go.
func packTrust(ctx context.Context, pool *pgxpool.Pool, refArg, class, reason string, out io.Writer) error {
	ref, err := packs.ParseRef(refArg)
	if err != nil {
		return err
	}
	class, reason, err = validateTrustClassReason(class, reason)
	if err != nil {
		return err
	}
	doc, path, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	marker := packs.SourceMarker(ref)
	names := make([]string, 0, len(doc.MCPServers))
	for name, server := range doc.MCPServers {
		if server.Source == marker {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("no connector installed from %s (source marker %q)", ref.String(), marker)
	}
	sort.Strings(names)

	approval := packTrustApproval(class, reason)
	for _, name := range names {
		server := doc.MCPServers[name]
		server.Trust = approval
		doc.MCPServers[name] = server
	}
	if err := mcpWriteManagedConfig(ctx, pool, path, doc, "pack-trust", ref.String(), reason); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "ok: trusted %d connector(s) from %s as %s\n", len(names), ref.String(), class)
	for _, name := range names {
		_, _ = fmt.Fprintf(out, "  %s\n", name)
	}
	return nil
}

func packTrustApproval(class, reason string) mcp.ManagedTrust {
	return mcp.ManagedTrust{
		Class:      class,
		ApprovedBy: mcpAuditActor(),
		ApprovedAt: time.Now().UTC().Format(time.RFC3339),
		Reason:     reason,
	}
}
