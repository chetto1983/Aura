# Go Module Hygiene — 2026 State of the Art

Research date: 2026-05-22
Scope: external 2026 best-practice survey for module-by-module modernization of a
~83k-LOC Go monorepo. **No Aura-specific recommendations** — this document
captures the industry baseline that a downstream synthesis pass can apply.

Format per dimension: claim → tool / source → 2026 status → gap.

---

## 1. Per-module test coverage discipline

### Consensus targets (2026)

The 2026 picture is **not** "everyone agrees on 80%". It splits by risk class:

| Class                              | Typical floor      | Notes |
|------------------------------------|--------------------|-------|
| Safety-critical (medical, finance) | 80–95 %+           | Per-module enforced |
| Standard libraries / agent loops   | 70–80 %            | Aligns with `std` average reported at ~75–80 % |
| Internal tools / glue code         | 60–70 %            | Project-wide floor acceptable |
| Generated code, fixtures           | Excluded           | Use `flags` / `components` to carve out |

Source: OtterWise — *Go Code Coverage Tracking: Best Practices and CI/CD Integration*.
Codecov's [5 Levels of Code Coverage](https://about.codecov.io/blog/the-5-levels-of-code-coverage-how-to-build-a-testing-culture-in-your-organization/)
formalises the maturity ladder.

### Per-module enforcement mechanics

Codecov 2026 supports **per-module floors** via `flags` and `components`. You declare
in `codecov.yml` a different `target` per file-path glob. Example pattern from
multiple 2026 case writeups: `internal/agent/** = 80 %`, `internal/api/** = 70 %`,
`cmd/** = 50 %`, `internal/**/generated/** = ignore`.

Native Go: `go test -coverprofile=cover.out ./internal/<pkg>` per package, then
`go tool cover -func=cover.out` for the breakdown. The build-cover feature
(go.dev/doc/build-cover) collects coverage from **integration test runs**, not
just unit tests — important for agent systems where a lot of behavior is only
reachable through a real LLM round-trip.

### Mutation testing

The 2026 reference tool is `avito-tech/go-mutesting` (fork of zimmski/go-mutesting).
The official Avito fork keeps the project current; the original repo is stagnant.
The newer `mutest` keeps mutant counts at 10–50 per package, making the cost
tractable in CI.

> "By limiting scope, mutest keeps the mutant count small — typically 10–50 per
> package instead of thousands." — DEV.to *Your Go Tests Pass, But Do They
> Actually Test Anything?*

Recommended cadence in 2026 writeups: run mutation testing **only on packages
with public APIs and high blast radius**, not the whole repo. Weekly cron in CI,
not per-PR.

### Property-based testing

The 2026 consensus tool is **`pgregory.net/rapid`** (formerly `flyingmutant/rapid`).
Gopter is functional but considered "Scala-port", with a larger API surface. Rapid's
auto-minimisation and the `rapid.MakeFuzz` adapter (any rapid test can be promoted
to a Go-1.18+ `testing.F` fuzz target with one line) make it the de-facto pick.

Native `testing.F` fuzzing (Go 1.18+) is the right tool for parser-shaped surfaces
(JSON / YAML / wire formats / regex inputs). It is **coverage-guided** and runs
on the same `go test` cadence. Rapid lacks coverage-guided feedback; the two are
complementary.

### Gap / contested

- **No 2026 consensus on a uniform per-module floor**. Coverage as a number is
  increasingly seen as a **leading indicator**, not a goal — see qodo.ai
  *10 Code Quality Metrics for Large Engineering Orgs (2026)*: "Code quality
  metrics work best as early signals, not scoreboards."
- **Mutation testing adoption is still <10 %** of mid-size Go shops. Cost is the
  killer: even with mutest's scoping, a 25-module repo can hit 60–90 min/run.

---

## 2. LOC budgets per file

### What linters actually enforce

`golangci-lint` does **not** ship a built-in *file-level* LOC linter as of v2.11.
The closest options are:

| Linter   | Scope          | Default | Configurable |
|----------|----------------|---------|--------------|
| `funlen` | per-function   | 60 lines / 40 statements | yes |
| `gocognit` | per-function (cognitive complexity) | 30 (recommended 10–20) | yes |
| `gocyclo` | per-function (cyclomatic complexity) | 10–15 typical | yes |
| `lll`    | per-line length | 120 chars | yes |
| `filen`  | per-file LOC   | community plugin, not in golangci-lint | yes |

