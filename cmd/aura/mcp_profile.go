package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

func mcpProfile(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aura mcp profile {list|create|use|add|remove}")
	}
	switch args[0] {
	case "list":
		return mcpProfileList(args[1:], out)
	case "create":
		return mcpProfileCreate(args[1:], out)
	case "use":
		return mcpProfileUse(args[1:], out)
	case "add":
		return mcpProfileAdd(args[1:], out)
	case "remove":
		return mcpProfileRemove(args[1:], out)
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

func mcpProfileCreate(args []string, out io.Writer) error {
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
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		return err
	}
	return writef(out, "ok: created profile %s\n", name)
}

func mcpProfileUse(args []string, out io.Writer) error {
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
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		return err
	}
	return writef(out, "ok: using profile %s\n", name)
}

func mcpProfileAdd(args []string, out io.Writer) error {
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
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		return err
	}
	return writef(out, "ok: added %s to profile %s\n", server, profile)
}

func mcpProfileRemove(args []string, out io.Writer) error {
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
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		return err
	}
	return writef(out, "ok: removed %s from profile %s\n", server, profile)
}

func mcpTrust(args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: aura mcp trust <name>")
	}
	name := strings.TrimSpace(args[0])
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
	server.Trust = mcp.ManagedTrust{Class: mcp.TrustTrustedLocal}
	doc.MCPServers[name] = server
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		return err
	}
	return writef(out, "ok: trusted %s as %s\n", name, mcp.TrustTrustedLocal)
}

func ensureProfileMembership(doc *mcp.ManagedConfig, profile, server string) {
	if strings.TrimSpace(profile) == "" {
		profile = mcp.DefaultMCPProfile
	}
	if doc.Profiles == nil {
		doc.Profiles = map[string]mcp.ManagedProfile{}
	}
	p := doc.Profiles[profile]
	for _, existing := range p.Servers {
		if existing == server {
			doc.Profiles[profile] = p
			return
		}
	}
	p.Servers = append(p.Servers, server)
	sort.Strings(p.Servers)
	doc.Profiles[profile] = p
}
