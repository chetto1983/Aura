#!/bin/sh
# Aura's entry point for agent-browser inside the per-identity box (prd.md §12, authenticated
# browsing). Measured 2026-09-26 in this box (spikes/agent-browser-auth/FINDINGS.md):
#   - the daemon reads its state key at spawn, and Aura's Exec scrubs secret-named env, so the key
#     arrives as a file Aura writes on every Resolve. Without it the vault would mint
#     ~/.agent-browser/.encryption-key beside its ciphertext, so this entry point refuses instead;
#   - Suspend SIGKILLs the browser after a 2 s grace: a 2 s autosave keeps a fresh login;
#   - state under /root dies with a box recreate, state on the /workspace volume survives it;
#   - Google refuses sign-in to a browser that announces automation (2026-09-27, Dockerfile), so
#     the browser starts with AutomationControlled off and the image's plain Chrome user agent;
#   - a browser idle for an hour, agent-browser's default, holds about 200 of the box's pids and
#     its CPU: two real sites left open kept a box at 398 pids (2026-09-27). It closes after ten
#     minutes instead; the session's profile keeps its login for the next open;
#   - a login lasts only in a persistent Chrome profile, one per session, and agent-browser
#     relaunches on a temporary one whenever a call of the session omits it (2026-09-27). A CLI
#     call therefore gets the directory the MCP bridge gives every call of the same session
#     (internal/agent/mcptools/bridge_browser_profile.go). `agent-browser mcp` is left alone: it
#     runs each call as its own child, past this entry point, and the bridge sets the profile there.
key_file=/run/aura/agent-browser.key
if [ ! -r "$key_file" ]; then
    echo "agent-browser: $key_file is missing. Aura derives it from AURA_AUTHULA_SECRET and writes it when the box starts; without that secret the browser is unavailable. Refusing to run, because the credential vault would otherwise create its own key next to the data it protects." >&2
    exit 78
fi
AGENT_BROWSER_ENCRYPTION_KEY="$(cat "$key_file")"
HOME="${AURA_AGENT_BROWSER_HOME:?set by the image}"
AGENT_BROWSER_AUTOSAVE_INTERVAL_MS="${AGENT_BROWSER_AUTOSAVE_INTERVAL_MS:-2000}"
AGENT_BROWSER_ARGS="${AGENT_BROWSER_ARGS:---disable-blink-features=AutomationControlled}"
AGENT_BROWSER_USER_AGENT="${AGENT_BROWSER_USER_AGENT:-$(cat /usr/local/share/aura/browser-user-agent)}"
AGENT_BROWSER_IDLE_TIMEOUT_MS="${AGENT_BROWSER_IDLE_TIMEOUT_MS:-600000}"
export AGENT_BROWSER_ENCRYPTION_KEY HOME AGENT_BROWSER_AUTOSAVE_INTERVAL_MS AGENT_BROWSER_ARGS \
    AGENT_BROWSER_USER_AGENT AGENT_BROWSER_IDLE_TIMEOUT_MS
if [ "${1:-}" != mcp ] && [ -z "${AGENT_BROWSER_PROFILE:-}" ]; then
    session="${AGENT_BROWSER_SESSION:-default}" given="" prev=""
    for arg in "$@"; do
        [ "$prev" = --session ] && session="$arg"
        case "$arg" in
            --session=*) session="${arg#--session=}" ;;
            --profile | --profile=*) given=1 ;;
        esac
        prev="$arg"
    done
    [ -n "$given" ] || export AGENT_BROWSER_PROFILE="$HOME/profiles/$session"
fi
mkdir -p "$HOME"
exec /usr/local/lib/agent-browser/agent-browser "$@"
