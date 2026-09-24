# MCP binary files: attachments and media land in /workspace

Agreed with the operator on 2026-09-24. Today Aura cannot open a mail attachment or a WhatsApp
media file: it can only forward them. After this change a tool call that returns a file puts the
file in the agent's `/workspace`, where it can open, convert or compute on it. The file is deleted
when the turn ends.

## Decisions

Stated by the operator:

- **The tool brings the file into the workspace in one call.** The agent is told to delete the
  file once it is done with it, and whatever is left is removed when the turn ends.
- **Fork + generic bridge.** Each MCP fork returns its file as standard MCP binary content, and
  Aura's MCP bridge materializes ANY binary content from ANY mounted server. No mail- or
  WhatsApp-specific code goes into Aura.
- **WhatsApp is in scope.** `download_media` returns its file the same way as mail attachments.
- **WhatsApp dependencies are updated** as part of this work.

Assumed during design:

- **One cap for every server:** 25 MiB per file. This is Gmail's attachment limit, and it is far
  below the 768 MiB container budget even after base64 inflation.
- **Returned as a link, not inline.** Both forks return a `resource_link` that Aura reads back with
  `resources/read` on the same session. A client that does not want the bytes pays nothing for
  them.

## Why Aura has to carry the bytes

Measured on 2026-09-24:

- **The box cannot fetch the file itself.** The sandbox egress floor drops RFC1918 and
  `172.18.0.0/16`, so a box cannot reach a sidecar. The PIM fork's `/attachments` endpoint is out
  of the box's reach, and the model holds no bearer for it anyway. The bytes can only travel
  through Aura's own MCP session.
- **Inline base64 costs model context.** With the PIM MCP mounted in Claude Code and a real
  mailbox, `mode: inline` put the base64 into the tool result. The client spilled it to a
  `tool-results/*.txt` file, and the model paid for the text either way.
- **Aura today decodes text only.** `mcp.DecodeToolPayload` (`internal/mcp/result.go`) keeps
  only `TextContent` and drops every other block. The MCP content types already exist in go-sdk
  v1.7.0:
  - `ImageContent`
  - `AudioContent`
  - `EmbeddedResource` with a `ResourceContents` `Blob`
  - `ResourceLink` with `URI`, `Name`, `MIMEType` and `Size`

  Aura already calls `ClientSession.ReadResource`, for view documents in `bridge_views.go`.

## Shape

```
fork tool call ──► CallToolResult { text JSON, resource_link attachment://<id> }
                         │
Aura MountedServer.CallTool (same session, same identity child)
   ├─ decode: text + binary blocks + links
   ├─ resources/read each link  ──► blob bytes (+ MIME, name)
   ▼
bridgedTool.newResult
   ├─ FileSink.Materialize ──► /workspace/mcp-files/<request-id>/<server>/<name>
   │                            registered with the turn's file collector
   └─ model sees text + {"files":[{path, mime_type, size_bytes, sha256}]}  (never base64)

LlmAgent.Run defer ──► collector: rm -rf /workspace/mcp-files/<request-id>
```

## Aura: the generic bridge

### Decode (`internal/mcp/result.go`)

`ToolPayload` gains two fields:

- **`Files []FilePart`**, with `Name`, `MIMEType` and `Data`. It is filled from:
  - `ImageContent`;
  - `AudioContent`;
  - `EmbeddedResource` whose contents carry a `Blob`, or `Text` (text is kept as UTF-8 bytes).
- **`Links []*sdkmcp.ResourceLink`.**

Text handling does not change. `DecodeToolPayload` stays the one decode site in the tree.

### Link resolution (`internal/agent/mcptools/bridge_links.go`, new)

`MountedServer.CallTool` (`bridge_supervisor.go`, 509 lines, so the logic lives in its own file)
resolves `payload.Links` before it returns, on the **session that made the call**. For an identity-pooled server that is the identity's child session, so a tenant-scoped
resource is read as the same tenant.

- **How a link is read.** Each link goes through `session.ReadResource` wrapped in
  `mcp.BoundedCall`, the pattern `readViewDocument` uses. Its contents become `FilePart`s.