Reference: [max-lines rule discussion #2881](https://github.com/golangci/golangci-lint/discussions/2881)
— maintainers have repeatedly declined a built-in file-LOC rule, citing
false-positive risk (migrations, generated code, fixtures).

### How mature projects actually enforce LOC

Three patterns observed across 2026 case studies:

1. **CI warning, not hard fail.** `funlen` + `gocognit` are wired as warnings with
   `severity: warning`, escalating to fail only on **new** violations (delta-mode).
   Migrate-mode: `golangci-lint run --new-from-rev=origin/master` only blocks files
   the PR touches.
2. **PR-template checkbox.** "I attest no file in this PR exceeds 600 LOC, or I've
   filed an issue." Cheap, social pressure works.
3. **Custom analyzer.** Few shops (Uber, GitHub, Hashicorp internal writeups)
   ship a custom `go/analysis` plugin via `golangci-lint`'s
   [custom analyzers](https://golangci-lint.run/plugins/module-plugins/) hook.
   Pattern: read file size, allow opt-in `//nolint:filen` for migrations.

### Handling "necessarily large" files

Established carve-outs:
- `internal/db/migrations/*.go` — exempt (append-only by design)
- `*_gen.go` / `mocks_*.go` — exempt (generated)
- `*_test.go` table-driven tests — usually exempted but watched for genuine bloat
- Any file with `//go:generate` header — typically exempt

### Gap

- `filen` is not officially supported in golangci-lint v2. Most shops still
  custom-write the check.
- **600 LOC is not industry standard.** Google internal style is silent on file
  size; Uber Go style guide caps `funlen` at 60 but doesn't mention files;
  Hashicorp public repos run with no file-size cap. The 600 cap is opinionated.

---

## 3. Dead code detection in 2026

### State of the art

| Tool                            | Status 2026          | Strength | Weakness |
|---------------------------------|----------------------|----------|----------|
| `golang.org/x/tools/cmd/deadcode` | **Production** (May 2026) | RTA call graph; sound about reflection + interface dispatch | Doesn't understand `//go:linkname`; misses test-only code unless `-test` |
| `golangci-lint`'s `unused`      | Production           | Fast; runs per-package          | Per-package only — misses cross-package dead exports |
| `unparam`                       | Production           | Catches unused function params  | Often flags interface-required params |
| `errcheck`                      | Production           | Unchecked errors                | Tangential to dead-code but caught here |
| `arxeiss/deadmono`              | New 2026             | Wraps `deadcode` for monorepos with multiple `main` packages | Niche; YAML-config heavy |

Source: [Finding unreachable functions with deadcode](https://go.dev/blog/deadcode) (Go blog),
[deadmono](https://github.com/arxeiss/deadmono).

### Reflection-blind spot — the registry pattern

Quote from the Go blog on `deadcode`:

> "Calls made using reflection are considered to reach any method of any type
> used in an interface conversion, or any type derivable from one using the
> reflect package."

This is **sound but coarse**: `deadcode` will not flag a registered-but-never-called
tool because the registry's `reflect.Call` is conservatively assumed to hit every
implementor of the registered interface. Result: a tool registered into a map
but never matched by name will be reported as *live*. False negative.

### How mature projects handle reflection-based registries

Three patterns in 2026 writeups (Sonar, in-com.com *20 Static Analysis Tools*):

1. **Static registration declarations.** Replace `registry.Register(x)` with an
   exported `var _ = registry.Register(x)` at package-init time, then write a
   custom `go/analysis` pass that **lists declared registrations** and a runtime
   probe that **lists actually-invoked names**, and diff the two in CI.
2. **String-table audit.** Maintain `tools.json` (or equivalent) generated from
   the registry. CI step: `go run ./cmd/dump-tools > tools.json.new && diff
   tools.json tools.json.new`. Forces a human to acknowledge any add/remove.
3. **Telemetry-driven usage logs.** Production observability (OpenTelemetry
   span attributes) records `tool.name` on every invocation. A monthly cron
   diffs registry contents vs. last-30-days invocation set. Flags zero-usage
   tools as candidates for deletion.

### Gap

The Go ecosystem **does not have** a turn-key analyzer for "registered but never
called by name". You have to write the diff yourself. Pattern (2) above is the
cheapest and is the de-facto 2026 norm.

---

## 4. Duplication detection

### Current toolset

| Tool       | Approach            | Status 2026 | Notes |
|------------|---------------------|-------------|-------|
| `golangci/dupl` | AST token-sequence (ignores values, compares types) | Production; bundled in golangci-lint | False positives on small clones (boilerplate getters/setters) |
| `mibk/dupl`     | Original upstream of golangci/dupl | Less maintained | Effectively superseded |
| `lizard`        | Multi-language CPD + cyclomatic | Stable | Supports Go but not idiomatic |
| PMD/CPD Go shim | None native; PMD doesn't target Go | n/a | Closest analog is `lizard` |

Source: [golangci/dupl](https://github.com/golangci/dupl),
[analysis-tools.dev — dupl](https://analysis-tools.dev/tool/dupl).

### AST vs semantic detection

`dupl` is **type-token-sequence based**: `if a == 13 {}` and `if x == 100 {}`
are considered the same clone. This is sufficient for ~90 % of clone detection
needs but produces false positives on:
- Generic error-handling boilerplate
- Table-driven test scaffolding
- HTTP handler signature ceremony

There is no production-grade **semantic-level** clone detector for Go in 2026.
Research papers exist (e.g., `gocd`, `simian` Go port attempts) but none is
golangci-lint-integrated.

### Threshold guidance

Default `dupl` token threshold = 150. Most 2026 writeups recommend:
- **60** for "ruthless mode" (catches small repeating idioms — heavy false-positive load)
- **100** for "balanced" (recommended for CI delta-mode)
- **150** for "loose" (catches only structural duplicates)

### Gap

- No semantic-level alternative is production-ready.
- `dupl` flags inevitable parallel structure (3 file-format generators are
  legitimately similar). The right answer is `//nolint:dupl // legit parallel
  generators`, not a refactor — context matters.

---

## 5. Dependency boundary enforcement

### Tool comparison (2026)

| Tool             | Approach                  | Status 2026 | Strength |
|------------------|---------------------------|-------------|----------|
| `depguard`       | Per-package allow/deny list (golangci-lint plugin) | **Production**, v2 active | Cheapest; YAML config; integrated |
| `fe3dback/go-arch-lint` | Layer-aware (hexagonal/DDD/onion), YAML rules | Production, active (v3 config) | Best for **named architectural layers** |
| `arch-go/arch-go`| Spec-as-code architecture tests | Active | Test-style assertions in code |
| `bvwells/go-clean-arch` | Clean architecture specifically | Active | Niche but battle-tested |
| Hand-rolled `go list -deps` | AST/import-graph check | Always works | DIY but no reporting layer |

Source: [go-arch-lint](https://github.com/fe3dback/go-arch-lint),
[OpenPeeDeeP/depguard](https://github.com/OpenPeeDeeP/depguard),
[arch-go](https://github.com/arch-go/arch-go).

### Recommended stack in 2026

The dominant pattern is **layered**:

1. `depguard` for **simple forbidden-import rules** (e.g., "no package in
   internal/storage may import internal/agent"). One-liner per rule in
   `.golangci.yml`.
2. `go-arch-lint` for **named architectural layers** when there are 3+ layers
   with directional rules ("agent depends on llm, llm depends on nothing").

You do **not** need both unless your architecture has both flat boundaries and
hierarchical layers.

### Strict vs Lax mode (depguard v2)

- **Strict**: everything denied unless explicitly allowed. Best for new modules.
- **Lax**: everything allowed unless explicitly denied. Best for retrofitting
  rules onto an existing codebase without breaking everything on day one.

### Gap

- `go-arch-lint`'s v3 config is more verbose than the v2 — some 2026 issue
  threads complain about migration cost.
- Neither tool handles **transitive** boundary violations gracefully (A → B → C
  where A is allowed to import B but B is not allowed to import C). You'd
  catch that in B's `depguard` rule, not at the import site in A.

---

## 6. API surface stability — internal packages

### Patterns documented in 2026 writeups

1. **`internal/` is the wall.** Go's built-in `internal/` directory blocks
   import from outside the parent module — this is the **only** language-level
   API control. Source: original Go proposal *internal packages*.
2. **Interface segregation per-consumer.** The 2026 consensus (rednafi.com
   *Revisiting Interface Segregation in Go*, HN discussion #45789218): "Define
   interfaces in the consumer, not the producer." This means `internal/agent`
   declares its narrow view of `LLMClient`; `internal/llm` exports a struct.
   Each consumer gets the smallest interface it actually uses.
3. **Exported-symbol audit via `apidiff`.** `golang.org/x/exp/apidiff` reports
   incompatible API changes between two versions of a package. Run in CI on
   the "stable" packages — fails the PR if a function signature changed
   incompatibly.
4. **Symbol allowlist file.** Some shops maintain `api.txt` per package
   (analogous to `cmd/api`'s `go1.txt` baseline for stdlib) listing the
   intended public surface; CI fails on unauthorized additions.

### Gap

- `apidiff` is still in `x/exp` after 6+ years. Reliable but officially
  experimental.
- No standard tool for "internal package SemVer" — you have to roll your
  own breaking-change-detector pipeline.

---

## 7. Cleanup-on-touch vs horizontal sweep

### Evidence from 2026 case studies

**Strong consensus against big-bang sweeps:**

- IBM 2026 *Reducing technical debt in 2026*: "CIOs are taking a different
  approach … reducing debt incrementally while protecting velocity, moving
  away from the traditional 'big cleanup' with multi-year re-platforming."
- microservices.io *STOP hurting yourself by doing big bang modernizations*
  (2024, re-promoted 2026): the big rewrite "takes longer than expected, costs
  more than planned, often creates new technical debt."
- Sweep.io *How to Manage Technical Debt in 2026*: 10–30 % of sprint capacity
  reserved for cleanup is the 2026 norm; 20–50 % productivity gain over time.
- Moderne *Tackling Tech Debt at Scale (Morgan Stanley)*: automated targeted
  refactors across the repo, modernizing patterns incrementally.

**McKinsey 2024 quoted in 2026 IBM piece:** systematic modernization (read:
continuous, automated, incremental) achieves 40–50 % faster completion times
and 30–40 % cost reduction vs. ad-hoc cleanup.

### When horizontal IS the right call

Despite the prevailing anti-big-bang sentiment, horizontal sweeps are
defensible in three cases:

1. **Pre-handoff hygiene.** Onboarding a new dev / contractor / co-founder
   benefits from a single "now everything is clean" moment.
2. **Pre-major-release stabilization.** Before locking the API surface for v1.0,
   one pass to ensure exports are intentional, not accidental.
3. **Post-spike rationalization.** After a heavy feature-exploration phase
   that left the codebase in an "MVP everywhere" state, a horizontal sweep is
   how you get back to baseline.

### Decision metrics (2026 reference list)

A horizontal sweep is justified when **two or more** of these are true:
- LOC growth rate > 20 % over the last 90 days
- Bug-per-touch rate > 0.3 (one bug reverted per 3 PRs)
- New-contributor onboarding time > 5 days for a "simple" change
- `golangci-lint` warning count growing > linearly
- More than 25 % of files exceed any of the "loose" thresholds (funlen, dupl,
  file size, gocognit)

Source synthesis: qodo.ai *10 Code Quality Metrics 2026*, CodeScene hotspot
guidance.

### The Boy Scout Rule vs deep refactor on touch

The 2026 hardening of the Boy Scout Rule: **same commit, not "later cleanup
ticket"**. Refraction.dev *Strategies for Refactoring Legacy Code*: the cost
of fixing now is ~5× less than reconstructing later. Augment Code *12 Essential
Code Refactoring Techniques* explicitly names "deferred cleanup tickets" as
an anti-pattern.

### Gap

- No empirical study quantifies "horizontal sweep ROI" for solo developers.
  All cited evidence comes from team-scale (5+ devs) contexts.
- The boy-scout / deep-refactor-on-touch rule has no automation. It is purely
  cultural / process — easy to skip under deadline pressure.

---

## 8. Tracking module modernization progress

### Tooling

| Tool        | What it shows                            | Cost     | 2026 status |
|-------------|------------------------------------------|----------|-------------|
| CodeScene   | Hotspots (complex + frequently changed), change coupling, code health per file/module | Paid (SaaS / on-prem) | Production; well-maintained |
| SonarQube   | Per-module debt rating, coverage, duplication | Paid | Production |
| GitHub Insights | Contributor / churn / PR-cycle metrics | Free for OSS | Production |
| `fdaines/spm-go` | Per-package size/complexity metrics for Go | Free, OSS | Maintained but niche |
| `gocover.io` / Codecov dashboard | Coverage per package over time | Free tier | Production |
| Custom Makefile dashboard | Cheap; per-tool aggregation | Free | DIY norm |

Source: [CodeScene Refactoring Targets](https://codescene.com/use-cases/refactoring-targets),
qodo.ai *10 Code Quality Metrics 2026*.

### Metrics that matter (2026 consensus)

From qodo.ai's *10 Code Quality Metrics for Large Engineering Orgs (2026)*,
ranked by signal strength:

1. **Defect density** (bugs per kLOC by module)
2. **Code churn** (LOC changed / week per module)
3. **Cyclomatic complexity** (per function, aggregated by module)
4. **Test effectiveness** (mutation kill rate, NOT line coverage alone)
5. **Static analysis issue density** (lint warnings per kLOC)
6. **Security vulnerability MTTR**
7. **Duplicate code ratio**
8. **Review quality signals** (PR comment count, PR time-in-review)
9. **Hotspot risk score** (CodeScene-style: complexity × churn)
10. **Ownership spread** (how many devs touched the module)

For a solo dev, items 1, 2, 3, 5, 7, 9 are directly observable; items 4 and 6
require active practice; items 8 and 10 are team-only.

### The "module scoreboard" pattern

Pattern from CodeScene + multiple 2026 mid-size shop writeups: **per-module
health card**.

A single Markdown table, regenerated weekly by CI, per-module row, columns =
{LOC, coverage, dupl count, golangci issues, file count > 600 LOC, dead
exports, last-touch-date, hotspot risk}. Stored in repo as `MODULE-HEALTH.md`
or rendered as a dashboard. Tracked over time = velocity of improvement.

### Critical pushback (2026)

> "Code quality metrics work best as **early signals, not scoreboards**."
> — qodo.ai 2026

If the team chases a number, the metric loses meaning (Goodhart's Law).
The 2026 evidence is to use metrics to **trigger investigation**, not to
score modules against each other or against a fixed target.

### Gap

- No standard "Go module health" report generator exists. Shops either pay
  for CodeScene/SonarQube or DIY a Makefile + Markdown generator.
- Aggregating across linters (`golangci-lint`, `dupl`, `deadcode`, coverage)
  into a single per-module score is a manual exercise. Each tool has its
  own output format; SARIF is the emerging interchange but adoption is uneven.

---

## Cross-cutting observations

### 2026 ecosystem stability

- **`golangci-lint v2`** is the consolidation point. Most listed tools (`dupl`,
  `funlen`, `gocognit`, `gocyclo`, `depguard`, `unused`, `errcheck`, `ireturn`,
  `revive`) are bundled. Running them standalone is rarely necessary.
- **`golang.org/x/tools/cmd/deadcode`** is the **only first-party Go-team
  static analyzer for dead code**. It supersedes the older deadcode finders.
- **`go.uber.org/mock`** is the **maintained fork** of `golang/mock` since
  Google deprecated the original.
- **`pgregory.net/rapid`** has displaced `gopter` for property-based testing.

### 2026 contested / weak areas

1. **Reflection-based registry blind spots.** No turn-key tool. DIY string-table
   diff is the norm.
2. **Semantic-level duplication detection.** `dupl`'s token-AST approach is
   still the only production option.
3. **File-size enforcement.** No first-class tool in golangci-lint; community
   plugins (`filen`) exist but aren't widely adopted.
4. **Per-module coverage floors for monorepos.** Codecov supports it via flags
   but the YAML is fiddly; not many open-source projects publish their config.
5. **Internal package SemVer / API stability.** `apidiff` remains in `x/exp`.

### What the 2026 evidence says about solo developers

Most cited evidence (IBM, McKinsey, Moderne, CodeScene) is enterprise-scale.
For a solo dev, the practical extraction is:

- 10–30 % sprint reserve for cleanup is overkill; **per-commit deep refactor
  on touch** (already a CLAUDE.md rule in this repo) is the higher-leverage
  practice.
- Mutation testing is too expensive to run on every commit; once per phase /
  milestone is sufficient.
- Architecture linters (`depguard`) are cheap to set up and pay off immediately
  — no team overhead to negotiate the rules.
- A weekly auto-generated `MODULE-HEALTH.md` is a 1–2 hour build cost and a
  permanent forcing function.

---

## Sources

1. [Go Code Coverage Tracking — OtterWise](https://getotterwise.com/blog/go-code-coverage-tracking-best-practices-cicd)
2. [Your Go Tests Pass, But Do They Actually Test Anything? — DEV.to](https://dev.to/r4mimu/your-go-tests-pass-but-do-they-actually-test-anything-an-introduction-to-mutation-testing-1k9l)
3. [avito-tech/go-mutesting](https://github.com/avito-tech/go-mutesting)
4. [Coverage profiling for integration tests — go.dev/doc/build-cover](https://go.dev/doc/build-cover)
5. [pgregory.net/rapid](https://github.com/flyingmutant/rapid)
6. [Go Fuzzing — go.dev/doc/security/fuzz](https://go.dev/doc/security/fuzz/)
7. [Comprehensive Guide to Property-Based Testing in Go — DZone](https://dzone.com/articles/property-based-testing-guide-go)
8. [max-lines discussion — golangci-lint #2881](https://github.com/golangci/golangci-lint/discussions/2881)
9. [golangci-lint v2 announcement](https://ldez.github.io/blog/2025/03/23/golangci-lint-v2/)
10. [Finding unreachable functions with deadcode — Go blog](https://go.dev/blog/deadcode)
11. [golang.org/x/tools/cmd/deadcode](https://pkg.go.dev/golang.org/x/tools/cmd/deadcode)
12. [arxeiss/deadmono](https://github.com/arxeiss/deadmono)
13. [golangci/dupl](https://github.com/golangci/dupl)
14. [analysis-tools.dev — dupl](https://analysis-tools.dev/tool/dupl)
15. [fe3dback/go-arch-lint](https://github.com/fe3dback/go-arch-lint)
16. [OpenPeeDeeP/depguard](https://github.com/OpenPeeDeeP/depguard)
17. [arch-go/arch-go](https://github.com/arch-go/arch-go)
18. [Revisiting Interface Segregation in Go — Rednafi](https://rednafi.com/go/interface-segregation/)
19. [Reducing technical debt in 2026 — IBM](https://www.ibm.com/think/insights/reduce-technical-debt)
20. [How to Manage Technical Debt in 2026 — Sweep.io](https://www.sweep.io/blog/how-to-manage-technical-debt-in-2026)
21. [Tackling Tech Debt at Scale (Morgan Stanley) — Moderne](https://www.moderne.ai/blog/enterprise-tech-debt-refactoring-at-scale)
22. [STOP hurting yourself by doing big bang modernizations — microservices.io](https://microservices.io/post/architecture/2024/06/27/stop-hurting-yourself-by-doing-big-bang-modernizations.html)
23. [Strategies for Refactoring Legacy Code — Refraction.dev](https://refraction.dev/blog/refactoring-legacy-code-outdated-software)
24. [12 Essential Code Refactoring Techniques — Augment Code](https://www.augmentcode.com/guides/12-essential-code-refactoring-techniques)
25. [10 Code Quality Metrics for Large Engineering Orgs (2026) — Qodo](https://www.qodo.ai/blog/code-quality-metrics-2026/)
26. [Identify Refactoring Targets — CodeScene](https://codescene.com/use-cases/refactoring-targets)
27. [Hotspots — CodeScene docs](https://codescene.io/docs/guides/technical/hotspots.html)
28. [The 5 Levels of Code Coverage — Codecov](https://about.codecov.io/blog/the-5-levels-of-code-coverage-how-to-build-a-testing-culture-in-your-organization/)
29. [Codecov Flags](https://docs.codecov.com/docs/flags)
30. [Mocking en Go: interfaces, Testify et GoMock — Guide 2026](https://benoit.gouthiere.be/blog/interfaces-mocking-go/)
31. [Mastering Mocking in Go: gomock vs Interface-Based Fakes — Leapcell](https://leapcell.io/blog/mastering-mocking-in-go-gomock-vs-interface-based-fakes)
32. [Write Better Go Code: 20 Static Analysis Tools — IN-COM](https://www.in-com.com/blog/write-better-go-code-20-static-analysis-tools-that-catch-bugs-before-you-do/)
33. [fdaines/spm-go — Software Package Metrics for Go](https://github.com/fdaines/spm-go)
34. [Settings — Golangci-lint](https://golangci-lint.run/docs/linters/configuration/)

---

*End of document.*
