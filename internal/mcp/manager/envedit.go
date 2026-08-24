package manager

import (
	"fmt"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/secret"
)

// envedit.go is the in-place MCP env-edit path (D-05 / MCPW-02). It is the WRITE half
// of the secrets-never-leaked belt: the board projects env to key-only chips outbound
// (envChips), and an operator edit submits the redacted ${KEY} placeholder for any value
// it leaves untouched. SetServerEnv reuses the exact D-05 substrate already proven in
// config.go — mergeEnvPreserveCredentials + isPlaceholderValue — so a submitted ${KEY}
// for a stored secret preserves that secret (it is NEVER overwritten with the placeholder
// text), while a real submitted value (or a cleared non-secret) takes effect. The path
// lives in package manager so it can call those unexported helpers directly.

// SetServerEnv replaces server `name`'s Env in doc with the merge of its existing Env and
// the operator-submitted Env (D-05). It rejects an unknown server with a 404-style error
// so the handler can map it to a clean 404. Other servers' Env are untouched; only the
// named entry is re-set with the merged Env.
//
// Four-state merge (the redacted-placeholder-preserved property, MCPW-02): for each
// submitted "KEY=VALUE", a secret KEY whose VALUE is the redacted ${KEY} placeholder
// (isPlaceholderValue) is treated as UNCHANGED — the stored real secret is preserved and
// is NEVER overwritten with the placeholder text. A real submitted secret VALUE overwrites
// (rotation). A non-secret VALUE edits (or clears) in place. A key present only in the
// existing Env (not re-submitted) is retained.
//
// This is stricter than the blanket-preserve mergeEnvPreserveCredentials (which keeps the
// existing secret against ANY incoming override and so could never rotate): SetServerEnv
// only preserves on the placeholder, so the cockpit's four-state edit (untouched vs
// rotated) round-trips. cutEnv / isPlaceholderValue are the shared config.go substrate.
func SetServerEnv(doc *mcp.ManagedConfig, name string, submitted []string) error {
	if doc == nil || doc.MCPServers == nil {
		return fmt.Errorf("%w: %q", ErrServerNotFound, name)
	}
	existing, ok := doc.MCPServers[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrServerNotFound, name)
	}
	next := existing
	next.Env = mergeSubmittedEnv(existing.Env, submitted)
	doc.MCPServers[name] = next
	return nil
}

// mergeSubmittedEnv applies the operator-submitted env over existing, preserving a stored
// secret only when the submitted value is the redacted ${KEY} placeholder (or empty for a
// secret key).
//
// The submitted list is AUTHORITATIVE: a key the operator did not submit is removed. It
// used to be retained, and that made a mistyped variable name permanent — the editor
// renders every stored key, so the operator can only ever submit the full set, and with
// retention no submission could mean "delete this one". The cockpit had no way to undo a
// typo, and neither did the CLI.
func mergeSubmittedEnv(existing, submitted []string) []string {
	existingByKey := map[string]string{}
	for _, entry := range existing {
		if key, value, ok := cutEnv(entry); ok {
			if _, seen := existingByKey[key]; !seen {
				existingByKey[key] = value
			}
		}
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(submitted)+len(existing))
	for _, entry := range submitted {
		key, value, ok := cutEnv(entry)
		if !ok {
			out = append(out, entry)
			continue
		}
		seen[key] = struct{}{}
		// A secret key whose submitted value is the redacted placeholder is unchanged: keep
		// the stored real secret, never the "${KEY}" text. Otherwise the submitted value
		// wins (rotation, non-secret edit, or clearing a non-secret).
		if prior, had := existingByKey[key]; had && secret.IsSecretEnvKey(key) && isPlaceholderValue(key, value) && !isPlaceholderValue(key, prior) {
			out = append(out, key+"="+prior)
			continue
		}
		out = append(out, entry)
	}
	return out
}
