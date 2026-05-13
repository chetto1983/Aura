# Wave 2.10 — Install Bootstrap (Auto-Download Embedding Model)

**Status:** plan (not implemented)
**Date drafted:** 2026-05-13
**Predecessor:** Wave 2.9 (markitdown sidecar)
**Successor:** Wave 2.10.b (tool index reconciler), Wave 2.11 (React setup wizard)

---

## 1. Goal

Eliminate the only remaining manual step on a fresh `git clone` + `docker compose up`: the **embedding model GGUF file**. Today the operator must out-of-band download `embeddinggemma-300m-Q4_0.gguf` (265 MB) and drop it at `./data/`. After Wave 2.10 the file is fetched automatically on first boot with SHA-256 verification and resumable transfer.

This is the **fresh-install audit blocker** identified 2026-05-13. Everything else (Qdrant, SearXNG, Garage, markitdown sidecar, MCP seed, default skill) already auto-provisions via Compose + `runtimebootstrap.EnsureLayout()`.

---

## 2. Architecture decision — init container, not in-process

Two options were considered:

| Option | Pros | Cons |
|--------|------|------|
| In-process in `cmd/aura` | One less container | Aura starts before llama-embed → embedding queries fail until aura's download completes |
| **Dedicated `aura-init-models` sidecar** | Compose orchestrates: secrets → models → embed → aura. Clean dependency graph mirroring `aura-secrets`. | One more container (tiny, exits fast on cache hit) |

**Choice: dedicated sidecar.** The pattern matches `aura-secrets` (already one-shot init), keeps `cmd/aura` focused on the bot, and gives Compose a clean dependency: `aura-llama-embed` waits for the model file to exist before starting. No race window, no retry storms, no degraded mode during first 30 s.

Compose dependency graph after Wave 2.10:

```
aura-secrets (one-shot, completes) ───────────────────┐
aura-init-models (one-shot, completes) ──┐            │
                                          ▼            │
                                aura-llama-embed       │
                                          │            │
                                          ▼            ▼
                                              aura
```

---

## 3. Module layout

### 3.1 New package `internal/install/`

Reusable download primitives. Used by both the init binary and (later) the dashboard's "re-download" endpoint.

```
internal/install/
  embedding.go        EnsureEmbeddingModel + manifest constants
  download.go         downloadWithResume(url, dest, sha256, progressFn)
  download_test.go    httptest server: full + partial + bad-hash + cache-hit
  progress.go         ProgressEvent {bytes_done, bytes_total, phase}
```

```go
// internal/install/embedding.go
package install

import "context"

const (
    EmbeddingModelFilename = "embeddinggemma-300m-Q4_0.gguf"
    EmbeddingModelURL      = "https://huggingface.co/unsloth/embeddinggemma-300m-GGUF/" +
                             "resolve/main/embeddinggemma-300m-Q4_0.gguf"
    // SHA-256 verified 2026-05-13 against HF's X-Linked-ETag header AND a
    // local re-hash of the downloaded body. Bump when upstream re-uploads.
    EmbeddingModelSHA256 = "edc6015cb15694c27be7d1d33f1bc015db9a358ff51ed524628c027504907ba9"
    EmbeddingModelSize   = 277_852_192 // bytes; for the progress UI
)

func EnsureEmbeddingModel(ctx context.Context, dataDir string, p ProgressFn) error {
    target := filepath.Join(dataDir, EmbeddingModelFilename)
    if err := verifyFileSHA256(target, EmbeddingModelSHA256); err == nil {
        return nil // already present + correct
    }
    return downloadWithResume(ctx, EmbeddingModelURL, target, EmbeddingModelSHA256, p)
}
```

### 3.2 New binary `cmd/aura-init-models/main.go`

Tiny entry point that calls `install.EnsureEmbeddingModel()` and exits. Logs to stderr, no flags for now (everything env-driven).

```go
// cmd/aura-init-models/main.go
package main

import (
    "context"
    "log/slog"
    "os"
    "github.com/aura/aura/internal/install"
)

func main() {
    logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
    dataDir := envOrDefault("AURA_MODELS_DIR", "/models")
    progress := install.NewLogProgressFn(logger, 5*time.Second)
    if err := install.EnsureEmbeddingModel(context.Background(), dataDir, progress); err != nil {
        logger.Error("model fetch failed", "error", err)
        os.Exit(1)
    }
    logger.Info("model ready", "path", filepath.Join(dataDir, install.EmbeddingModelFilename))
}
```

