# Mutation cache: would a test-binary fingerprint reuse more, soundly?

Date: 2026-11-26. An offline measurement in WSL (Linux, Go 1.27.4, go-mutesting at the CI pin
`v0.0.0-20251226130216-48d0401f00fb`). No mutant ran: the measurement compares fingerprints,
not scores. Window: the last 40 first-parent commits of `master` up to `659413383`,
from `657da290f` (2026-11-25 15:17) to `659413383` (2026-11-26 16:08). That gives 39
consecutive pairs, and 19 of the 40 commits touch `*.go`. Nothing in `scripts/` or `.github/`
changed.

## Method

- **Clone.** A WSL-local clone, `~/mutspike`, with `git checkout -f` and `git clean -fdx` per
  commit. GOCACHE and GOMODCACHE were shared, and GOFLAGS was empty, as in the `go-mutation`
  job: the job sets no GOFLAGS or tags, and go-mutesting runs a bare
  `go test -timeout <n>s <pkg>` (`cmd/go-mutesting/main.go:467` at the pin).
- **Fingerprint A** is the scheme in use. It calls `go_mutation_cache.fingerprint`, using one
  copy of `scripts/go_mutation_cache.py` taken from `659413383` for every checkout. It is
  computed as `measurer()` computes it: `go list -deps -test -json` per scope,
  `go_toolchain()` with the go-mutesting build, and the `internal/agui` exec-timeout key.
  Neither script changed inside the window.
- **Fingerprint B** is the candidate. It is the sha256 of four things: the
  `go test -c -trimpath -buildvcs=false` binary of the scope's package, the mutated file's
  bytes, every `testdata/` file of the closure (`closure_files` filtered to `testdata`), and
  the same toolchain dict, including the slow-suite timeout.
- **Roster.** The roster is the 23 Go scopes of `GO_SCOPES` in 16 packages; every scope file
  exists at the oldest commit. The brief's "27 scopes" does not match this roster. With the
  two frontend scopes, `REQUIRED_SCOPE_IDS` has 25.
- **Counterfactual C** came out of the soundness check below. It is A's inputs without the
  `_test.go` files of closure packages other than the scope's own. It was computed offline
  from the saved closures, with no new tool.

The raw data has one row per commit and scope: both fingerprints, the binary sha, the
timings and the input count. It is in `~/mutspike-data.csv`, inside the WSL home that holds
the clone. The closure listings, one gzip per commit, are in `~/mutspike-closures/`.

## Reproducibility

The 16 test binaries of `659413383` were built in two checkouts at different paths, the
second with an empty GOCACHE. **All 16 sha256 values were identical.**

- **Path.** Without `-trimpath`, the same package from the two paths differed, in its build
  ID and in its bytes. `go help build` describes `-trimpath` as "remove all file system paths
  from the resulting executable".
- **VCS stamping played no part.** The binary built with `-buildvcs=false` is byte-identical
  to the one built without it. `go version -m | grep vcs` finds no line in the default build.
  In the `-buildvcs=false` build it finds only the flag itself. Test binaries are not
  stamped, so the flag is redundant but harmless.
- **Perimeter.** One host, one C compiler, `CGO_ENABLED=1`. Whether WSL and a runner image
  produce the same binary was not measured, and the scheme does not need it: each fingerprint
  is only compared with fingerprints from the same runner image.
- **The build ID stays in.** It is `actionID(binary)/actionID(main.a)/contentID(main.a)/contentID(binary)`
  (`cmd/go/internal/work/buildid.go`). The link action ID hashes a
  `packagefile <path>=<contentID>` line for every archive it links
  (`cmd/go/internal/work/exec.go:1546-1580`, `linkActionID`).
- **A probe showed why that matters.** An unused function was appended to a dependency of
  `internal/gateway`. The two binaries differed in only 40 bytes: the 20 characters of the
  first Go build-ID component, and the 20-byte `.note.gnu.build-id`. `readelf -n` shows both
  notes differing. The linker had dropped the function itself. A recipe that strips the build ID (`-ldflags=-buildid=`) would therefore
  probably turn a dead-code-only change into a match. That was inferred, not built.
- **More probes on the same dependency:**
  - A comment edited without a line shift: binary unchanged.
  - A test added to its `_test.go`: binary unchanged.
  - A line added above the code: binary changed.
  - A changed expression in a live function: binary changed.
- **The incremental builds were reproducible too.** Over the 39 pairs, B never changed
  where A stayed equal (0 pairs).

## Reuse over 39 pairs

