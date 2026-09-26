#!/bin/sh
# Aura's entry point for agent-browser inside the per-identity box (prd.md §12, authenticated
# browsing). Measured 2026-09-26 in this box (spikes/agent-browser-auth/FINDINGS.md):
#   - the daemon reads its state key at spawn, and Aura's Exec scrubs secret-named env, so the key
#     arrives as a file Aura writes on every Resolve. Without it the vault would mint
#     ~/.agent-browser/.encryption-key beside its ciphertext, so this entry point refuses instead;
#   - Suspend SIGKILLs the browser after a 2 s grace: a 2 s autosave keeps a fresh login;
#   - state under /root dies with a box recreate, state on the /workspace volume survives it.
key_file=/run/aura/agent-browser.key
if [ ! -r "$key_file" ]; then
    echo "agent-browser: $key_file is missing; Aura writes it when the box starts. Refusing to run without it, because the credential vault would otherwise create its own key next to the data it protects." >&2
    exit 78
fi
AGENT_BROWSER_ENCRYPTION_KEY="$(cat "$key_file")"
HOME=/workspace/.agent-browser-home
AGENT_BROWSER_AUTOSAVE_INTERVAL_MS="${AGENT_BROWSER_AUTOSAVE_INTERVAL_MS:-2000}"
export AGENT_BROWSER_ENCRYPTION_KEY HOME AGENT_BROWSER_AUTOSAVE_INTERVAL_MS
mkdir -p "$HOME"
exec /usr/local/lib/agent-browser/agent-browser "$@"