### 3.3 New `docker/init-models/Dockerfile`

```dockerfile
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/aura-init-models ./cmd/aura-init-models

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/aura-init-models /aura-init-models
ENTRYPOINT ["/aura-init-models"]
```

Distroless because this container only does HTTPS GET + SHA256 + file write. No shell needed.

### 3.4 `compose.yaml` changes

```yaml
aura-init-models:
  build:
    context: .
    dockerfile: docker/init-models/Dockerfile
  image: aura-init-models:local
  environment:
    AURA_MODELS_DIR: "/models"
  volumes:
    - ./data:/models   # shared with aura-llama-embed (read-write here, read-only there)
  restart: "no"        # one-shot

aura-llama-embed:
  # ...existing config...
  depends_on:
    aura-init-models:
      condition: service_completed_successfully
```

---

## 4. Download protocol

### 4.1 Resume with Range header

```go
func downloadWithResume(ctx, url, dest, wantSHA string, p ProgressFn) error {
    partial := dest + ".partial"
    var startAt int64
    if info, err := os.Stat(partial); err == nil {
        startAt = info.Size()
    }
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if startAt > 0 {
        req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startAt))
    }
    resp, err := http.DefaultClient.Do(req)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode != 200 && resp.StatusCode != 206 {
        return fmt.Errorf("install: HTTP %d", resp.StatusCode)
    }
    f, err := os.OpenFile(partial, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
    if err != nil { return err }
    defer f.Close()

    // Stream with progress + SHA-256 accumulation.
    total := startAt + resp.ContentLength
    h := sha256.New()
    if startAt > 0 {
        // Re-hash existing partial bytes so the final hash is over the full file.
        if err := rehashPartial(partial, h, startAt); err != nil { return err }
    }
    written := startAt
    buf := make([]byte, 256*1024)
    for {
        n, err := resp.Body.Read(buf)
        if n > 0 {
            if _, werr := io.MultiWriter(f, h).Write(buf[:n]); werr != nil { return werr }
            written += int64(n)
            p.Update(written, total)
        }
        if err == io.EOF { break }
        if err != nil { return err }
    }
    got := hex.EncodeToString(h.Sum(nil))
    if got != wantSHA {
        _ = os.Remove(partial)
        return fmt.Errorf("install: sha256 mismatch want=%s got=%s", wantSHA, got)
    }
    return os.Rename(partial, dest)
}
```

### 4.2 Cache-hit fast path

```go
func verifyFileSHA256(path, want string) error {
    f, err := os.Open(path)
    if err != nil { return err }
    defer f.Close()
    h := sha256.New()
    if _, err := io.Copy(h, f); err != nil { return err }
    if hex.EncodeToString(h.Sum(nil)) != want {
        return errors.New("hash mismatch")
    }
    return nil
}
```

Result: re-running `docker compose up` after first install does a single SHA-256 scan of the 265 MB file (~1 s on SSD) and exits immediately. No network calls.

### 4.3 Progress reporting

`ProgressFn(bytesDone, bytesTotal int64)` invoked roughly every 256 KiB chunk. The init container's `LogProgressFn` rate-limits to one log line every 5 s so the boot logs aren't flooded:

```
INFO model fetch progress  bytes_done=52428800 bytes_total=277852192 percent=18.9
INFO model fetch progress  bytes_done=104857600 bytes_total=277852192 percent=37.7
...
INFO model ready  path=/models/embeddinggemma-300m-Q4_0.gguf
```

Later (Wave 2.11) the React wizard will subscribe to an SSE endpoint backed by the same `ProgressFn` interface for live UI updates.

---

## 5. Makefile target (optional, for offline-prep)

```makefile
EMBEDDING_MODEL_URL := https://huggingface.co/unsloth/embeddinggemma-300m-GGUF/resolve/main/embeddinggemma-300m-Q4_0.gguf
EMBEDDING_MODEL_PATH := data/embeddinggemma-300m-Q4_0.gguf
EMBEDDING_MODEL_SHA := edc6015cb15694c27be7d1d33f1bc015db9a358ff51ed524628c027504907ba9

download-models: $(EMBEDDING_MODEL_PATH)
$(EMBEDDING_MODEL_PATH):
	@mkdir -p data
	curl -fL --retry 3 -o $@.partial "$(EMBEDDING_MODEL_URL)"
	@echo "$(EMBEDDING_MODEL_SHA)  $@.partial" | sha256sum -c -
	mv $@.partial $@
```

