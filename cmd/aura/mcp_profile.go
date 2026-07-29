package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/jackc/pgx/v5/pgxpool"
)

func mcpProfile(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aura mcp profile {list|create|use|add|remove}")
	}
	switch args[0] {
	case "list":
		return mcpProfileList(args[1:], out)
	case "create":
		return mcpProfileCreate(ctx, pool, args[1:], out)
	case "use":
		return mcpProfileUse(ctx, pool, args[1:], out)
	case "add":
		return mcpProfileAdd(ctx, pool, args[1:], out)
	case "remove":
		return mcpProfileRemove(ctx, pool, args[1:], out)
	default:
		return fmt.Errorf("unknown mcp profile subcommand %q", args[0])
	}
}

func mcpProfileList(args []string, out io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: aura mcp profile list")
	}
	doc, _, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(doc.Profiles))
	for name := range doc.Profiles {
		names = append(names, name)
	}
	if len(names) == 0 {
		names = append(names, doc.ActiveProfileName())
	}
	sort.Strings(names)
	for _, name := range names {
		marker := " "
		if name == doc.ActiveProfileName() {
			marker = "*"
		}
		if err := writef(out, "%s %s\t%d servers\n", marker, name, len(doc.ProfileServerNames(name))); err != nil {
			return err
		}
	}
	return nil
}

func mcpProfileCreate(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: aura mcp profile create <name>")
	}
	name := strings.TrimSpace(args[0])
	if name == "" {
		return fmt.Errorf("MCP profile name cannot be empty")
	}
	doc, path, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	if doc.Profiles == nil {
		doc.Profiles = map[string]mcp.ManagedProfile{}
	}
	if _, exists := doc.Profiles[name]; exists {
		return fmt.Errorf("MCP profile %q already exists", name)
	}
	doc.Profiles[name] = mcp.ManagedProfile{}
	if err := mcpWriteManagedConfig(ctx, pool, path, doc, "profile_create", name, ""); err != nil {
		return err
	}
	return writef(out, "ok: created profile %s\n", name)
}

func mcpProfileUse(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: aura mcp profile use <name>")
	}
	name := strings.TrimSpace(args[0])
	doc, path, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	if doc.Profiles == nil {
		doc.Profiles = map[string]mcp.ManagedProfile{}
	}
	if _, ok := doc.Profiles[name]; !ok {
		return fmt.Errorf("MCP profile %q not found", name)
	}
	doc.ActiveProfile = name
	if err := mcpWriteManagedConfig(ctx, pool, path, doc, "profile_use", name, ""); err != nil {
		return err
	}
	return writef(out, "ok: using profile %s\n", name)
}

func mcpProfileAdd(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: aura mcp profile add <profile> <server>")
	}
	profile, server := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
	doc, path, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	if _, ok := doc.MCPServers[server]; !ok {
		return fmt.Errorf("MCP server %q not found", server)
	}
	ensureProfileMembership(&doc, profile, server)
	if err := mcpWriteManagedConfig(ctx, pool, path, doc, "profile_add", server, ""); err != nil {
		return err
	}
	return writef(out, "ok: added %s to profile %s\n", server, profile)
}

func mcpProfileRemove(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: aura mcp profile remove <profile> <server>")
	}
	profile, server := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
	doc, path, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	p, ok := doc.Profiles[profile]
	if !ok {
		return fmt.Errorf("MCP profile %q not found", profile)
	}
	next := p.Servers[:0]
	for _, name := range p.Servers {
		if name != server {
			next = append(next, name)
		}
	}
	p.Servers = append([]string(nil), next...)
	doc.Profiles[profile] = p
	if err := mcpWriteManagedConfig(ctx, pool, path, doc, "profile_remove", server, ""); err != nil {
		return err
	}
	return writef(out, "ok: removed %s from profile %s\n", server, profile)
}

const mcpTrustUsage = "usage: aura mcp trust <name> --reason <text> [--class <class>]"

// parseMCPTrustArgs hand-parses `aura mcp trust <name> --reason <text> [--class <class>]`
// (Pitfall #12): --reason is REQUIRED — a trust with no reason is rejected before any
// read/write; --class is optional, defaulting to trusted_local for parity with the
// pre-existing CLI behavior. The full class/reason validation (known class, non-blank
// reason) is deferred to the shared validateTrustClassReason single source of truth.
func parseMCPTrustArgs(args []string) (name, class, reason string, err error) {
	if len(args) == 0 {
		return "", "", "", errors.New(mcpTrustUsage)
	}
	name = strings.TrimSpace(args[0])
	class = mcp.TrustTrustedLocal
	pendingReason := false
	pendingClass := false
	for _, arg := range args[1:] {
		if pendingReason {
			reason = arg
			pendingReason = false
			continue
		}
		if pendingClass {
			class = arg
			pendingClass = false
			continue
		}
		switch arg {
		case "--reason":
			pendingReason = true
		case "--class":
			pendingClass = true
		default:
			return "", "", "", fmt.Errorf("unknown mcp trust option %q\n%s", arg, mcpTrustUsage)
		}
	}
	if pendingReason {
		return "", "", "", fmt.Errorf("--reason requires a value\n%s", mcpTrustUsage)
	}
	if pendingClass {
		return "", "", "", fmt.Errorf("--class requires a value\n%s", mcpTrustUsage)
	}
	if strings.TrimSpace(reason) == "" {
		return "", "", "", fmt.Errorf("aura mcp trust requires --reason\n%s", mcpTrustUsage)
	}
	return name, class, reason, nil
}

// mcpTrust implements `aura mcp trust <name> --reason <text> [--class <class>]` (D-12/
// D-13, Pitfall #12): the CLI trust-approve gains the same required-reason + known-class
// validation the web endpoint already enforces (validateTrustClassReason, shared with
// TrustApprove), so a CLI trust can never write a NULL-reason audit row or reintroduce
// the F-038 empty-class default.
func mcpTrust(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) error {
	name, class, reason, err := parseMCPTrustArgs(args)
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("MCP server name cannot be empty")
	}
	class, reason, err = validateTrustClassReason(class, reason)
	if err != nil {
		return err
	}
	doc, path, err := loadManagedMCPConfig()
	if err != nil {
		return err
	}
	server, ok := doc.MCPServers[name]
	if !ok {
		return fmt.Errorf("MCP server %q not found in managed config", name)
	}
	if err := writef(out, "server: %s\ncommand: %s\nsource: %s\nruntime: %s\n", name, renderMCPCommand(mcp.ServerConfig{Command: server.Command, Args: server.Args}), server.Source, server.Runtime.Kind); err != nil {
		return err
	}
	server.Trust = mcp.ManagedTrust{
		Class:      class,
		ApprovedBy: mcpAuditActor(),
		ApprovedAt: time.Now().UTC().Format(time.RFC3339),
		Reason:     reason,
	}
	doc.MCPServers[name] = server
	if err := mcpWriteManagedConfig(ctx, pool, path, doc, "trust", name, reason); err != nil {
		return err
	}
	return writef(out, "ok: trusted %s as %s\n", name, class)
}

func ensureProfileMembership(doc *mcp.ManagedConfig, profile, server string) {
	if strings.TrimSpace(profile) == "" {
		profile = mcp.DefaultMCPProfile
	}
	if doc.Profiles == nil {
		doc.Profiles = map[string]mcp.ManagedProfile{}
	}
	p := doc.Profiles[profile]
	if slices.Contains(p.Servers, server) {
		doc.Profiles[profile] = p
		return
	}
	p.Servers = append(p.Servers, server)
	sort.Strings(p.Servers)
	doc.Profiles[profile] = p
}