| Scope | Package | A | B | C |
|---|---|---|---|---|
| turn_reading | internal/agent | 24 | 31 | 29 |
| elicitation_route | internal/agent/mcptools | 31 | 34 | 33 |
| task_approval | internal/agent/tools | 31 | 31 | 31 |
| task_message | internal/agent/tools | 31 | 31 | 31 |
| signin_proxy | internal/agui | 24 | 31 | 29 |
| pim_account_relay | internal/agui | 24 | 31 | 29 |
| pim_secret_fingerprint | internal/agui | 24 | 31 | 29 |
| message_fire | internal/cron/handlers | 24 | 31 | 29 |
| gateway | internal/gateway | 27 | 31 | 30 |
| lapsed_approval | internal/cron | 39 | 39 | 39 |
| profile_validation | internal/config | 38 | 38 | 38 |
| sandbox | internal/sandbox/usersandbox | 38 | 38 | 38 |
| decide_floor, decide_tick, mail_repair | internal/pimwatch | 38 each | 38 each | 38 each |
| decide_wire, identity_isolation, ssrf, pausable | their own | 39 each | 39 each | 39 each |
| media_clamp, media_watcher | internal/mediagen | 39 each | 39 each | 39 each |
| signin_flow, signin_options | internal/browserlogin | 39 each | 39 each | 39 each |
| **Total** | | **781/897 (87.1%)** | **823/897 (91.8%)** | **811/897 (90.4%)** |

The same counts by CI group, where "whole group" counts the pairs in which no scope of the
group would run a mutant:

| Group | Scopes | Scope pairs A / B | Whole group A / B |
|---|---|---|---|
| turn-reading | 5 | 149 / 163 of 195 | 24 / 28 of 39 |
| elicitation-boundaries | 13 | 452 / 473 of 507 | 24 / 30 of 39 |
| media | 5 | 180 / 187 of 195 | 24 / 31 of 39 |

## What B would add, and whether it is sound

B adds 42 scope reuses, from 7 pairs. None of the pairs runs the other way: A never stayed
equal where B changed. Every added reuse falls on the expensive scopes: the three agui scopes
(signin_proxy measured 3625 s cold, ci.yml), turn_reading (1012.9 s), message_fire,
elicitation_route and gateway.

| Pair | What changed in the closure | Scopes | Could a mutant outcome differ? |
|---|---|---|---|
| `7547cf730→f85f17b10` | `internal/messagedrafts/review_test.go` | gateway, the 3 agui, turn_reading, message_fire | No. A dependency's test file is never compiled into another package's test binary (`go help test`), and no test of these packages reads it. |
| `47e07d81a→a93b7ac3d` | `internal/gateway/scope_test.go` | the 3 agui, turn_reading, message_fire | No, the same reason. The gateway scope itself re-measures under both A and B. |
| `267546121→7ddd0f8a6` | `internal/agent/tools/search_gate_test.go` | gateway, elicitation_route, the 3 agui, turn_reading, message_fire | No, the same reason. |
| `aa39f6d2b→782074187` | `internal/agent/display/sidecars_test.go` | the 3 agui, turn_reading, message_fire | No, the same reason. |
| `72e2caa7b→6a4a81889` | `internal/agent/tools/task_received_test.go` | gateway, elicitation_route, the 3 agui, turn_reading, message_fire | No, the same reason. |
| `e4e428917→72e2caa7b` | `task_received.go`: the literal U+2028/U+2029 runes rewritten as `' '`/`' '`, plus its test | the same 7 | Not here: the rune constants are equal and the archives are identical. It is the class with the hole, below. |
| `6a4a81889→a492dc6cd` | `internal/agent/display/mcp_source.go`: a doc comment rewrapped, 3 lines to 3 lines | the 3 agui, turn_reading, message_fire | Not here: no test of these packages reads that file's text. It is the class with the hole, below. |

**The hole: a test that reads Go source text.** B sees compiled output. Some unit tests of
the scope packages read source as text at runtime, so a comment edit can change their verdict
and leave B unchanged. This was shown on the clone:

1. The probe appended "(not telegram)" to the first comment line of
   `internal/agent/tools/send_file.go`, with no line shift.
2. The `internal/agent/tools` test binary kept its sha256.
3. `TestSendFileChannelAgnostic` (`send_file_test.go:47`, which reads `send_file.go`) went
   from `ok` to `FAIL`.

The harmful direction is the reverse:

1. An entry is measured while that test fails. Every mutant of `task_approval.go` and
   `task_message.go` is then killed. go-mutesting counts exit code 1 as killed
   (`main.go`, the `execExitCode` switch), and these groups run no baseline.
2. A later comment-only fix turns the test green and leaves B unchanged.
3. B reuses the inflated score.

That sequence is argued from go-mutesting's exit-code handling; it was not run. A catches
the edit, because `send_file.go` is one of its inputs. The same pattern appears in two more places:

- `internal/agui/mutation_coverage_test.go:80-88` regex-scans every non-test `.go` file of
  agui (three scopes).
- `internal/agent/turn_recall_eval_set_test.go:106` reads the text of the `prompt/*.go`
  dependency.

