# TokenJuice Rules Catalog — for Aura Go port

**Snapshot date:** 2026-05-18
**Upstream:** `vincentkoc/tokenjuice` (MIT, © 2026 Vincent Koc) — TypeScript original.
**Studied port:** `openhuman/src/openhuman/tokenjuice/` (Rust, GPLv3 host project).
**Local source root:** `D:/tmp/openhuman/src/openhuman/tokenjuice/vendor/rules/`
**Built-in rule count:** 96 (matches `BUILTIN_RULE_JSONS` in `rules/builtin.rs`).

> This is a study document for the Aura Go port. We are NOT vendoring openhuman's
> Rust code (GPLv3 incompatible with our needs); we may re-vendor upstream's MIT
> rule JSONs directly. See §5 (license).

---

## 1. JSON rule format (formal schema)

The schema is the upstream TypeScript `JsonRule` shape, ported 1:1 in
`types.rs::JsonRule`. Field names are camelCase. Every field except `id`,
`family`, and `match` is optional (`#[serde(default)]`).

### 1.1 Top-level fields

| Field | Type | Required | Default | Meaning |
|-------|------|----------|---------|---------|
| `id` | string | **yes** | — | Stable rule identifier, e.g. `"git/status"`. Used by `matchedReducer`, forced classification, and three-layer overlay merge. Convention: `<family-prefix>/<short-name>`. |
| `family` | string | **yes** | — | Coarse category used by the post-classification formatter to decide whether to surface facts headers, e.g. `"git-status"`, `"test-results"`, `"search"`, `"help"`, `"generic"`. See `reduce.rs::format_inline` for the special-cased families. |
| `description` | string | no | `null` | Human-readable doc — ignored at runtime, useful when rendering rule pickers. |
| `priority` | int | no | `0` | Multiplied by 1000 in `score_rule()` so it dominates structural specificity. Only `generic/help` uses it (`priority: 25`) — all other vendored rules omit it. |
| `onEmpty` | string | no | `null` | Canned message returned when filtering eliminates every line. Example: `install/npm-install` → `"npm install: ok"`. |
| `matchOutput` | array&lt;`RuleOutputMatch`&gt; | no | `[]` | Regex-on-full-output short-circuits. First match wins and replaces the entire summary. See §1.4. |
| `counterSource` | enum `"postKeep"`\|`"preKeep"` | no | `"postKeep"` | Sample stream for counters. `preKeep` counts on the pre-`keepPatterns` snapshot so that "5 failed tests" survives even when keep-filtering throws the FAILED lines away. Only `tests/pytest` uses `preKeep`. |
| `match` | `RuleMatch` | **yes** | `{}` (matches everything) | See §1.2. `generic/fallback` has `"match": {}` so it always classifies last. |
| `filters` | `RuleFilters` | no | `null` | `skipPatterns` + `keepPatterns`. See §1.3. |
| `transforms` | `RuleTransforms` | no | `null` | Output transformation flags. See §1.5. |
| `summarize` | `RuleSummarize` | no | `{ head: 6, tail: 6 }` | Head/tail line counts for the `head_tail()` window (gap rendered as `"... N lines omitted ..."`). |
| `counters` | array&lt;`RuleCounter`&gt; | no | `[]` | Named line-count facts surfaced in the inline-text header. See §1.6. |
| `failure` | `RuleFailure` | no | `{ preserveOnFailure: false, head: 6, tail: 12 }` | Failure-mode overrides applied iff `input.exitCode != 0`. See §1.7. |

### 1.2 `match` sub-object (`RuleMatch`)

Every dimension is ANDed. Within a dimension, group semantics differ.

| Field | Type | Semantics |
|-------|------|-----------|
| `toolNames` | `string[]` | `input.toolName` ∈ list. Vendored rules use `["exec"]` almost universally; `git/*` rules omit this so they match any tool that invokes git (caller-friendly). |
| `argv0` | `string[]` | `argv[0]` ∈ list. Used to bind a rule to a specific binary. Examples: `["git"]`, `["docker"]`, `["apt", "apt-get"]`, `["fly", "flyctl"]`, `["playwright", "pnpm", "npx", "bunx", "yarn", "npm"]`. |
| `argvIncludes` | `string[][]` | Two-level "all-of-groups, all-of-strings". Each inner group must have ALL its tokens present in `argv`. Example: `[["status"]]` = argv contains `"status"`; `[["diff"], ["--stat"]]` = argv contains BOTH `"diff"` AND `"--stat"`. |
| `argvIncludesAny` | `string[][]` | Same shape but at least ONE inner group must fully match. Example (`generic/help`): `[["--help"], ["help"]]` matches argv with `--help` OR a `help` subcommand. |
| `commandIncludes` | `string[]` | All substrings must appear in the joined command. Example (`build/vite`): `["vite", "build"]`. Used when classification depends on a shell pipeline rather than argv[0]. |
| `commandIncludesAny` | `string[]` | At least one substring present. Example (`generic/help`): `[" --help", " help"]`. |

**Empty `match: {}`** → matches every input (used by `generic/fallback`).

**Scoring (`classify.rs::score_rule`):** the winner is the rule with the highest specificity sum. Weights are:

```text
priority         × 1000
argv0            × 100   (count of entries)
argvIncludes     × 40    (sum of group sizes)
argvIncludesAny  × 35    (sum of group sizes)
commandIncludes  × 25
commandIncludesAny × 20
toolNames        × 10
```

Tiebreak: alphabetical rule id. `generic/fallback` ends up last because it has all-zero counts.

### 1.3 `filters` sub-object (`RuleFilters`)