- **Which MIME type wins.**
  1. The contents' MIME type, unless it is empty or `application/octet-stream`.
  2. Otherwise the link's MIME type.
  3. Otherwise `application/octet-stream`.

  The Python SDK fixes a template's MIME type for every read, so the link is where a server
  states the real type.
- **Which name is used.** The link's `Name`, otherwise the last path segment of the URI.
- **Oversized links are not read.** A link whose `Size` exceeds the cap is never fetched.
- **Failures become text.** A read that fails, or contents that exceed the cap, turn into a line
  in the text that names the link and the reason. The call itself does not fail, because the
  text part of the result is still valid.
- **No fetching by URL.** Aura never fetches a link URI over HTTP. The only read path is
  `resources/read` on the server's own session, so a hostile link cannot make Aura reach anything
  on the network.

### Materialize (`internal/agent/tools/mcp_files.go`, new)

`FileSink` is an interface declared in `mcptools` and implemented in `tools` beside
`document_open`, so `mcptools` does not import the sandbox package. It is injected through a new
field, `MountOptions.Files`, on all three mount paths:

- boot;
- live mount;
- stdio `MountServer`.

For each `FilePart`, `Materialize`:

1. Checks the file against the caps: 25 MiB per file and 50 MiB per call.
2. Calls `Router.Route(ctx)`. If the box is unreachable it denies and falls back to text, the
   same way `document_open` answers `sandbox_unavailable`.
3. Writes the file to `/workspace/mcp-files/<request-id>/<server>/<name>` with
   `WriteFileStream`. `<name>` passes the same rule `document_open` applies to file names. A
   collision gets a `-2`, `-3`… suffix before the extension. A failed write removes its partial
   file, as `document_open.write` does.
4. Registers `/workspace/mcp-files/<request-id>` with the turn's file collector.

It then returns `{path, name, mime_type, size_bytes, sha256}`.

**No collector on the context means nothing is materialized.** Callers without an `LlmAgent.Run`
have no turn to own the file: `toolpipe`, `docs_mcp`, and a readiness check. Writing anyway would
leak the file, so they get the text only, plus a line saying the file was not materialized.

### What the model sees (`bridge_call.go`)

`newResult` appends one JSON block to the text:

```json
{"files":[{"path":"/workspace/mcp-files/…/aura-pim/invoice.pdf","mime_type":"application/pdf","size_bytes":81234,"sha256":"…"}],
 "note":"Delete each file (rm) once you are done with it. Whatever is left is deleted when this turn ends; copy a file elsewhere in /workspace to keep it."}
```

The bytes never reach the model, the idempotency ledger or the transcript. A result replayed from
the ledger carries paths from the turn that produced it, and those paths are gone. The `path` names
its request ID, so the model can tell, and a fresh call fetches the file again.

### Turn-end cleanup (`internal/agent/llm_agent_files.go`, new)

- **Where it runs.** `LlmAgent.Run` installs a file collector on `turnCtx`, beside
  `tools.WithRequestID`. `llm_agent.go` is 591 lines, so `Run` gains only the two calls and the
  drain lives in the new file. The outermost `defer` drains it: for each registered `(box handle,
  directory)` it runs `rm -rf --` on a `context.WithoutCancel` context with a short timeout.
- **Only if something was written.** A turn that wrote nothing makes no box call at all.
- **Swarm workers are covered.** Every worker is its own `LlmAgent.Run`, so each gets its own
  collector.
- **Why not `hooks.OnTurnEnd`.** The runner wires that hook for the root agent only, so a worker
  never gets it and a worker's files would leak.
- **What this costs.** A file a worker downloads is gone when the worker's turn ends. The worker
  is expected to process it itself.
- **An `ask_user` pause also ends the turn and deletes the files.** After the answer, the resumed
  turn fetches again. Keeping the files across a pause would leak them whenever the question is
  never answered.

## PIM fork (`aura-pim-mcp`)

- **Result of `get_email_attachment`.** The curated action forwards to upstream with
  `mode: "stash"` and returns a `CallToolResult` with two blocks:
  - the upstream JSON, carrying `attachmentId`, `name`, `contentType` and `size`;
  - a `ResourceLinkBlock` with `attachment://<attachmentId>`, the name, the MIME type and the
    size.

  The `mode` parameter leaves the curated schema, and `inline` goes with it. The
  `attachmentId` stays usable by `send_email`.