Closing the hole means hashing the source text of the closure's Go files. B then becomes C
plus a binary hash, and the binary hash adds nothing. The 12 reuses that only B finds (7 in
one pair, 5 in the other) are exactly the source edits that change text without changing the
compiled output. **So the sound form of B reuses what C reuses: 811/897.** C's 30 extra reuses
come from the five dependency-test-file pairs.

C is not new. The script's header records that narrowing test files "moved no scope's reuse
rate by more than two points" over 263 pushes. In this window it moved 5 scopes by 5/39
(12.8 points) and the total by 3.3 points. The two figures use different units. These are
consecutive commits, while CI compares each push with the last cached entry. A push that
bundles a test-only commit with a code commit re-measures under A, B and C alike, so
per-commit pairs overstate the reuse of all three, and they overstate C's gain most, since
that gain lives in test-only commits.

## Cost

B costs one `go test -c` per distinct package of the group, before the first mutant. The
measurement was taken at `659413383`, pinned to 4 CPUs (`taskset -c 0-3`; an `ubuntu-latest`
runner has 4 vCPU), with each group's packages built in order:

| Group | Packages | Cold GOCACHE | Warm (link only) | `go list` (A pays it too) |
|---|---|---|---|---|
| turn-reading | 4 | 172.9 s | 16.3 s | 4.4 to 5.8 s |
| elicitation-boundaries | 10 | 153.2 s | 23.5 s | 7.4 to 8.0 s |
| media | 3 | 173.8 s | 7.9 s | 1.9 to 3.3 s |

A's own cost per scope is the `go list` plus at most 0.16 s of hashing (median over the
window). In the commit loop, where each commit's build ran after its parent's in one shared
cache, the median B build was 0.8 to 5.8 s per package and the maximum 26.5 s (agui).

**The cold cost is not shared with the mutation run.** A cold `-trimpath` build of
`internal/mediagen` took 78.2 s. The plain `go test -c` that followed, which is what a mutant
builds, took another 86.8 s on the same cache, because `-trimpath` changes every action ID.
Which of the two columns CI pays depends on what `setup-go`'s restored GOCACHE holds, and
that cannot be told from here.

## Verdict

- **Reproducibility: VALIDATED.** With `-trimpath`, the test binary is byte-identical across
  checkout paths and cache states, and the build ID must stay in it.
- **B as a sound replacement for A: INVALIDATED.**
  - B misses edits to source text that a test reads, as `TestSendFileChannelAgnostic` showed.
  - Closing that hole reduces B to C.
  - B also costs up to about 3 minutes of cold compile per group, which the mutation run
    does not reuse.
- **C (A minus the dependencies' `_test.go`)** reused 811/897 against A's 781/897 here, at no
  build cost. It is a separate candidate. It has to be measured per push, the unit of the
  header's 263-push figure, before any spec relies on it.

## What this does NOT prove

- **Runtime-read files outside `testdata/`.** Neither A nor B sees them:
  - `turn_recall_eval_set_test.go:132` reads `docs/verification/turn-recall-frozen-eval.md`.
  - `backup_shutdown_test.go:67` reads `deploy/aura-scheduler.service`.
  - A test that reads a dependency's `_test.go` would break C. None was found by grep in
    the 16 packages, which is not proof that none exists.
- **Embedded files.** B covers them inside the binary, and A lists `EmbedFiles`. A file
  embedded and also read from disk was not looked for.
- **Tests that read env or the network.** They are outside both fingerprints. A
  `CI`-dependent skip or a live endpoint can change a mutant's outcome between runs on
  identical inputs.
- **go test runs vet.** B does not model vet, which `go help test` lists as: atomic, bools,
  buildtag, directive, errorsas, ifaceassert, nilfunc, printf, stdversion, stringintconv,
  tests. A comment edit to a `//go:` directive in the scope package can change vet's verdict
  without changing the binary.
- **Trimpath versus the real mutant build.** B is built with `-trimpath`, while the mutants
  build without it. An identical B is taken to mean an identical untrimmed binary at CI's
  fixed checkout path. That was not measured.
- **Whether the mutant binary is identical.** B hashes the unmutated binary. That the mutated
  binaries are equal too is argued, not measured: it holds when every other archive's content
  ID is equal, which the build ID pins.
- **Cross-group effects.** Every group was costed on its own runner model. A run where
  sibling groups write the shared cache at once was not modelled. Neither was the entry that
  the master change after this window (`a54afb796`, a cache saved per group) now leaves.
- **Per-push reuse.** The pairs are consecutive commits, not pushes: CI's real reuse for A,
  B or C over the same day is lower, by an amount not measured here.
- **Generality.** These are 39 pairs from one day of work, in which the extra reuses cluster
  in PIM-split follow-ups and test-only commits. The timings come from WSL on a 16-core host
  pinned to 4 CPUs, not from a runner. No scores were run, so "a reuse would be correct" is
  argued from the build model, never compared against a measured score.
