#!/usr/bin/env bash
# Checks that Dependabot itself ran (security-scan.yml, spec D5):
#
#   dependabot_fresh.sh [.github/dependabot.yml]
#
# Every entry of the config needs a run of the `Dependabot Updates` workflow in the last eight
# days, and no failed run that a later success on the same entry has not cleared (the operator's
# rule, 2026-10-24). It stopped for 15 days in October 2026 and nothing said so (spec, finding 3). A run's title names its entry, measured on 2026-10-24:
# "<ecosystem> in <dir>[, <dir>...][ for <deps>] - Update #N". A security update has the same
# title, so for an ecosystem that gets them a security update can stand in for a stopped schedule.
#
# The runs come from `gh api` (GH_REPO, GH_TOKEN), or from DEPENDABOT_RUNS_FILE, a JSON array of
# {name, created_at, conclusion, html_url}. DEPENDABOT_NOW fixes the clock.
set -euo pipefail

config=${1:-.github/dependabot.yml}
now=$(date -u -d "${DEPENDABOT_NOW:-now}" +%s)
since=$(date -u -d "@$((now - 8 * 86400))" +%Y-%m-%dT%H:%M:%SZ)

if [[ -n ${DEPENDABOT_RUNS_FILE:-} ]]; then
  runs="$(cat "$DEPENDABOT_RUNS_FILE")"
else
  workflow="$(gh api "repos/$GH_REPO/actions/workflows" --paginate \
    --jq '.workflows[] | select(.path == "dynamic/dependabot/dependabot-updates") | .id')"
  if [[ -z $workflow ]]; then
    echo "dependabot: no Dependabot Updates workflow in $GH_REPO" >&2
    exit 1
  fi
  runs="$(gh api "repos/$GH_REPO/actions/workflows/$workflow/runs?created=%3E%3D$since&per_page=100" \
    --paginate --jq '.workflow_runs[] | {name, created_at, conclusion, html_url}' | jq -s .)"
fi

# One line per entry: the ecosystem and its first directory, as the config spells them.
entries="$(awk '
  /^  - package-ecosystem:/ { eco = $3; want = 1; next }
  want && /^    directory:/ { gsub(/"/, "", $2); print eco "\t" $2; want = 0; next }
  want && /^      - / { gsub(/"/, "", $2); print eco "\t" $2; want = 0 }
' "$config")"
if [[ -z $entries ]]; then
  echo "dependabot: no entries read from $config" >&2
  exit 1
fi

status=0
echo "| ecosystem | directory | runs in 8 days | last run | failed | verdict |"
echo "|---|---|---|---|---|---|"
while IFS=$'\t' read -r eco dir; do
  case $eco in
    gomod) title=go_modules ;;
    npm) title=npm_and_yarn ;;
    github-actions) title=github_actions ;;
    docker-compose) title=docker_compose ;;
    *) title=$eco ;;
  esac
  [[ $dir == / ]] && shown=/. || shown=$dir
  row="$(jq -r --arg eco "$title" --arg dir "$shown" --arg since "$since" '
    [.[] | select(.created_at >= $since)
         | select(.name | startswith($eco + " in "))
         | select(.name | sub("^[^ ]+ in "; "") | sub(" (for .*)?- Update #[0-9]+$"; "")
                        | split(", ") | index($dir) != null)]
    | (map(select(.conclusion == "success") | .created_at) | max // "") as $cleared
    | [length, (map(.created_at) | max // "-"),
       (map(select(.conclusion == "failure" or .conclusion == "timed_out"
                   or .conclusion == "startup_failure")
            | select(.created_at > $cleared)) | length)]
    | @tsv' <<<"$runs")"
  IFS=$'\t' read -r count last failed <<<"$row"
  verdict=ok
  if (( count == 0 || failed > 0 )); then verdict=FAIL; status=1; fi
  echo "| $eco | $dir | $count | $last | $failed | $verdict |"
done <<<"$entries"
exit "$status"