- **Return type of `Calendar`.** It becomes `Task<CallToolResult>`. Every other action returns
  its JSON as a single text block, which is what the client receives today.
- **The `attachment://{attachmentId}` resource template.** It is registered on both servers:
  - It binds the tenant from `RequestContext.User`. On stdio that is the local principal the
    incoming message filter sets for every message.
  - It reads with `TryRead`, which does not consume, so Aura's read does not spend the handle
    `send_email` may still need.
  - It answers with `BlobResourceContents.FromBytes`.
  - An unknown or expired ID gets `attachment expired or unknown; call get_email_attachment
    again`.

  The store is tenant-scoped, so tenant B reading tenant A's ID gets the same error.
- **MIME fallback.** The stored content type is used unless it is empty or
  `application/octet-stream`; otherwise the type comes from `MimeKit.MimeTypes.GetMimeType(name)`.
- **Store cap.** The per-item limit goes from 10 MiB to 25 MiB. The 100 MiB total and the
  15-minute TTL are unchanged.
- **Prerequisite.** The 1.8.3 merge branch (`aura/upstream-1.8.3`, six follow-up commits on top of
  the merge, local only) ships first. It carries the EventRef, scope, local-file and stdio fixes.

## WhatsApp fork (`whatsapp-mcp`)

### `download_media`

- **Result.** It returns a `CallToolResult` carrying two blocks:
  - JSON text `{success, message, name, mime_type, size_bytes, file_path}`. `file_path` stays so
    that `send_file` can forward received media.
  - A `ResourceLink` to `whatsapp-media://{chat_jid}/{message_id}`.
- **Annotation.** The annotation becomes `-> CallToolResult`. On the locked `mcp` 2.0.0 an
  `ImageContent` or `list[...]` annotation would copy the base64 into `structuredContent`.
- **The resource template.** It is registered with `mime_type="application/octet-stream"`,
  because the Python SDK fixes a template's MIME type for every read; the link carries the real
  type.
  - The handler resolves the file through the same bridge download path the tool uses.
  - It refuses anything over 25 MiB.
  - The URI carries no path and no tenant: the Python process can read every tenant's store, so
    a path in the URI would be a cross-tenant read.
- **Tenant binding.** `SubjectTenantMiddleware` binds the tenant for `resources/read` as well as
  `tools/call`. Today it skips every other method (`tenant_context.py:78`), and a resource read
  would fail with `tenant context is not active`.
- **MIME type and name.** The bridge stores no MIME type, saves every image as `.jpg` even when it
  is a PNG, and drops a document's original name when it writes the file.
  - A document takes its name from `messages.filename`, and its type from that name.
  - Any other media type is sniffed from magic bytes (JPEG, PNG, WebP, GIF, PDF, OGG, MP4) and
    named `<type>_<message_id><ext>`.
  - Anything unrecognised is `application/octet-stream`.
- **Timeout.** The download `requests.post` gets a 70 s timeout. Today it has none; 70 s is above
  the gateway's 65 s.

`get_media_data` does not change. Its name, arguments, `dict` shape and the
`whatsapp_actions.download_media` hook are the MCP Apps view's contract (`ui/client.html`).

### Dependencies (measured 2026-09-24)

| Where | Today | Action |
|---|---|---|
| `whatsmeow` | `v0.0.0-20260622185415` | latest (`20260921121126` at audit), bridge tests + a live pairing check |
| `x/crypto`, `x/net`, `go-sqlite3`, `protobuf`, `go.mau.fi/util` | 0.53 / 0.56 / 1.14.45 / 1.36.11 / 0.9.10 | latest |
| Go toolchain | image `golang:bookworm` (floating); `govulncheck` finds 9 reachable stdlib CVEs at go1.26.3, incl. GO-2026-4970 (`os.Root` symlink escape in the media read/write paths) | pin a patched `golang:1.26.x-bookworm` and a `toolchain` line; `govulncheck` must report 0 reachable |
| `anyio` | 4.13, capped `<4.14` | drop the cap: it came from dependabot #49 ("permit the latest version"), not a compatibility need; ≥ 4.14.2 (CVE-2026-63374/64847) |
| `cryptography` | 48.0.1 | ≥ 50, the release that fixes all of PYSEC-2026-3552/3553/3554 |
| `httpx2` | 2.10.0 | ≥ 2.12 (PYSEC-2026-3846/3848/3849) |
| `mcp` / `mcp-types` | 2.0.0 | 2.2.0, and check the structured-output change 2.1 made (`_returns_content`) against the view |
| `uv` in Dockerfile | 0.5.31 | current |

