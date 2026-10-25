package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/mcp/mcpenv"
	"github.com/chetto1983/aura/internal/packs"
	"github.com/chetto1983/aura/internal/plugins"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/jackc/pgx/v5/pgxpool"
)

// serve_plugins.go wires the plugin backend (internal/plugins) into the daemon over the same
// machinery the MCP and skills boards use: the registry through the one registry lock, the
// deployment's skill library, the MCP verification and live mounts, and the caller's box.

// newPluginResumeHook records an accept of a relayed plugin_pack challenge into the tool's own
// store, and only from an identity that may administer plugins. It reads the tool's backend
// when it runs: serve boot binds it after the hook chain is built.
func newPluginResumeHook(pack *tools.PackTool) runner.ResumeHook {
	if pack == nil || pack.Approvals == nil {
		return nil
	}
	return newChallengeResumeHook(pack.Approvals.ApproveChallenge, pack.Approvals.DiscardChallenge,
		tools.PluginApprovalContext, "approval_sha256", func(ctx context.Context) bool {
			if pack.Backend == nil {
				return false
			}
			ok, err := pack.Backend.MayAdminister(ctx, identityctx.IdentityID(ctx))
			if err != nil {
				slog.Warn("plugin approval: check the approver", "err", err)
			}
			return err == nil && ok
		})
}

// buildPluginBackend returns the daemon's plugin backend, or nil without a database, a plugins
// directory or a skills directory: the plugin routes then answer 503.
func buildPluginBackend(cfg *config.Config, pool *pgxpool.Pool, live *liveMCPMount, prep *mcpenv.Preparer,
	router *usersandbox.SandboxRouter, invalidate func()) *plugins.Backend {
	if cfg == nil || pool == nil || cfg.PluginsDir == "" || cfg.SkillsDir == "" {
		return nil
	}
	writer := newSkillWriter(cfg, pool)
	library := pluginSkills{
		writer: writer,
		installer: skills.NewInstaller(skills.InstallerConfig{
			Writer: writer, Blocklist: cfg.SkillInjectionBlocklist, BodyCapBytes: cfg.SkillBodyCapBytes, WorkDir: cfg.RunDir,
		}),
		invalidate: invalidate,
	}
	capabilities := identity.New(pool)
	return plugins.New(plugins.Deps{
		Store:    plugins.Store{Root: cfg.PluginsDir},
		Checkout: (&packs.Resolver{WorkDir: cfg.RunDir}).Checkout,
		Head: func(ctx context.Context, ref packs.Ref) (string, error) {
			return packs.GitFetcher{}.Head(ctx, ref.Source)
		},
		Skills:   library,
		Registry: pluginRegistry{pool: pool},
		Verify: func(ctx context.Context, name string, s mcp.ManagedServer) (mcp.ManagedServer, error) {
			server, _, _, err := mcpInstallGuard(ctx, prep, name, s, live.box())
			return server, err
		},
		Mount: live.MountNow,
		Unmount: func(name string) {
			live.Unmount(name)
			if err := prep.Remove(name); err != nil {
				slog.Warn("plugin removal: the connector's prepared environment stayed", "server", name, "err", err)
			}
		},
		Probe: func(ctx context.Context, name string, s mcp.ManagedServer) mcp.ProbeResult {
			pctx, cancel := context.WithTimeout(ctx, mcpProbeTimeout)
			defer cancel()
			return probeManagedMCPServer(pctx, name, s, live.box())
		},
		PurgeData: func(ctx context.Context, dataDir string) error { return purgeBoxData(ctx, router, dataDir) },
		Admins: func(ctx context.Context, id string) (bool, error) {
			return capabilities.HasCapability(ctx, id, identity.CapIdentityCreate)
		},
		Taken:        pluginTaken(library),
		Blocklist:    cfg.SkillInjectionBlocklist,
		BodyCapBytes: cfg.SkillBodyCapBytes,
	})
}

// pluginRegistry is the plugin backend's registry: its writes take turns with the MCP board's.
type pluginRegistry struct {
	pool *pgxpool.Pool
}

