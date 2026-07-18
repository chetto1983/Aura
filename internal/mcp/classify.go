package mcp

import (
	"fmt"
	"strings"
)

// Classify is the single canonical source of truth for an MCP managed server's
// transport type and effective trust class (D-01, MCPH-01). It replaces the
// scattered, independently-drifting decision bodies previously duplicated across
// normalizedServerType/NormalizedTrust (this package), manager/runtime.go, and
// internal/agent/mcptools/mount.go: every caller that needs to know "what
// transport is this" or "what trust governs it" now resolves both, together, in
// one place, so the two can never again silently disagree.
//
// Classify rejects ambiguous or internally-inconsistent configurations instead
// of silently resolving them:
//   - a server with both url and command set and no explicit type is REJECTED
//     (D-02, closes F-027: the old normalizedServerType silently fell through
//     to stdio for this exact shape, so a mis-shaped remote entry could reach a
//     stdio subprocess spawn);
//   - an explicitly-typed server whose declared type is inconsistent with its
//     trust class (e.g. stdio+remote_http, or streamable_http+trusted_local/
//     sandboxed_local) is REJECTED as an inconsistent explicit declaration.
//
// Trust resolution never auto-promotes an unset/unknown remote trust to a
// runnable class (D-03, closes F-013): an HTTP-shaped server with no known
// trust class and no recipe:-prefixed source resolves to TrustBlocked, full
// stop -- explicit trust is required for every runnable remote transport.
func Classify(s ManagedServer) (serverType string, trust string, err error) {
	hasURL := strings.TrimSpace(s.URL) != ""
	hasCmd := strings.TrimSpace(s.Command) != ""
	explicitType := strings.TrimSpace(s.Type)

	switch {
	case explicitType == "" && hasURL && hasCmd:
		// D-02/F-027: mixed, ambiguous, no explicit type to disambiguate it --
		// reject outright, never silently resolve to stdio (or anything else).
		return "", "", fmt.Errorf("mcp classify: server has both url and command with no explicit type")
	case explicitType == "" && hasURL:
		serverType = ServerTypeStreamableHTTP
	case explicitType == "":
		// Covers both "command set, url empty" and "bare" (neither set): both
		// infer stdio, matching the pre-existing non-buggy fallback. Only the
		// mixed case above is ambiguous enough to reject.
		serverType = ServerTypeStdio
	case explicitType == ServerTypeStdio:
		serverType = ServerTypeStdio
	case explicitType == ServerTypeStreamableHTTP:
		serverType = ServerTypeStreamableHTTP
	default:
		return "", "", fmt.Errorf("mcp classify: unknown type %q", s.Type)
	}

	trust = resolveTrust(s)

	if err := checkTypeTrustConsistency(serverType, trust); err != nil {
		return "", "", err
	}

	return serverType, trust, nil
}

// resolveTrust absorbs NormalizedTrust's pre-existing logic MINUS the F-013
// auto-promote-to-TrustRemoteHTTP fallback: an explicit known class always
// wins, then a recipe:-sourced server (Aura's own vetted catalog, a genuinely
// different case from "nothing was ever set") resolves to TrustTrustedRecipe,
// and anything else -- including an empty, blank, whitespace-only, or unknown
// trust class on a remote server -- resolves to TrustBlocked (D-03). There is
// deliberately no streamable_http-shape check here: an unset remote trust must
// block regardless of transport, so transport plays no role in this fallback.
func resolveTrust(s ManagedServer) string {
	if isKnownTrust(s.Trust.Class) {
		return strings.TrimSpace(s.Trust.Class)
	}
	if strings.HasPrefix(strings.TrimSpace(s.Source), "recipe:") {
		return TrustTrustedRecipe
	}
	return TrustBlocked
}

// checkTypeTrustConsistency enforces the explicit type<->trust compatibility
// matrix (38-RESEARCH.md Open Question #1 / Assumption A1): a transport whose
// resolved trust class could never legitimately apply to it is a hard Classify
// error, not a silently-accepted inconsistency. Valid combos: stdio with any of
// {trusted_recipe, trusted_local, sandboxed_local, blocked}; streamable_http
// with any of {trusted_recipe, remote_http, blocked}.
func checkTypeTrustConsistency(serverType, trust string) error {
	switch serverType {
	case ServerTypeStdio:
		if trust == TrustRemoteHTTP {
			return fmt.Errorf("mcp classify: stdio server cannot have trust class %q", trust)
		}
	case ServerTypeStreamableHTTP:
		if trust == TrustTrustedLocal || trust == TrustSandboxedLocal {
			return fmt.Errorf("mcp classify: streamable_http server cannot have trust class %q", trust)
		}
	}
	return nil
}
