# Handoff — 2026-08-08, document plane

Merged to `master` as `61c2db0e5` (29 commits). Supersedes
`2026-08-07-document-pipeline-rewrite-handoff.md` and `2026-08-07-next-session-prompt.md`.

## What the plane is now

A file manager over **per-identity Garage buckets**, reconciled into ArcadeDB by the
CocoIndex sidecar. The Go pipeline, its catalog HTTP surface and Docling are gone.

The cockpit's document surface is SVAR React File Manager (MIT) driven by its **own**
`RestDataProvider`, against a mount that speaks the component's REST dialect verb for verb
— taken from its official Go reference backend and confirmed against the live demo. There
is no request-building code of ours between the widget and the API, which is the point: the
menu is the stock one because the backend implements the whole contract.

## Proven live, not by unit test

Against the running stack with the operator's session:

- create folder, upload, list, open inline, rename, copy, move a folder, delete, and the
  root back to its original contents
- internal storage refused: 404 on read, 400 on delete and upload
- `Zephyr Report.docx` uploaded, extracted, carded, indexed — 1 passage
- `foto.png` indexed with 0 passages and an honest card ("image, 69 bytes. Aura did not
  look at the image"), **zero** ParseErrors in the log
- a bucket minted on first access (`aura-b130c94d-…`), carrying only that identity's key
- 26 identities → 2, via Aura's own de-provisioning saga

## Five defects found by driving it, and what each taught

**1. `/api/filemanager/*` 404'd with an HTML body.** The routes existed on the AG-UI mux
but not the parent one, which falls anything it does not recognise through to the SPA. The
component reported "Unexpected non-whitespace character after JSON". Unauthenticated the
path still returned 401, which is why the auth layer looked fine and the fault was one
layer further in. *Every backend route needs registering on BOTH muxes; the subtree test
now covers all seven verbs.*

**2. `/identity` was listed as an ordinary folder — with Rename and Delete on it.** That is
the asset service's tree: every chat attachment and share artifact. Deleting it would have
taken every attachment in the account. *A listing shows a bucket ROOT; anything in the
bucket that is not the user's must be hidden AND write-refused, at every seam, because
hiding alone leaves it reachable by typing the path.*

**3. `default-src 'none'` on the inline CSP blocked documents from rendering themselves.**
`sandbox` alone is the control — opaque origin, no scripts, no session access. *Safe but
unable to render is not safe, it is broken.*

**4. 26 identities shared one bucket with one credential.** `resolveObjects` caught
"not provisioned" and returned the shared bucket, overriding the layer beneath it, which
says in as many words: *"an identity without its own provisioned key must resolve to
nothing (fail closed), never to the shared or another identity's bucket (F-007)"*. The
`identity/<id>/` prefix that had been separating them is a naming convention, not a
boundary — and the file manager, which lists a root, is what turned that into exposure.
*Measured, not inferred: 26 rows in `aura.identities`, 0 in `aura.identity_object_store`.
The comment said bucket-per-identity; the table said otherwise. Read the table.*

**5. Deleting a user was structurally impossible.** `buildArcadeMemoryPurger` returns nil
without a server credential, and the saga refuses every deletion when it is nil — correctly,
since dropping the Postgres row while the tenant database survived would orphan it. Compose
gave `ARCADEDB_ADMIN_*` to `arcadedb-mcp` and not to `aura`. The block withholding it read
"no root/admin credential enters Aura", which sounds like a containment boundary and is not
one: `AURA_ARCADEDB_TENANT_SECRET` two lines above derives EVERY tenant's credential. Root
adds `DROP DATABASE`, not reach. *A comment promising a boundary the code does not have is
worse than no comment: it cost the ability to remove a user.*

## The pipeline's own two fixes

**Aura's storage is excluded from the sweep.** `AURA_INGEST_S3_PREFIX` defaults to empty, so
the sidecar walked the whole bucket including `identity/…/asset/…/original`. Every chat
attachment was indexed a SECOND time under a second `search_document_id` — one document
twice in one search, 8 rows for 1 uploaded file — and those objects have no extension, so
Tika threw on each. Fixed with CocoIndex's own `PatternFilePathMatcher(excluded_patterns=…)`.
Measured after: 8 rows → 1.

**Binaries never reach the extractor.** Tika raises on a format it cannot parse and the
raise killed the whole component, so one photo stopped that file being indexed at all.
`extract.extractable()` decides by family before the call — the same shape the library's own
image and audio examples use. A non-textual file still gets a row: name-searchable,
card-described, openable.

## Open, in the order I would take them

1. **A chat attachment shows as `<assetID>.pdf`.** The key carries the extension and
   deliberately not the name (a key travels into presigned URLs and access logs), and the
   reconciler names a record from its key. The real name is on the asset row; nothing
   carries it into the index yet. S3 object metadata is the obvious channel.
2. **`internal/documents` catalog store/service + the drop migration.** ~4.5k LOC. Live
   consumers: `document_version_recorder.go`, `assets/store.go`, the `aura docs` CLI,
   `agui/storage_orphans_service.go`. The migration slot is whatever `ls
   internal/db/migrations/ | tail -1` says at the time — never a number from a document.
3. **`retrieval_rank.go`, 269 LOC.** Fuses lexical and dense with an invented heuristic
   (`lexical → 3+score`, `dense → 2+closeness`) while ArcadeDB ships `vector.rrf`,
   `hybridscore`, `fuse`, `mmr`, `rerank`. Same class as the deleted `internal/rerank`.
4. **filecard: 442 LOC in `xlsx.go` + 270 in `ooxml.go`** against excelize (BSD-3).
   `pdf.go`/`pdf_text.go` stay — they shell out to poppler, which is already the industrial
   choice.
5. **Images and audio as their own families.** CocoIndex's `live-image-search` (CLIP into a
   shared text/image space) and `audio-to-text` (`LiteLLMTranscriber`) are additions to
   `process_file`, not rewrites. Aura already has vision and cloud STT configured.

## Environment notes worth carrying

- `aura-ingest` is behind `profiles: [ingest]`; a bare `compose up -d` never starts it. Its
  `AURA_INGEST_S3_*` now point at the identity's own bucket with a dedicated **read-only**
  Garage key.
- The 9 objects that predate bucket-per-identity were copied into the new bucket; the
  originals remain in `aura-assets`, so the migration is reversible. `aura.assets` rows for
  the old bucket were deleted.
- `TestStageBoxArtifact_ExtractsRegularFile` fails on Windows only (0600 vs 0666) and passes
  under WSL. Everything else in `./internal/... ./cmd/...` is green.
- The Claude Code classifier blocks destructive Postgres writes regardless of the
  `permissions.allow` list — they are a separate layer. Destructive SQL has to be run by a
  human.
