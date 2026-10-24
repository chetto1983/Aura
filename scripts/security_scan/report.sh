#!/usr/bin/env bash
# Opens one issue for a red scheduled scan, or comments on the one already open (spec D3). GitHub
# notifies only the user who last edited a scheduled workflow's cron line about its failures, and
# commits here are not the operator's. Every watcher sees an issue.
#
# GH_REPO, RUN_ID and GH_TOKEN come from the workflow.
set -euo pipefail

: "${GH_REPO:?}" "${RUN_ID:?}"
label=security-scan
run_url="${GITHUB_SERVER_URL:-https://github.com}/$GH_REPO/actions/runs/$RUN_ID"

failed="$(gh api "repos/$GH_REPO/actions/runs/$RUN_ID/jobs" --paginate \
  --jq '.jobs[] | select(.conclusion == "failure") | "| \(.name) | \(.html_url) |"')"
body="$(printf 'The weekly security scan of %s is red: %s\n\n| failed job | log |\n|---|---|\n%s\n\nEach job summary lists its findings. Fix the finding, or accept it with a dated reason in .trivyignore.yaml or .github/osv-scanner-craft.toml.\n' \
  "$(date -u +%Y-%m-%d)" "$run_url" "$failed")"

gh label create "$label" --repo "$GH_REPO" --force --color B60205 \
  --description "Findings of the weekly security scan (security-scan.yml)"
issue="$(gh issue list --repo "$GH_REPO" --label "$label" --state open --limit 1 \
  --json number --jq '.[0].number // empty')"
if [[ -n $issue ]]; then
  gh issue comment "$issue" --repo "$GH_REPO" --body "$body"
else
  gh issue create --repo "$GH_REPO" --label "$label" --title "Weekly security scan: findings to act on" --body "$body"
fi
