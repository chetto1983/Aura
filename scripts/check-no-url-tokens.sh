#!/usr/bin/env bash
# check-no-url-tokens.sh — MUSR-06 static gate: NO long-lived session/auth token is ever
# emitted into a URL or query string across the Go + web source.
#
# Why: a session/bearer token in a URL leaks through Referer headers, browser history,
# proxy/access logs, and shared links. Aura's long-lived auth session travels ONLY as the
# __Host-aura_session / __Host-authula_session Secure cookie — never a query param. This
# gate is the static proof of that invariant (phase-36 D-24 / T-36-11-I).
#
# Carve-out (MUSR-06): a query token is allowed ONLY for a <=1h SETUP BOOTSTRAP — the
# Telegram deep-link `t.me/<bot>?start=<token>` and the first-run setup wizard
# `/setup/?token=<token>` (onboardingTTL = 1h, single-use). Those use the generic
# `start=` / `token=` keys, which are deliberately NOT in the long-lived key set below,
# so they pass without an explicit per-line allowlist.
#
# Usage:
#   bash scripts/check-no-url-tokens.sh              # scan the tracked tree; fail on a finding
#   bash scripts/check-no-url-tokens.sh --self-test  # prove a planted violation is caught
#
# Exit codes:
#   0 — clean (no long-lived token in any URL/query), or self-test passed
#   1 — at least one violation found (or self-test failed to catch a planted one)
#   2 — usage error
set -euo pipefail

# LONG_LIVED_KEYS are the session/auth token query-parameter names that must NEVER appear
# in a URL. The generic bootstrap keys `start` and `token` are intentionally absent — they
# carry the <=1h setup-bootstrap tokens (Telegram deep-link + setup wizard), the documented
# MUSR-06 carve-out.
LONG_LIVED_KEYS='session|aura_session|authula_session|session_token|sessiontoken|session_id|sessionid|jsessionid|access_token|auth_token|refresh_token|id_token|bearer_token'
# QUERY_RE matches a long-lived key in query position: after a `?` or `&`, immediately
# followed by `=`. Case-insensitive so sessionToken / Access_Token are caught too.
QUERY_RE="[?&](${LONG_LIVED_KEYS})="

# COMMENT_RE strips file:line-prefixed comment-only lines from the hit stream so doc prose
# (which may legitimately reference `?session_token=` as an example) never self-invalidates
# the gate. Whole-line // and /* * comments only — never a trailing strip, since `//` lives
# inside every `http://` URL.
COMMENT_RE=':[0-9]+:[[:space:]]*(//|\*|/\*)'

# EXCLUDE_RE drops paths that are exempt from the scan:
#   - generated (sqlc), vendored (third_party/vendor/node_modules), build output (dist, *.d.ts)
#   - redaction / secret-scan modules: these exist to STRIP tokens FROM URLs, so their source
#     and test fixtures legitimately contain `?access_token=`/`?token=` as the mitigation
#     pattern — flagging them would flag the defense itself.
EXCLUDE_RE='^internal/db/sqlc/|^third_party/|^vendor/|(^|/)(node_modules|dist)/|\.d\.ts$|redact|secret_scan'

# list_targets prints the tracked Go + web source files in scope, one per line.
list_targets() {
  git ls-files '*.go' '*.ts' '*.tsx' | grep -vE "$EXCLUDE_RE" || true
}

# scan_hits reads a NUL-safe file list on stdin and prints every offending
# `file:line:content` (comment-only lines removed). It never exits non-zero itself; the
# caller counts the lines.
scan_hits() {
  # `grep || true` — "no match" (exit 1) is the clean/success case under `set -e`.
  tr '\n' '\0' | xargs -0 grep -HniE "$QUERY_RE" 2>/dev/null | grep -vE "$COMMENT_RE" || true
}

run_self_test() {
  local tmp
  tmp=$(mktemp "${TMPDIR:-/tmp}/nourltoken-selftest-XXXXXX.go")
  # A planted violation: a session token concatenated into a URL query string.
  printf '%s\n' 'package x' \
    'func leak(tok string) string {' \
    '	return "https://aura.example/api/data?session_token=" + tok // planted' \
    '}' > "$tmp"
  local hits
  hits=$(printf '%s\n' "$tmp" | scan_hits)
  rm -f "$tmp"
  if [ -z "$hits" ]; then
    echo "check-no-url-tokens: SELF-TEST FAILED — a planted ?session_token= violation was NOT caught." >&2
    return 1
  fi
  echo "check-no-url-tokens: self-test OK — planted ?session_token= violation is caught."
  return 0
}

main() {
  case "${1:-}" in
    --self-test) run_self_test; exit $? ;;
    -h|--help)   sed -n '2,26p' "$0"; exit 0 ;;
    "")          ;;
    *)           echo "usage: $0 [--self-test|--help]" >&2; exit 2 ;;
  esac

  local targets hits
  targets=$(list_targets)
  if [ -z "$targets" ]; then
    echo "check-no-url-tokens: no source files matched; nothing to check."
    exit 0
  fi
  hits=$(printf '%s\n' "$targets" | scan_hits)
  if [ -n "$hits" ]; then
    echo "check-no-url-tokens: FAIL — long-lived session/auth token(s) in a URL/query string:" >&2
    printf '%s\n' "$hits" >&2
    echo "" >&2
    echo "A long-lived token must travel in a Secure cookie or an Authorization header, never a URL." >&2
    echo "(The only allowed query token is a <=1h setup bootstrap: Telegram ?start= / setup ?token=.)" >&2
    exit 1
  fi
  echo "check-no-url-tokens: OK — no long-lived session/auth token in any URL/query string."
}

main "$@"