| Field | Type | Semantics |
|-------|------|-----------|
| `skipPatterns` | `string[]` (regex) | Lines matching ANY pattern are dropped. Applied BEFORE `keepPatterns`. Example: `build/tsc` strips ~30 verbose `--diagnostics` lines. |
| `keepPatterns` | `string[]` (regex) | Whitelist mode. If non-empty AND any line matches, ONLY matching lines survive. Falls back to "keep everything" if zero lines match (so a totally clean output isn't blanked). Example: `search/rg` keeps `^.+:\d+[: -].+`. |

Order in `apply_rule`: `skipPatterns` → snapshot for `preKeep` counters → `keepPatterns`.

### 1.4 `matchOutput` array (`RuleOutputMatch`)

A short-circuit applied to the trimmed full text BEFORE line filtering. First hit wins; the rule's normal summarize/counter pipeline is bypassed entirely.

```json
"matchOutput": [
  {
    "pattern": "up to date, audited \\d+ package",
    "message": "npm install: up to date",
    "flags": "i"
  }
]
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `pattern` | string (regex) | yes | Matched against the trimmed-edges full output. |
| `message` | string | yes | Replacement text used as the entire summary. |
| `flags` | string | no | Regex flags. `"i"` for case-insensitive; `u` is always implied by the Rust regex engine. |

Used by all four `install/*` rules to collapse "no work needed" runs into a one-liner.

### 1.5 `transforms` sub-object (`RuleTransforms`)

| Field | Type | Default | Effect |
|-------|------|---------|--------|
| `stripAnsi` | bool | `false` | Strip ANSI CSI escape sequences before line splitting. Every vendored rule sets this `true`. |
| `trimEmptyEdges` | bool | `false` | Drop leading/trailing blank lines. Every vendored rule sets this `true`. |
| `dedupeAdjacent` | bool | `false` | Collapse adjacent identical lines to one. Every vendored rule sets this `true`. |
| `prettyPrintJson` | bool | `false` | If the raw output parses as JSON, re-emit as `serde_json::to_string_pretty`. No vendored rule uses this — present in the schema for user/project rule layers. |

### 1.6 `counters` array (`RuleCounter`)

Each counter produces one named integer fact, surfaced in the inline-text header for `search`, `test-results` (failure runs), or whenever the summary text contains `"omitted"`. Counter facts are formatted via `pluralize(count, name)`.

```json
{
  "name": "failed test",
  "pattern": "FAILED",
  "flags": "m"
}
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `name` | string | yes | Used in the `pluralize` output and as the `facts` map key. |
| `pattern` | string (regex) | yes | Tested against each post-filter line (or pre-keep line if `counterSource: preKeep`). |
| `flags` | string | no | `"i"` case-insensitive, `"m"` multiline anchor mode. `u` is always set. |

Output formatting: only counters with `count > 0` are included; the parts are alphabetically sorted before being joined with `", "`. See `reduce.rs::format_inline`.

### 1.7 `failure` sub-object (`RuleFailure`)

Applied iff `input.exitCode != 0` AND `preserveOnFailure: true`.

| Field | Type | Default | Effect |
|-------|------|---------|--------|
| `preserveOnFailure` | bool | `false` | Switches the head/tail window to the failure overrides. |
| `head` | int | `6` | Replacement `head` count for failure runs. |
| `tail` | int | `12` | Replacement `tail` count for failure runs. |

Every vendored rule sets `preserveOnFailure: true` and roughly doubles its head/tail (e.g. `git/status` 10+4 → 12+12; `build/webpack` 4+6 → 4+8).

### 1.8 Pipeline order (from `reduce.rs::apply_rule`)

1. `prettyPrintJson` (if set, on raw text)
2. `normalize_lines` (split, trim CR)
3. `stripAnsi` (if set)
4. `matchOutput` check on trimmed-edges joined text — early return
5. `skipPatterns` removal
6. Snapshot lines for `preKeep` counters
7. `keepPatterns` whitelist (with fall-back when zero hits)
8. `trimEmptyEdges`
9. `dedupeAdjacent`
10. **Special post-processors** — hard-coded in the Rust port:
    - `git/status` → `rewrite_git_status_lines()` rewrites `modified:` → `M:`, etc.
    - `cloud/gh` → `rewrite_gh_lines()` formats JSON records OR tab-delimited tables into `#123 title [open] (branch) 4c {bug, perf} 2026-05-01`.
11. Counter pass (post-keep or pre-keep)
12. `onEmpty` short-circuit
13. `head_tail()` window (failure override applies here)
14. Header assembly (`exit N` + facts) → `format_inline`
15. `select_inline_text` — picks between raw passthrough, compact text, and short-output passthrough using `TINY_OUTPUT_MAX_CHARS = 240` (`maxInlineChars` for `help` family)
16. `clamp_text` / `clamp_text_middle` (middle clamp if multiline or `help` family) at `maxInlineChars` (default 1200)

### 1.9 Caller input (`ToolExecutionInput`)

Required for matching:

| Field | Type | Notes |
|-------|------|-------|
| `toolName` | string | Maps to `match.toolNames` |
| `argv` | `string[]` | Auto-populated from `command` via `tokenize_command` if absent. |
| `command` | string | Falls back to `argv.join(" ")` when matching `commandIncludes*`. |
| `stdout` / `stderr` / `combinedText` | string | `build_raw_text` prefers `combinedText`; else `stdout\n stderr`. |
| `exitCode` | int | Triggers the `failure` block when ≠ 0. |
| `partial`, `cwd`, `runId`, `toolCallId`, `args`, `startedAt`, `finishedAt`, `durationMs`, `metadata` | misc | Carried through but not used by classification or reduction in v1. |

### 1.10 `ReduceOptions` (caller-side knobs, not in rule JSON)

| Field | Default | Notes |
|-------|---------|-------|
| `classifier` | `null` | Force a specific rule id, bypassing scoring. Found at `reduce_execution_with_rules(opts.classifier)`. |
| `maxInlineChars` | `1200` | Final clamp target. `help` family uses this as both passthrough and final cap. |
| `raw` | `false` | Bypass everything and return raw text. |
| `cwd` | `null` | For project-layer rule discovery (`.tokenjuice/rules/`). |

`TINY_OUTPUT_MAX_CHARS = 240` is a const inside `reduce.rs` and is NOT exposed as an option — outputs shorter than that bypass compaction altogether.

---

## 2. Inventory of built-in rules (all 96)

| Filename | ID | Family | Match anchor | Notable behavior |
|----------|----|--------|--------------|------------------|
| `archive__tar.json` | `archive/tar` | archive-cli | `argv0=[tar]` | counters: `error`. head=10 tail=8. |
| `archive__unzip.json` | `archive/unzip` | archive-cli | `argv0=[unzip]` | counter `warning` matches `inflating|extracting|replace|error`. |
| `archive__zip.json` | `archive/zip` | archive-cli | `argv0=[zip]` | counter `warning` matches `adding|updating|warning|error`. |
| `build__esbuild.json` | `build/esbuild` | build-bundler | `commandIncludes=[esbuild]` | counters: `error`, `warning`. |
| `build__tsc.json` | `build/tsc` | build-typescript | `commandIncludes=[tsc]` | aggressive `skipPatterns` for `--diagnostics`; keeps `TS\d+` errors; small head=4 tail=4. |
| `build__tsdown.json` | `build/tsdown` | build-bundler | `commandIncludes=[tsdown]` | esbuild-clone. |
| `build__vite.json` | `build/vite` | build-bundler | `commandIncludes=[vite, build]` | skips `transforming/rendering/computing gzip`. |
| `build__webpack.json` | `build/webpack` | build-bundler | `commandIncludes=[webpack]` | `keepPatterns` for `Entrypoint`, `ERROR in`, `Module`. tiny head=4 tail=6. |
| `cloud__aws.json` | `cloud/aws` | cloud-cli | `argv0=[aws]` | counter `error` over `error|exception|denied|not found`. |
| `cloud__az.json` | `cloud/az` | cloud-cli | `argv0=[az]` | counter `error` adds `forbidden`. |
| `cloud__flyctl.json` | `cloud/flyctl` | deploy-cli | `argv0=[fly, flyctl]` | counter `error` adds `unhealthy|warning`. |
| `cloud__gcloud.json` | `cloud/gcloud` | cloud-cli | `argv0=[gcloud]` | counter `error` adds `permission|denied`. |
| `cloud__gh.json` | `cloud/gh` | developer-cli | `argv0=[gh]` | **special post-processor** `rewrite_gh_lines` — parses JSON-per-line OR tab-delimited tables into compact `#NUM title [state] (branch) Nc {labels} date`. |
| `cloud__vercel.json` | `cloud/vercel` | deploy-cli | `argv0=[vercel]` | counter `error` adds `canceled|timed out`. |
| `database__mongosh.json` | `database/mongosh` | database-cli | `argv0=[mongosh]` | counter `error` over `error|failed|exception`. |
| `database__mysql.json` | `database/mysql` | database-cli | `argv0=[mysql]` | counter `error` adds `denied|unknown`. |
| `database__psql.json` | `database/psql` | database-cli | `argv0=[psql]` | counter `error` adds `permission denied`. |
| `database__redis-cli.json` | `database/redis-cli` | database-cli | `argv0=[redis-cli]` | counter `error` adds `could not connect`. head=8 tail=6. |
| `database__sqlite3.json` | `database/sqlite3` | database-cli | `argv0=[sqlite3]` | counter `error` adds `no such table`. head=8 tail=6. |
| `devops__docker-build.json` | `devops/docker-build` | container-build | `argv0=[docker] + argvIncludes=[[build]]` | skips BuildKit progress lines (`^#\d+\s+[0-9.]+\s`), keeps `[stage]`, `DONE`, `ERROR:`. counter `step`. |
| `devops__docker-compose.json` | `devops/docker-compose` | container-compose | `argv0=[docker] + argvIncludes=[[compose]]` | counters `service`, `error`. keeps NAME/SERVICE rows. |
| `devops__docker-images.json` | `devops/docker-images` | container-images | `argv0=[docker] + argvIncludes=[[images]]` | counter `image` over non-header lines. |
| `devops__docker-logs.json` | `devops/docker-logs` | container-logs | `argv0=[docker] + argvIncludes=[[logs]]` | keeps `error|warn|fatal|panic|exception|traceback|timeout|refused|fail` + `Caused by:` + `Traceback`. |
| `devops__docker-ps.json` | `devops/docker-ps` | container-list | `argv0=[docker] + argvIncludes=[[ps]]` | counter `container` over non-header rows. |
| `devops__kubectl-describe.json` | `devops/kubectl-describe` | kubernetes-describe | `argv0=[kubectl] + argvIncludes=[[describe]]` | keepPatterns for `Name|Namespace|...|Events:` headers + event rows. counters: `warning`, `event`. |
| `devops__kubectl-get.json` | `devops/kubectl-get` | kubernetes-list | `argv0=[kubectl] + argvIncludes=[[get]]` | counter `resource` over non-`NAME` rows. |
| `devops__kubectl-logs.json` | `devops/kubectl-logs` | kubernetes-logs | `argv0=[kubectl] + argvIncludes=[[logs]]` | same keepPatterns as docker-logs. |
| `filesystem__find.json` | `filesystem/find` | filesystem-find | `argv0=[find]` | keeps `^./.+`, `^/.+`, `Permission denied`, `No such file`. counters `match`, `permission denied`. |
| `filesystem__ls.json` | `filesystem/ls` | filesystem-listing | `argv0=[ls]` | counter `item` over non-`total` rows. |
| `generic__fallback.json` | `generic/fallback` | generic | `{}` (matches everything) | **Required**. counters: `error`, `warning`. head=8 tail=8, failure 12/20. |
| `generic__help.json` | `generic/help` | help | `toolNames=[exec] + argvIncludesAny=[[--help],[help]] + commandIncludesAny=[" --help"," help"]` | **`priority: 25`** (only rule with explicit priority). head=80 tail=40 (very generous because help is high-signal). |
| `git__branch.json` | `git/branch` | git-branches | `argv0=[git] + argvIncludes=[[branch]]` | counter `branch` matches `.+` (every line). |
| `git__diff-name-only.json` | `git/diff-name-only` | git-diff | `argv0=[git] + argvIncludes=[[diff],[--name-only]]` | head=16 tail=4. |
| `git__diff-stat.json` | `git/diff-stat` | git-diff | `argv0=[git] + argvIncludes=[[diff],[--stat]]` | counters `file` over `\|`, `insertion`, `deletion`. |
| `git__log-oneline.json` | `git/log-oneline` | git-history | `argv0=[git] + argvIncludes=[[log],[--oneline]]` | counter `commit` matches `^[a-f0-9]{7,}\s`. |
| `git__remote-v.json` | `git/remote-v` | git-remote | `argv0=[git] + argvIncludes=[[remote],[-v]]` | counter `remote` matches `\((fetch|push)\)`. |
| `git__show.json` | `git/show` | git-show | `argv0=[git] + argvIncludes=[[show]]` | keepPatterns for commit header, diff hunks, mode changes. counters: `file`, `commit`. |
| `git__stash-list.json` | `git/stash-list` | git-stash | `argv0=[git] + argvIncludes=[[stash],[list]]` | counter `stash` matches `^stash@\{\d+\}:`. |
| `git__status.json` | `git/status` | git-status | `argv0=[git] + argvIncludes=[[status]]` | **special post-processor** `rewrite_git_status_lines` → `M: path`, `A: path`, `D: path`, `R: path`, `?? path`. Strips `On branch`, `(use "git …")` hints. counters: `modified file`, `new file`, `deleted file`, `untracked file`. |
| `install__bun-install.json` | `install/bun-install` | dependency-install | `argv0=[bun] + argvIncludes=[[install]]` | `matchOutput` → `"bun install: up to date"`. counters: `warning`, `package`. |
| `install__npm-install.json` | `install/npm-install` | dependency-install | `argv0=[npm] + argvIncludes=[[install]]` | `onEmpty: "npm install: ok"`, `matchOutput` → `"npm install: up to date"`, skips `^npm notice `, counters: `warning`, `vulnerability`. |
| `install__pnpm-install.json` | `install/pnpm-install` | dependency-install | `argv0=[pnpm] + argvIncludes=[[install]]` | `matchOutput` → `"pnpm install: up to date"`. counters: `warning`, `package`. |
| `install__yarn-install.json` | `install/yarn-install` | dependency-install | `argv0=[yarn] + argvIncludes=[[install]]` | `matchOutput` → `"yarn install: up to date"`. counters: `warning`, `package`. |
| `lint__biome.json` | `lint/biome` | lint-results | `commandIncludes=[biome]` | counters `error`, `warning` (word-boundaried). |
| `lint__eslint.json` | `lint/eslint` | lint-results | `commandIncludes=[eslint]` | keepPatterns for filename/line:col/error/warning/`✖`/problems summary. |
| `lint__oxlint.json` | `lint/oxlint` | lint-results | `commandIncludes=[oxlint]` | clone of biome. |
| `lint__prettier-check.json` | `lint/prettier-check` | lint-results | `commandIncludes=[prettier, --check]` | counter `file` matches `\[[^\]]+\]`. |
| `media__ffmpeg.json` | `media/ffmpeg` | media-cli | `argv0=[ffmpeg]` | counter `error` over `error|invalid|failed|frame=`. |
| `media__mediainfo.json` | `media/mediainfo` | media-cli | `argv0=[mediainfo]` | counter `warning` triggers on `duration|format` too. |
| `network__curl.json` | `network/curl` | network-http | `argv0=[curl]` | counter `error` over `error|failed|timed out`. |
| `network__dig.json` | `network/dig` | network-dns | `argv0=[dig]` | counter `answer` over `ANSWER SECTION|\sIN\sA\s|\sIN\sAAAA\s`. |
| `network__nslookup.json` | `network/nslookup` | network-dns | `argv0=[nslookup]` | counter `server` matches `^Server:`. |
| `network__ping.json` | `network/ping` | network-probe | `argv0=[ping]` | counters: `reply`, `packet loss`. |
| `network__ssh.json` | `network/ssh` | network-remote-shell | `argv0=[ssh]` | counter `error` over `permission denied|connection refused|timed out|host key verification failed`. |
| `network__traceroute.json` | `network/traceroute` | network-route | `argv0=[traceroute]` | counter `hop` matches `^\s*\d+\s`. |
| `network__wget.json` | `network/wget` | network-http | `argv0=[wget]` | counter `error` over `error|failed`. |
| `observability__free.json` | `observability/free` | resource-memory | `argv0=[free]` | minimal — counter `warning` over `error|failed`. |
| `observability__htop.json` | `observability/htop` | resource-processes | `argv0=[htop]` | counter `warning` over `load average|tasks|zombie`. |
| `observability__iostat.json` | `observability/iostat` | resource-io | `argv0=[iostat]` | counter `busy` over `%util|Device`. |
| `observability__top.json` | `observability/top` | resource-processes | `argv0=[top]` | counter `warning` over `load average|zombie|stopped`. |
| `observability__vmstat.json` | `observability/vmstat` | resource-vm | `argv0=[vmstat]` | counter `warning` over `swpd|cache|wa|st`. |
| `package__apt-install.json` | `package/apt-install` | system-package-install | `argv0=[apt, apt-get] + argvIncludes=[[install]]` | skips `^Reading database …`. counter `error` over `error|failed|unable to`. |
| `package__apt-upgrade.json` | `package/apt-upgrade` | system-package-upgrade | `argv0=[apt, apt-get] + argvIncludes=[[upgrade]]` | counter `error` adds `kept back`. |
| `package__brew-install.json` | `package/brew-install` | system-package-install | `argv0=[brew] + argvIncludes=[[install]]` | counter `warning` over `warning|error|failed`. |
| `package__brew-upgrade.json` | `package/brew-upgrade` | system-package-upgrade | `argv0=[brew] + argvIncludes=[[upgrade]]` | same as brew-install. |
| `package__dnf-install.json` | `package/dnf-install` | system-package-install | `argv0=[dnf] + argvIncludes=[[install]]` | counter `error` adds `nothing to do`. |
| `package__yum-install.json` | `package/yum-install` | system-package-install | `argv0=[yum] + argvIncludes=[[install]]` | counter `error` adds `nothing to do`. |
| `search__git-grep.json` | `search/git-grep` | search | `argv0=[git] + argvIncludes=[[grep]]` | counter `match` matches `.+:.+`. |
| `search__grep.json` | `search/grep` | search | `argv0=[grep]` | keep file:line:content; `\d+ matches?` summary line; counters `match`. |
| `search__rg.json` | `search/rg` | search | `argv0=[rg]` | clone of grep with same keep set. |
| `service__journalctl.json` | `service/journalctl` | service-logs | `argv0=[journalctl]` | keepPatterns for `error|warn|fatal|panic|exception|traceback|timeout|refused|fail` + `Caused by:` + `Traceback`. |
| `service__launchctl.json` | `service/launchctl` | service-state | `argv0=[launchctl]` | keeps PID/Status/Label rows; counters `service`, `error`. |
| `service__lsof.json` | `service/lsof` | service-open-files | `argv0=[lsof]` | counter `entry` over non-header rows. |
| `service__netstat.json` | `service/netstat` | service-network-state | `argv0=[netstat]` | counter `socket` over non-`Proto|Active` rows. |
| `service__service.json` | `service/service` | service-state | `argv0=[service]` | keeps state/Active/Status rows + error/failed/inactive. counters: `warning`, `error`. |
| `service__ss.json` | `service/ss` | service-network-state | `argv0=[ss]` | counter `socket` over non-`Netid|State` rows. |
| `service__systemctl-status.json` | `service/systemctl-status` | service-state | `argv0=[systemctl] + argvIncludes=[[status]]` | keeps `●` header line + `Loaded/Active/Main PID/Tasks/Memory/CPU`. |
| `system__df.json` | `system/df` | system-disk | `argv0=[df]` | counter `filesystem` matches `.+` (every line). head=12 tail=4. |
| `system__du.json` | `system/du` | system-disk | `argv0=[du]` | counter `entry` over `^\S+\s+.+`. |
| `system__file.json` | `system/file` | file-inspection | `argv0=[file]` | counter `warning` over `cannot open|error`. |
| `system__ps.json` | `system/ps` | system-processes | `argv0=[ps]` | counter `process` over non-header rows. |
| `task__just.json` | `task/just` | task-runner | `argv0=[just]` | counter `error`. |
| `task__make.json` | `task/make` | task-runner | `argv0=[make]` | counter `error`. |
| `tests__bun-test.json` | `tests/bun-test` | test-results | `argv0=[bun] + argvIncludes=[[test]]` | keepPatterns: vitest-style `❯`/`✓`/`FAIL`/`PASS`/`AssertionError`/`Error:`/Test Files/Tests/Duration. counters: `failed`, `passed`. |
| `tests__cargo-test.json` | `tests/cargo-test` | test-results | `toolNames=[exec] + argv0=[cargo] + argvIncludes=[[test]]` | skips `Compiling/Finished/Running`. counters: `failed test` (`FAILED`), `passed test` (`ok`). |
| `tests__go-test.json` | `tests/go-test` | test-results | `argv0=[go] + argvIncludes=[[test]]` | skips `^ok\s` packages. keeps `FAIL`/`--- FAIL:`/`panic:`/`*_test.go:N`/`Error Trace`/`Error:`. counters: `failed package`, `passed package`. |
| `tests__jest.json` | `tests/jest` | test-results | `commandIncludes=[jest]` | skips `^\s*at `, `Ran all test suites`. counters: `failed test` (`^FAIL\s`), `passed suite` (`^PASS\s`). |
| `tests__mocha.json` | `tests/mocha` | test-results | `commandIncludes=[mocha]` | counters: `failing`, `passing`. |
| `tests__npm-test.json` | `tests/npm-test` | test-results | `argv0=[npm] + argvIncludes=[[test]]` | vitest-style keep set (generic catch-all for `npm test`). |
| `tests__playwright.json` | `tests/playwright` | test-results | `argv0=[playwright,pnpm,npx,bunx,yarn,npm] + argvIncludes=[[playwright],[test]] + commandIncludes=[playwright,test]` | maximal match set — Playwright is invoked many ways. counters: `failed`, `passed`. |
| `tests__pnpm-test.json` | `tests/pnpm-test` | test-results | `argv0=[pnpm] + argvIncludes=[[test]]` | vitest-style keep set. |
| `tests__pytest.json` | `tests/pytest` | test-results | `commandIncludes=[pytest]` | **`counterSource: "preKeep"`** so failed-test counts survive keep-filtering. keeps `==…failed/passed/error…==`, `FAILED`, `ERROR`, `AssertionError`, `>`, `nodeid::… (FAILED|ERROR)`. |
| `tests__vitest.json` | `tests/vitest` | test-results | `commandIncludes=[vitest]` | skips `\s*at`, in-node_modules frames; keepPatterns for `❯`/`FAIL`/Test Files/Tests. |
| `tests__yarn-test.json` | `tests/yarn-test` | test-results | `argv0=[yarn] + argvIncludes=[[test]]` | vitest-style keep set. |
| `transfer__rsync.json` | `transfer/rsync` | file-transfer | `argv0=[rsync]` | counter `error` over `error|failed|connection|sent `. |
| `transfer__scp.json` | `transfer/scp` | file-transfer | `argv0=[scp]` | counter `error` over `error|failed|permission denied|lost connection`. head=8 tail=6. |

**Wiring vs vendored:** all 96 are wired via `BUILTIN_RULE_JSONS` in `rules/builtin.rs`. There is no rule that is vendored-but-not-wired or wired-but-not-vendored — the vendoring is exhaustive.

---

## 3. Categorization by domain

### 3.1 Archive (3)
`archive/tar`, `archive/unzip`, `archive/zip`

### 3.2 Build / bundler / compiler (5)
`build/esbuild`, `build/tsc`, `build/tsdown`, `build/vite`, `build/webpack`

### 3.3 Cloud CLI (6)
`cloud/aws`, `cloud/az`, `cloud/flyctl`, `cloud/gcloud`, `cloud/gh` *(JSON+table reformatter)*, `cloud/vercel`

### 3.4 Database CLI (5)
`database/mongosh`, `database/mysql`, `database/psql`, `database/redis-cli`, `database/sqlite3`

### 3.5 Container / orchestration (8)
`devops/docker-build`, `devops/docker-compose`, `devops/docker-images`, `devops/docker-logs`, `devops/docker-ps`, `devops/kubectl-describe`, `devops/kubectl-get`, `devops/kubectl-logs`

### 3.6 Filesystem (2)
`filesystem/find`, `filesystem/ls`

### 3.7 Generic / fallback (2)
`generic/fallback` *(required, last)*, `generic/help` *(priority=25)*

### 3.8 Git (8)
`git/branch`, `git/diff-name-only`, `git/diff-stat`, `git/log-oneline`, `git/remote-v`, `git/show`, `git/stash-list`, `git/status` *(special porcelain rewriter)*

### 3.9 Package manager — install (4)
`install/bun-install`, `install/npm-install`, `install/pnpm-install`, `install/yarn-install`

### 3.10 Lint (4)
`lint/biome`, `lint/eslint`, `lint/oxlint`, `lint/prettier-check`

### 3.11 Media (2)
`media/ffmpeg`, `media/mediainfo`

### 3.12 Network (7)
`network/curl`, `network/dig`, `network/nslookup`, `network/ping`, `network/ssh`, `network/traceroute`, `network/wget`

### 3.13 Observability (5)
`observability/free`, `observability/htop`, `observability/iostat`, `observability/top`, `observability/vmstat`

### 3.14 System package manager (6)
`package/apt-install`, `package/apt-upgrade`, `package/brew-install`, `package/brew-upgrade`, `package/dnf-install`, `package/yum-install`

### 3.15 Search (3)
`search/grep`, `search/git-grep`, `search/rg`

### 3.16 Service / sysadmin state (7)
`service/journalctl`, `service/launchctl`, `service/lsof`, `service/netstat`, `service/service`, `service/ss`, `service/systemctl-status`

### 3.17 System core (4)
`system/df`, `system/du`, `system/file`, `system/ps`

### 3.18 Task runner (2)
`task/just`, `task/make`

### 3.19 Tests (11)
`tests/bun-test`, `tests/cargo-test`, `tests/go-test`, `tests/jest`, `tests/mocha`, `tests/npm-test`, `tests/playwright`, `tests/pnpm-test`, `tests/pytest` *(preKeep counters)*, `tests/vitest`, `tests/yarn-test`

### 3.20 Transfer (2)
`transfer/rsync`, `transfer/scp`

---

## 4. Relevance scoring for Aura

### 4.1 Aura's verbose-output tools (from `docs/qa-tool-surface.md`, snapshot 2026-05-18)

| Aura tool | Capability | Typical output verbosity | TokenJuice fit |
|-----------|-----------|--------------------------|----------------|
| `execute_shell` (`internal/agent/tools/registry/exec.go:416`) | sandbox-exec | Whatever the user shell command emits — git, npm, cargo, docker, grep, find, build, test, etc. | **HIGHEST** — this is the canonical TokenJuice input surface. Every vendored rule lands here. |
| `execute_code` (`exec.go:148`) | sandbox-exec (Python) | Stdout from Python sandbox runs. Often pytest, requests dumps, pandas tables. | HIGH for `tests/pytest`, MEDIUM for generic Python noise (fallback rule). |
| `web` action=fetch (`web.go:115`, helper `direct_fetch.go:168`) | external-API | HTML/markdown, JSON API responses, sometimes 10k+ char pages. | LOW — none of the vendored rules target HTML; the generic fallback gives ok compaction but proper HTML→text reduction belongs in a separate path (we already truncate via `web_common`). |
| `search_memory` (`memory_search.go:161`) | read-only | Multi-doc results joined into one string. | LOW — not line-oriented in the tokenjuice sense; output structure is doc snippets. |
| `source` action=read (`source_read.go:55`) | storage-write | Full document bytes (PDF text, OCR markdown). | LOW — semantic content; truncation belongs in the source layer, not regex compaction. |
| `source` action=ingest (`ingest.go:45`) | storage-write | LLM extraction output — prose. | LOW. |
| MCP `mcp_<server>_<tool>` (`mcp.go:79`) | external-API | Highly variable per server (filesystem, github MCP servers can be huge). | MEDIUM — generic fallback helps; specific rules would need to know each MCP server. |

### 4.2 Per-category Aura score

| Category | Aura score | Reason |
|----------|-----------|--------|
| Git (8 rules) | **HIGH** | User shells `git status/log/diff/show/branch` constantly via `execute_shell`. `git/status` rewriter alone saves ~70% tokens on typical statuses. |
| Tests (11 rules) | **HIGH** | `cargo test`, `go test`, `pytest`, `jest`, `vitest` runs are some of the biggest outputs we get. `preserveOnFailure` is exactly the right policy for Aura. |
| Search (3) | **HIGH** | `grep`/`rg`/`git grep` runs via `execute_shell` are extremely common and very verbose. The keep+counter pattern is gold. |
| Generic (2) | **REQUIRED** | `generic/fallback` is non-optional. `generic/help` saves a lot of tokens on `tool --help` runs. |
| Build (5) | **MEDIUM** | Useful when users build inside Aura, e.g. `npm --prefix web run build`. |
| Container/orchestration (8) | **MEDIUM** | `docker ps`, `docker logs`, `kubectl logs` are common debug commands. |
| Package install (4) + system pkg (6) | **MEDIUM** | `matchOutput` for "up to date" cases is high-value. |
| Filesystem (2), System (4) | **MEDIUM** | `find`, `ls`, `df`, `du`, `ps` are routine but already short usually. |
| Lint (4) | **MEDIUM** | Useful when Aura is reviewing user repos. |
| Cloud (6) | **LOW** | `cloud/gh` is interesting if Aura ever runs `gh` directly. AWS/Azure/GCP CLI rarely shows up in Aura's workload. |
| Database CLI (5) | **LOW** | Aura is more likely to talk to DBs through MCP, not raw `psql`/`mysql`. |
| Network (7) | **LOW** | `curl`/`wget` outputs are already truncated by `web_fetch`; raw `ping`/`dig`/`traceroute` calls are niche. |
| Service/observability/transfer/media/task/archive (~22) | **LOW** | Rare in Aura's workload. Generic fallback is enough. |

### 4.3 Recommended Aura starter set (~10 rules)

These cover ≥80% of Aura's `execute_shell` token waste while keeping the vendored JSON footprint tiny.

1. **`generic/fallback`** — required. The skeleton head/tail with `error|warning` counters.
2. **`generic/help`** — priority=25. Saves tokens on every `--help` invocation.
3. **`git/status`** — the killer feature; the porcelain rewriter is a special post-processor we must port.
4. **`git/log-oneline`** — frequent in PR context-gathering.
5. **`git/diff-stat`** — common when summarizing changes.
6. **`tests/go-test`** — Aura is a Go codebase; runs go test inside `execute_shell` or in our CI prompts.
7. **`tests/pytest`** — `execute_code` Python sandbox + user Python projects. `preKeep` counters logic must be ported.
8. **`search/rg`** — best general-purpose search compaction (covers `rg` and the same keepPatterns trivially extend to `grep`).
9. **`build/tsc`** — Aura ships React; `tsc --noEmit` runs are huge.
10. **`install/npm-install`** — `onEmpty` + `matchOutput` give 90%+ savings on no-op installs.

**Honorable mentions** (consider as v1.1):
- `devops/docker-logs` and `devops/kubectl-logs` (same keep set; one shared rule could cover both).
- `cloud/gh` if we start using `gh` directly.
- `tests/jest` / `tests/vitest` if web tooling gets exercised inside Aura.

---

## 5. License & attribution

### 5.1 What we are studying vs what we can use

- **openhuman's Rust port** is **GPLv3** (`D:/tmp/openhuman/LICENSE`). We are studying its structure (`reduce.rs`, `classify.rs`, `types.rs`) but we **must NOT copy any Rust source** into Aura. Aura is not GPLv3, so doing so would force a license change. Concepts and algorithms are not copyrightable; we re-implement.
- **Upstream `vincentkoc/tokenjuice`** is **MIT** (per `vendor/README.md` and the embedded MIT text — Copyright © 2026 Vincent Koc). The rule JSONs come from `src/rules/**/*.json` upstream.
- **The rule JSON format** is a data schema. Data schemas are not copyrightable; we can re-implement the schema and parser in Go without restriction.
- **Individual rule JSON files** are short, fact-shaped configurations. They are MIT-licensed by Vincent Koc. We can copy them verbatim into Aura under the MIT terms — namely: include the MIT copyright notice + permission text in the rule directory (or in an `NOTICES.md`).

### 5.2 What Aura needs to ship

If we vendor any rule JSON verbatim:

1. Add `internal/tokenjuice/rules/LICENSE-UPSTREAM` (or similar) containing the upstream MIT text and copyright line:
   `Copyright (c) 2026 Vincent Koc`
2. Add a top-of-package comment in our Go reducer file pointing to the upstream repo and noting which behaviors are inspired (algorithm) vs copied (rule JSONs).
3. Do NOT credit openhuman in the rule directory — we are not vendoring openhuman code. The Rust port is reference material only.
4. NEVER copy text/code from `src/openhuman/tokenjuice/*.rs` or its tests into Aura's Go tree.

If we author every rule JSON ourselves (which is trivial for the ~10-rule starter set), no attribution is required at all. Given the small starter set, **rewriting the 10 rules in our own words is the cleaner path** — it gives us total freedom to tailor patterns to Aura's exact `execute_shell` quirks without dragging in upstream's choices.

### 5.3 Patent/trademark

None — MIT has no patent clause; tokenjuice is not a trademark.

---

## 6. Fixture-driven test cases

Only three fixture files exist (`tests/fixtures/`); the bulk of upstream's fixtures live in TS and were not vendored into the Rust port. Each fixture is `RuleFixture` shape: `{ input: ToolExecutionInput, expectedOutput: string, description?, options? }`.

### 6.1 `git_status_modified.fixture.json`

- **Rule under test:** `git/status`
- **Description:** "git status with a modified file rewrites to compact M: notation; hint lines are preserved when indented (Rust port behavior)"
- **Input:** `argv=["git","status"]`, stdout:
  ```text
  On branch main

  Changes not staged for commit:
  \tmodified:   src/foo.rs

  no changes added to commit (use "git add" and/or "git commit -a")
  ```
- **Expected output:**
  ```text
  Changes not staged:
  M: src/foo.rs
  ```
- **Why it matters:** Exercises (a) the porcelain rewriter (`modified:` → `M:`), (b) `skipPatterns` for `On branch` + `no changes added` + the `(use "git …")` hint, (c) section-header rewrite (`Changes not staged for commit:` → `Changes not staged:`).

### 6.2 `cargo_test_failure.fixture.json`

- **Rule under test:** `tests/cargo-test`
- **Description:** "cargo test failure: exit code + facts header + preserved output"
- **Input:** `argv=["cargo","test"]`, `exitCode=1`, full cargo output with 3 tests (1 FAILED).
- **Expected output:**
  ```text
  exit 1
  2 failed tests, 2 passed tests
  running 3 tests
  test tests::test_a ... ok
  test tests::test_b ... FAILED
  test tests::test_c ... ok

  failures:

  ---- tests::test_b stdout ----
  thread 'tests::test_b' panicked at 'assertion failed', src/lib.rs:42:5

  failures:
      tests::test_b

  test result: FAILED. 2 passed; 1 failed; 0 ignored
  ```
- **Why it matters:** Exercises (a) `exit N` prefix when non-zero exit code, (b) facts header pluralization (`pluralize`), (c) `failure.preserveOnFailure: true` widening head/tail so the panic context is fully preserved, (d) `skipPatterns` stripping `Compiling/Finished/Running` lines from the top.
- **Edge case:** Counter `passed test` regex is just `ok`, which matches both the "ok" tail of test_a/test_c AND inadvertently any other `ok` substring — the fixture documents the resulting count of 2 (test_a and test_c only, because the `tests::test_b` FAILED line doesn't contain `ok`).

### 6.3 `fallback_long_output.fixture.json`

- **Rule under test:** `generic/fallback`
- **Description:** "Long generic output (20 lines) gets head=8 tail=8 summarised by fallback rule"
- **Input:** `argv=["some_tool"]`, stdout = `line 1\n…\nline 20`
- **Expected output:**
  ```text
  line 1
  line 2
  line 3
  line 4
  line 5
  line 6
  line 7
  line 8
  ... 4 lines omitted ...
  line 13
  line 14
  line 15
  line 16
  line 17
  line 18
  line 19
  line 20
  ```
- **Why it matters:** Exercises the canonical `head_tail` window: with 20 lines, head=8 + tail=8, the middle `20 - 8 - 8 = 4` lines collapse to a single `... 4 lines omitted ...` separator. This is the lowest-common-denominator behavior every reducer falls back to.

### 6.4 Additional fixtures we should add for the Go port

Because upstream's fixtures are sparse, we should write Aura-side fixtures that cover the special-cased plumbing:

- `git/log-oneline` with 30 commits → verify head=8 tail=6 + commit counter.
- `tests/pytest` with both passing and failing tests → verify `preKeep` counters keep counting `passed test` lines even though the `keepPatterns` whitelist drops the `PASSED` lines.
- `install/npm-install` with "up to date, audited 250 packages" → verify `matchOutput` short-circuit replaces summary entirely.
- `install/npm-install` with empty stdout → verify `onEmpty` returns `"npm install: ok"`.
- `cloud/gh pr list --json` JSON-per-line → verify `rewrite_gh_lines` JSON formatter.
- `generic/help` with `argv=["aws","--help"]` → verify priority=25 wins over `cloud/aws` and head=80 tail=40 applies.
- A 200-char short output → verify `TINY_OUTPUT_MAX_CHARS=240` passthrough kicks in.
- Same input with `exitCode=1` → verify failure-mode head/tail.

---

## 7. Implementation notes for the Go port

These are observations the Go author should keep in mind:

- **Regex engine:** Rust port uses `regex` crate which does NOT support backreferences or lookaround. Some upstream patterns rely on negative lookahead (e.g. `^(?!NAME\s).+`). Go's `regexp` package uses RE2 which has the same limitation — **good news**, the rule set is already RE2-compatible. No upstream pattern uses lookbehind.
- **Pattern flags:** The vendored rules use only `"i"` (case-insensitive) and `"m"` (multiline, anchors match per-line). Go's `regexp` syntax handles both as inline `(?i)` and `(?m)` prefixes — straightforward to translate.
- **Special post-processors:** `git/status` and `cloud/gh` have hard-coded Go-level (TS-level) logic. Decide whether to (a) port them, (b) skip them for v1 (rule still classifies and head/tail-summarizes, just without porcelain rewrite), or (c) make them pluggable via a `family → postprocessor` registry. **Recommendation: skip both for v1**; the rules still give big wins without the rewriter.
- **`pluralize`:** Upstream uses naive "add s if count != 1, except when name already ends in s". Easy port.
- **`TINY_OUTPUT_MAX_CHARS = 240`:** Hard-coded in `reduce.rs`. Expose this as a config knob in the Aura port — different LLM contexts have different "small" thresholds.
- **No state, no I/O:** The library is pure. All builtin rules are embedded via `include_str!` — Go equivalent is `//go:embed`. Three-layer overlay (builtin/user/project) needs file discovery; skip for v1, only ship the builtin layer.
- **Performance:** The hot loop is per-line regex matching. The Rust port uses a thread-local regex cache; Go would use a `sync.Map[string]*regexp.Regexp` or precompile on rule load. Precompile-at-load is simpler and adequate.
- **`onEmpty` and `matchOutput` ordering:** `matchOutput` is checked first (on trimmed full text), before filters or counters run. `onEmpty` is checked after filters when `lines.len() == 0`. Make sure to preserve this in the Go pipeline.
- **`select_inline_text`:** The "pick raw vs compact vs passthrough" heuristic is subtle. The clean Go translation is exactly the four-branch decision tree in `reduce.rs::select_inline_text` — don't try to simplify it.

---

## Top-10 priority list (recap)

| # | Rule | One-line rationale |
|---|------|--------------------|
| 1 | `generic/fallback` | Required floor — every uncategorized `execute_shell` output gets head/tail compaction with error/warning facts. |
| 2 | `git/status` | The single highest-value rule for Aura's workload — porcelain rewrite slashes 70% of `git status` tokens. |
| 3 | `tests/go-test` | Aura is a Go codebase; `go test` is invoked by us and by users mirroring our patterns. |
| 4 | `tests/pytest` | Python sandbox + user-side Python; the `preKeep` counter trick keeps fail counts truthful. |
| 5 | `search/rg` | Ripgrep dumps are massive; keep+counters preserve the matches and surface "N matches" cleanly. |
| 6 | `build/tsc` | TypeScript builds (web dashboard) drop `--diagnostics` noise while preserving real `TS\d+` errors. |
| 7 | `install/npm-install` | `onEmpty` + `matchOutput` collapse no-op runs to one line — major win for repeated installs. |
| 8 | `git/log-oneline` | Frequent in PR/context gathering; cheap head/tail with commit counter. |
| 9 | `git/diff-stat` | Common when summarizing changes; tiny rule, big readability win. |
| 10 | `generic/help` | `priority=25` saves tokens on every `<cmd> --help` invocation without losing the structure agents need. |