Use case: user with flaky home connection runs `make download-models` once on a stable network, then `docker compose up` skips the download. Identical SHA check + filename → init container exits in 1 s.

---

## 6. Test plan

### Unit tests (`internal/install/download_test.go`)

- **Full fresh download** — httptest serves 1 MB body with correct SHA → file written, hash matches.
- **Cache hit** — file already exists with correct SHA → no HTTP request issued.
- **Bad SHA from server** — httptest serves garbage → `.partial` is deleted, error returned, NO `dest` file created.
- **Resume after partial** — write half the file as `.partial`, restart download → server gets `Range: bytes=N-`, final hash matches.
- **HTTP 5xx** — server returns 503 → error surfaced, partial preserved for retry.
- **Context cancel mid-download** — ctx canceled at 50% → no panic, partial preserved.

### Integration test (`cmd/aura-init-models/main_test.go`)

- Spawn the binary with `AURA_MODELS_DIR=/tmp/aura-test/` pointing at httptest server URL (env override `AURA_MODEL_URL` for test injection)
- Expect exit 0, file present, SHA matches

### Live test

After implementation:
```bash
docker compose down -v               # wipe the data volume
rm -f data/embeddinggemma-*.gguf     # ensure model absent
docker compose up                    # observe aura-init-models pull, then llama-embed starts, then aura
```

Expected: `aura-init-models` logs progress for ~30 s, exits 0; `aura-llama-embed` starts immediately after with model present.

---

## 7. Done criteria

Wave 2.10 ships when **all** of:

1. `internal/install/` package: `EnsureEmbeddingModel`, `downloadWithResume`, `verifyFileSHA256`, `ProgressFn` with 6+ unit tests covering the matrix in §6.
2. `cmd/aura-init-models` binary builds, exits 0 on cache hit and on fresh download, exits 1 on SHA mismatch.
3. `docker/init-models/Dockerfile` produces a working distroless image.
4. `compose.yaml` adds the service + dependency on `aura-llama-embed`.
5. `Makefile` has `download-models` target.
6. Live verify: `docker compose down -v && docker compose up` brings the stack up end-to-end with zero manual steps (besides the existing first-run Telegram-token + LLM-credentials wizard).
7. `docs/wave-2.10-install-bootstrap.md` (this file) updated to "Status: shipped" + commit hash.

---

## 8. Out of scope / next waves

**Wave 2.10.b — tool index reconciler.** Hash-based diff between `tools.Registry` and Qdrant `aura_tool_search_v2`, three triggers (boot, fsnotify on `mcp.json`, post-write hooks from skill install/delete + MCP `notifications/tools/list_changed`). See research notes in `feedback_inspect_artifact_visually_not_just_pass_status.md` neighbor doc.

**Wave 2.11 — React setup wizard.** Delete `internal/setup/page.html`, port the form to React routes under `web/src/routes/setup/`, persist `bootstrapped_version` in SQLite `settings` (never as a sentinel file — Claude Code bug #4714 lesson).

**Future model fetches.** The `install` package is generic. When Wave 2.9.5 adds the GLM-OCR sidecar, `EnsureGLMOCRModel()` becomes another two constants + one function call. No new infrastructure.

---

## 9. Estimated effort

- `internal/install/` package + tests: **0.5 day**
- `cmd/aura-init-models` binary: **0.25 day**
- Dockerfile + compose wiring: **0.25 day**
- Makefile target: **0.1 day**
- Live verify: **0.25 day**

Total: **~1.5 days** of focused work.

---

## 10. Open decisions (chiudere prima di iniziare)

1. **Bump policy for `EmbeddingModelSHA256`.** Today the constant is a code change → rebuild → ship. Acceptable? Or do we want a runtime-overridable env var `AURA_EMBEDDING_MODEL_SHA256` for cases where the user mirrors the file themselves? **Reco**: env override allowed, code constant is the default.

2. **What happens if HF goes down at first boot.** Aura today is unusable without the embedding model (no memory search, no tool retrieval). Should `aura-init-models` retry indefinitely with exponential backoff, or fail fast and let the operator retry compose? **Reco**: 5 attempts with exponential backoff (1m → 16m), then exit 1. Compose `restart: "no"` prevents retry storms; operator decides whether to re-run.

3. **Should we mirror the model on the Aura GitHub Releases page as a fallback?** GitHub Releases allows 2 GB per asset, no per-clone download cost. Could be a fallback URL in the constants. **Reco**: not yet — adds maintenance burden and the unsloth HF mirror has been stable. Revisit if HF outage incidents accumulate.
