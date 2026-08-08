# Handoff — 2026-08-08, document plane

`master` at `77e12fc50`. Supersedes `2026-08-07-document-pipeline-rewrite-handoff.md` and
`2026-08-07-next-session-prompt.md`, and carries forward the still-open items from the
`.superpowers/sdd/2026-08-06-document-pipeline-rewrite` ledger.

**Production works; CI does not.** The two are separate and the priority is in that order — a
green suite over a broken feature is a test that lies. What is red is my own e2e spec, not the
product. See "Red right now".

## What the plane is

A file manager over **per-identity Garage buckets**, reconciled into ArcadeDB by the CocoIndex
sidecar. The Go pipeline, its catalog HTTP surface and Docling are gone.

The cockpit's document surface is SVAR React File Manager (MIT) driven by its **own**
`RestDataProvider` against a mount that speaks the component's REST dialect verb for verb,
taken from its official Go reference backend. No request-building code of ours sits between
the widget and the API; the menu is the stock one because the backend implements the whole
contract.

## Proven live, driven not asserted

Against the running stack with the operator's session: create folder, upload, list, open
inline (a text file renders "ciao dal file manager" in a real tab), rename, copy, move a
folder, delete, and the root back to its original contents. Internal storage refused — 404 on
read, 400 on delete and upload. `Zephyr Report.docx` extracted, carded, indexed (1 passage);
`foto.png` indexed with 0 passages and an honest card, zero ParseErrors. A bucket minted on
first access. 26 identities → 2 via Aura's own de-provisioning saga.

## Red right now

**CI job `Web E2E` fails on `web/e2e/file-manager.spec.ts`** — my spec, not the product. I
corrected two assertions by reading the CI log instead of running Playwright locally against
the live stack, and it is still red. First action next session: read the failing run's log,
then reproduce **locally against the running stack before touching a line**. Rule out that the
product behaves differently under compose than on the dev box before assuming the spec is
wrong.

**Dependabot `npm_and_yarn in /web for nanoid` fails.** A security update on the frontend,
unrelated to this work, untouched.

## Open — this session

1. **A chat attachment shows as `<assetID>.pdf`.** The key deliberately carries the extension
   and not the name (keys travel into presigned URLs and S3 access logs), and the reconciler
   names a record from its key. The real name is on the asset row; nothing carries it into the
   index. S3 object metadata is the obvious channel.
2. **CSRF defence-in-depth thinned.** The file-manager writes no longer gate on Content-Type,
   because `RestDataProvider` sends JSON with no header and the gate 400'd every write. Not
   exploitable: `auth_cookie.go:130` sets `SameSite=Strict` ("CSRF baseline (CWE-352)") so a
   cross-site request carries no session at all. To restore the layer without breaking the
   widget, refuse `application/x-www-form-urlencoded` and `multipart/form-data` on the JSON
   writes — the two enctypes a cross-origin form can send without preflight — while still
   accepting the `text/plain` the provider actually sends. ~5 lines, needs live verification.
3. **`internal/documents` catalog store/service + the drop migration.** ~4.5k LOC. Live
   consumers: `document_version_recorder.go`, `assets/store.go`, the `aura docs` CLI,
   `agui/storage_orphans_service.go`. Migration slot = whatever `ls internal/db/migrations/ |
   tail -1` says at the time, never a number copied from a document.
4. **`retrieval_rank.go`, 269 LOC.** Fuses lexical and dense with an invented heuristic
   (`lexical → 3+score`, `dense → 2+closeness`) while ArcadeDB ships `vector.rrf`,
   `hybridscore`, `fuse`, `mmr`, `rerank`. Same class as the deleted `internal/rerank`.
5. **filecard: 442 LOC in `xlsx.go` + 270 in `ooxml.go`** against excelize (BSD-3).
   `pdf.go`/`pdf_text.go` stay — they shell out to poppler, already the industrial choice.
6. **Images and audio as their own families.** CocoIndex's `live-image-search` (CLIP into a
   shared text/image space) and `audio-to-text` (`LiteLLMTranscriber`) are additions to
   `process_file`, not rewrites. Aura already has vision and cloud STT configured.

## Open — carried from earlier sessions

7. **`arcadedb_integration` runs NOWHERE.** Ten test files carry the tag — the whole LOCOMO
   memory suite, `aura_cypher_integration`, `serve_deprovision_memory_integration`, the
   adaptive graph view — and neither CI, the Makefile nor the coverage scripts execute it. It
   is not even `go vet`-compiled, so it can rot silently. Until it is wired the memory
   substrate is untested in the pipeline. (CLAUDE.md §Coverage gate tag set.)
8. **`docker_integration` feeds no coverage.** The `sandbox-docker-integration` job proves the
   box works and counts zero toward the 85% floor; that split is why the CAP_NET_ADMIN
   cap-assertion bug stayed latent (WR-01).
9. **`scripts/quality_snapshot_gate.sh` calls `python`**, which WSL does not have (python3
   only). Shimmed locally; worth fixing in the script.
10. **Ingest package-name trap.** Tests import `services.ingest.*` but the image COPYs
    `services/ingest` → `/app/ingest`, so the deployed package is `ingest`. Every module must
    use `ingest.` or a relative import, and a new module needs an image rebuild before its
    import resolves.
11. **Deferred minors from the SDD ledger.** `extract.py`'s soffice-exits-0-but-produced-no-
    file branch raises correctly but has no test; `chunk.py` is 155 LOC against a 120
    instruction (600 is the project cap); the chunk **splitting path is unproven** — every
    canary fixture is a single chunk, so the splitter has never been exercised on real
    multi-chunk input.
12. **`TestStageBoxArtifact_ExtractsRegularFile`** fails on Windows only (0600 vs 0666) and
    passes under WSL. Everything else in `./internal/... ./cmd/...` is green.

## Environment notes

- `aura-ingest` is behind `profiles: [ingest]`; a bare `compose up -d` never starts it. Its
  `AURA_INGEST_S3_*` point at the identity's own bucket with a dedicated **read-only** key.
- The 9 objects predating bucket-per-identity were copied into the new bucket; the originals
  remain in `aura-assets`, so the migration is reversible. `aura.assets` rows for the old
  bucket were deleted.
- The Claude Code classifier blocks destructive Postgres writes regardless of the
  `permissions.allow` list — separate layers. Destructive SQL must be run by a human.
- `aura` now receives `ARCADEDB_ADMIN_USER/PASSWORD`; without them the de-provisioning saga
  refuses every identity deletion.

## The lesson this session cost

Three defects shipped and were found by the operator in their browser, each invisible to a
green suite: a 404 that read as an auth failure (routes on one mux, not the parent), a blank
tab (`CSP: sandbox` stops Chrome running its PDF viewer), and every write 400'ing (the widget
sends JSON with no Content-Type; my curl "proof" set the header by hand). Green gates are not
evidence. Drive the real client, in a real browser, before reporting done.