The `pip-audit` versions differ from `uv tree`: the export resolved a different set. The
implementation re-runs the audit on the lock file it commits, and the target is 0 findings.

CI changes:

- **Test the locked set.** CI installs Python dependencies unlocked (`uv pip install -e`), so it
  never tests what ships. It moves to `uv sync --frozen --extra dev`.
- **Run the Go tests.** CI runs no `go test` today. It gains a `go test ./...` step for the
  bridge.

## Errors

| Situation | What the model sees |
|---|---|
| Box unreachable | the text, plus `file not materialized: sandbox unavailable` |
| File over 25 MiB, or the call over 50 MiB | the text, plus `not materialized: <size> exceeds <cap>` |
| Link read fails or has expired | the text, plus `not materialized: <reason>`; the model calls again |
| Caller has no turn (no collector) | the text, plus `not materialized: no agent turn owns the file` |
| Box write fails mid-stream | the partial file is removed; a plain tool error, not `sandbox_unavailable` |
| Turn-end `rm` fails | logged with request ID and directory; the next turn is unaffected |

## Testing

**Aura, unit (≥ 85% on every touched package):**

- **Decode.** A table test over every content type: image, audio, embedded blob, embedded text,
  link, and mixed blocks.
- **Link resolution.** Runs against an in-process go-sdk server over the SDK's in-memory
  transport, and covers:
  - the read goes out on the calling session;
  - a `Size` over the cap is never read;
  - a failed read becomes text;
  - the MIME precedence rule.
- **Sink.** Covers:
  - path building and name sanitization, with `../`, an empty name and collisions;
  - the per-file and per-call caps;
  - no collector means no write;
  - a fake router records the write and the partial-file removal.
- **Collector.** Runs through `LlmAgent.Run` with a fake router and covers:
  - it drains on a normal end, on `ask_user_pause` and on a panic;
  - a turn that wrote nothing makes no box call.
- **Bytes stay out.** The model-visible text, the ledger entry and the transcript never contain
  the base64.

**Aura, `docker_integration`:** a real box. The file exists under
`/workspace/mcp-files/<request-id>/…` during the turn and is gone after it.

**PIM fork (MSTest):**

- `get_email_attachment` returns the link with its MIME type and size.
- The template reads it back under tenant A.
- Tenant B and an expired ID get the error.
- `TryRead` does not consume, so `send_email` can still use the `attachmentId` afterwards.
- MIME fallback.
- The stdio server with the local principal.

**WhatsApp fork (pytest, locked `mcp`):**

- `download_media` returns the link.
- `resources/read` works with the tenant middleware.
- A cross-tenant read fails.
- The MIME sniff table and the document name.
- The size cap.
- `get_media_data` keeps its shape.

**E2E on the lab VM** (Definition of Done > 9.8, real agent, real accounts, read-only):

1. Release both fork images and Aura through the updater. Every step that publishes or rolls out
   waits for the operator's go.
2. Mail: ask Aura for a real email's PDF attachment and a fact that needs the file itself, such as
   its page count. Check that the file exists during the turn, that the answer is right, and that
   the file is gone after the turn.
3. WhatsApp: the same with a received image and a received document.
4. Delete every personal copy that the test produced.

## Out of scope

- **The upload direction:** binary arguments from Aura to an MCP server. `send_email` keeps
  `attachmentId` and base64.
- **Chunked or streamed resources.** The 25 MiB cap stands in for them.
- **Showing the files in the cockpit.**
- **Keeping a file past its turn.** The agent copies it within `/workspace` if it needs it.

## PRD

No PRD amendment is written before the E2E runs. Once it has, the amendment records:

- what was measured: sizes, timings, cleanup;
- what the measurement does not prove, including:
  - servers other than the two forks;
  - files near the cap on the appliance's memory.