func (r pluginRegistry) Update(ctx context.Context, actor, action, subject, reason string, fn func(*mcp.ManagedConfig) error) error {
	return updateManagedMCPConfig(ctx, r.pool, mcpmanager.MCPAuditInsert{
		ActorIdentityID: actor, Action: action, ServerName: subject, Reason: reason,
	}, fn)
}

func (pluginRegistry) Load() (mcp.ManagedConfig, error) { return loadManagedMCPConfig() }

// pluginSkills is the deployment's skill library, where a plugin's skills land: the registry
// and /plugins are the deployment's too.
type pluginSkills struct {
	writer     *skills.Writer
	installer  *skills.Installer
	invalidate func()
}

func (s pluginSkills) Exists(name string) bool {
	return s.writer.ActiveExists(name) || s.writer.ArchivedExists(name)
}

func (s pluginSkills) InstallDir(ctx context.Context, dir, source string, actor skills.AuditActor) (string, error) {
	info, err := s.installer.InstallDir(ctx, dir, source, actor)
	if err != nil {
		return "", err
	}
	s.changed()
	return info.ContentHash, nil
}

// ActiveHash hashes the active copy the way the install hashed what it wrote, so an edit since
// the install shows as a different hash.
func (s pluginSkills) ActiveHash(name string) (string, bool) {
	if !s.writer.ActiveExists(name) {
		return "", false
	}
	hash, err := skills.HashSkillDir(filepath.Join(s.writer.ActiveDir(), name))
	if err != nil {
		slog.Warn("plugin removal: the active skill could not be hashed, so it is left in place", "skill", name, "err", err)
		return "", false
	}
	return hash, true
}

func (s pluginSkills) Delete(ctx context.Context, name string, actor skills.AuditActor) error {
	if _, err := s.writer.Delete(ctx, name, actor); err != nil {
		return err
	}
	s.changed()
	return nil
}

func (s pluginSkills) changed() {
	if s.invalidate != nil {
		s.invalidate()
	}
}

// pluginTaken reads the names in use: skills active or archived in the library, registry rows,
// and catalog recipes, which a default-on server holds without a row.
func pluginTaken(library pluginSkills) func() (plugins.Taken, error) {
	return func() (plugins.Taken, error) {
		doc, err := loadManagedMCPConfig()
		if err != nil {
			return plugins.Taken{}, fmt.Errorf("read the MCP registry: %w", err)
		}
		return plugins.Taken{
			Skill: library.Exists,
			Server: func(name string) (string, bool) {
				if s, ok := doc.MCPServers[name]; ok {
					return s.Source, true
				}
				if _, ok := mcpmanager.LookupCatalog(name); ok {
					return "recipe:" + name, true
				}
				return "", false
			},
		}, nil
	}
}

// purgeBoxData deletes a plugin's data in the box of the identity the context carries. Every
// identity's box holds its own copy; a removal reaches the caller's.
func purgeBoxData(ctx context.Context, router *usersandbox.SandboxRouter, dataDir string) error {
	if router == nil {
		return errors.New("no sandbox, so the plugin's data in the boxes cannot be reached")
	}
	h, err := router.Route(ctx)
	if err != nil {
		return err
	}
	res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Command: "rm -rf -- " + tools.ShellQuoteArg(dataDir)})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("rm %s exited %d: %s", dataDir, res.ExitCode, res.Stderr)
	}
	return nil
}

// pluginTreeResolver is what the box mirror carries: the tree of every plugin installing,
// installed or upgrading. An upgrading record names the new commit, so its new tree reaches the
// boxes before its connectors are verified there. A removing plugin's tree leaves the boxes at
// the next mirror.
func pluginTreeResolver(store plugins.Store) usersandbox.PluginTreeResolver {
	return func(context.Context) ([]usersandbox.PluginTree, error) {
		records, err := store.List()
		if err != nil {
			return nil, err
		}
		var trees []usersandbox.PluginTree
		for _, rec := range records {
			if rec.Phase != plugins.PhaseRemoving {
				trees = append(trees, usersandbox.PluginTree{Name: rec.Name, HostDir: store.TreeDir(rec.Name), Version: rec.Commit})
			}
		}
		return trees, nil
	}
}
