# Document-field coverage policy reconciliation

Date: 2026-10-22. Source: `5bb0a91767e3f0e0878b1e5bc90461cbaff33629`;
`internal/docfields` is unchanged since the Task 1b live package measurement.

The coordinating agent ran the official disposable database command:

```sh
wsl bash -lc 'cd /mnt/d/Repo/Aura && bash scripts/coverage_docker.sh'
```

All unit/database consumer suites passed, including `cmd/aura` (176.085 s). The retained native
union `cover_gate.out.filtered` measures **48,423/55,407 = 87.3950944826466%** on owned source,
clearing the 85% aggregate. The only final gate failure was the expected package inventory drift:
`internal/docfields` moved from the pinned **217/932 (23.2833%)** to measured
**392/1,112 (35.2518%)**. A focused rerun against the retained profile reproduced that sole
denominator failure before policy editing.

The existing baseline rule should therefore pin exactly 392 covered / 1,112 total statements.
Its mode remains `baseline`, with the visible 85% target and exact non-regression check. The
aggregate, target-package floors, allowed delegated authorities and drift checks are unchanged.
This records the actual expanded source inventory; it does not classify 35.2518% as meeting 85%.

The `db_integration` build cannot execute the ArcadeDB-backed store. The separate, same-source
package run actually executed `arcadedb_integration` with race detection and measured **90.6%**:

```sh
go test -race -tags arcadedb_integration -coverprofile=/tmp/aura-documents-task1b.cover ./internal/docfields -count=1
```

Its native commands, exact fixture scope and limitations are recorded in
[the Task 1b evidence](2026-10-21-paperless-document-file-types.md). These two tier denominators
are neither concatenated nor averaged. The full database run establishes owned aggregate and
inventory evidence; the live package run establishes the store behavior that this tier cannot
reach. Neither substitutes for the separate native Docker or live ArcadeDB release authorities.

Focused RED command (exit 1, only `docfields` denominator drift):

```sh
python3 scripts/coverage_package_gate.py --profile cover_gate.out.filtered --policy scripts/coverage_package_policy.json --report artifacts/production-readiness/coverage-report.json
```

Policy test and GREEN command results are appended after the measured policy row is reconciled.
