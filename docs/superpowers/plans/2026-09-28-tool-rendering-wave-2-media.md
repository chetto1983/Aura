# Tool Media Rendering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Play authorized audio, video and images inline in tool results, and group real multi-image results without losing Aura's artifact actions or safe loading behavior.

**Architecture:** Keep `AssetSourceContext` as the only route selector, extend the existing authenticated/share Range stream to a closed audio MIME set, then wrap copied Elements media-player and Tool UI image/gallery presentation around Aura's asset/error hooks. The generation state machine and download/edit controls remain in Aura.

**Tech Stack:** Go `net/http.ServeContent`, Aura asset/share APIs, React 19, TypeScript, Vitest, Playwright, Elements standalone Media player, Tool UI Image/Image gallery source.

**Spec:** `docs/superpowers/specs/2026-09-28-tool-rendering-tool-ui-elements-design.md` (Wave 2). This plan is independently shippable after Wave 1's router contract; MCP results and outbound drafts are separate plans.

## Global Constraints

- Reuse authenticated and public-share `AssetSourceContext` routes. The stream never presigns a provider URL, and download still serves inert `application/octet-stream`.
- No autoplay, including on a reopened thread. Preserve Range seeking and natural video proportions. A failed media load shows `PreviewError`.
- `image/svg+xml` stays download-only. A MIME hint never authorizes execution or overrides stored content type. All media URLs are constructed from the active source tier and validated before a copied component receives them.
- Copy only selected component source with upstream revision/license recorded; keep shipped blue tokens, en/it labels, reduced motion, 44px touch targets, and files under 600 lines.
- Leave unrelated video-studio worktree changes alone. Commit one tested behavior per task with a `Co-Authored-By` trailer; update PRD only after a live-stack measurement.

## Review Focus

1. A public-share recipient with no login can seek audio but cannot stream a different asset; Task 1 tests both.
2. A thread reload must not start video or audio playing; Task 2 tests this.
3. An SVG named `.png` must remain download-only when stored MIME is SVG; Task 2 tests this.
4. A failed media request must show a useful error and keep download access; Tasks 1 and 2 test this.
5. A gallery with missing or zero dimensions must not invent ratios or crop; Task 3 tests this.

---

### Task 1: Audio stream authorization and player

**Files:**
- Modify: `internal/agui/assets_stream_api.go`, `assets_stream_api_test.go`, `share_api_artifact.go`, `share_stream_api_test.go`
- Modify: `web/src/chat/artifacts/artifactMeta.ts` and its tests, `renderers/previewDispatch.tsx`, `displays/LocalArtifactDisplay.tsx`
- Create: `web/src/chat/artifacts/renderers/AudioPreview.tsx`, `AudioPreview.test.tsx`
- Add the copied standalone `@assistant-ui/elements-media-player` source and its required dependencies.

**Interfaces:**
- Consumes: `AssetSource.streamUrl(assetId)` and stored `asset.MIMEType` after the existing owner/share gate.
- Produces: `PreviewKind = 'audio'` for an explicit safe MIME allowlist; `AudioPreview` passes the active source URL and filename to Elements `AudioPlayer` and retains the Aura download link.

- [ ] **Step 1: Write failing API and UI tests.** Request `Range: bytes=2-5` for owned and public-share `audio/mpeg`, `audio/mp3`, `audio/ogg`, `audio/wav`, `audio/x-wav`, `audio/webm`, `audio/mp4`, `audio/m4a`, `audio/x-m4a`; require `206`, correct `Content-Range`, `nosniff`, inline type, HEAD, and `416` out-of-range. Verify cross-owner/share 404, HTML/SVG 415, audio preview error and inert download. Test that `audio/webm;codecs=opus` is parsed to the bare allowed type.

```go
req := httptest.NewRequest(http.MethodGet, "/api/assets/"+ownedAudioID+"/stream", nil)
req.Header.Set("Range", "bytes=2-5")
rec := httptest.NewRecorder()
server.ServeHTTP(rec, withPrincipal(req, ownerID))
if rec.Code != http.StatusPartialContent || rec.Header().Get("Content-Range") != "bytes 2-5/8" { t.Fatalf("range: %d %q", rec.Code, rec.Header().Get("Content-Range")) }
```

- [ ] **Step 2: Run `go test ./internal/agui -run 'Test.*(AssetStream|ShareStream)' -count=1` and focused Vitest; confirm failures.**
- [ ] **Step 3: Implement the closed allowlist and player.** Refactor `videoStreamType` to a media-type classifier used by both authenticated and share handlers; leave ownership checks before `ServeContent`. Keep existing video behavior. Route safe audio MIME to `audio` in `previewKind`, lazy-load `AudioPreview`, use `new URL(streamUrl(assetId), location.origin).href` only for a same-origin stream route, and mount `<AudioPlayer src={src} title={fileName} />`. Adapt the copied player's underlying `<audio>` with an error callback that selects `PreviewError`. Keep the existing download control beside it.

```ts
const STREAMABLE_AUDIO = new Set(['audio/mpeg', 'audio/mp3', 'audio/ogg', 'audio/wav', 'audio/x-wav', 'audio/webm', 'audio/mp4', 'audio/m4a', 'audio/x-m4a']);
// The Go allowlist mirrors these values after mime.ParseMediaType; reject all other media.
```

- [ ] **Step 4: Run focused Go/Vitest, web typecheck, and the authenticated/share Range fixture.** Inspect `audio` on a narrow viewport and seek twice.
- [ ] **Step 5: Commit only audio-stream/player files.** Use `feat(assets): stream authorized audio in cockpit`.

### Task 2: Image and video presentation over Aura's asset layer

**Files:**
- Modify: `web/src/chat/artifacts/renderers/GeneratedImagePreview.tsx`, `ImagePreview.tsx`, `VideoPreview.tsx`, their focused tests
- Modify: `web/src/chat/displays/LocalArtifactDisplay.tsx`, its tests, `web/src/chat/generation/GenerationToolDisplay.tsx` and its tests only if completed-asset handoff needs it
- Add copied Tool UI `image` source; reuse Elements `VideoPlayer` from Task 1.

**Interfaces:**
- Consumes: `useBlobPreview` for authorized images, `AssetSource.streamUrl` for video, existing `PreviewError` and `EditMediaButton`.
- Produces: completed image and video views with title/source and unchanged Aura download/edit actions; generation's queued/progress view is unchanged until an asset exists.

- [ ] **Step 1: Write failing tests.** Cover image `blob:` URL and revocation, natural image proportions, portrait/square video on a narrow viewport, no `autoplay` attribute after mount/reload, source-tier URLs, media load error, SVG MIME with `.png` filename, and retained edit/download controls.

```tsx
render(<VideoPreview assetId="v1" fileName="portrait.mp4" mimeType="video/mp4" />);
const video = screen.getByLabelText('portrait.mp4');
expect(video).not.toHaveAttribute('autoplay');
expect(video).toHaveAttribute('src', '/api/assets/v1/stream');
```

- [ ] **Step 2: Run focused renderer Vitest and confirm failures.**
- [ ] **Step 3: Adapt the copied frames.** Use Tool UI Image only after `useBlobPreview` resolves; change its `ratio="auto"` handling to natural `<img>` sizing and add `onError` fallback. Adapt the copied Elements `VideoPlayer` to expose its underlying `<video>` error and select `PreviewError`; use `ratio="auto"`, no autoplay, and the stream URL. Preserve `GeneratedImagePreview` zoom, copy, download and edit behavior. Keep local artifact fallback for missing asset ID and unsupported MIME.

```tsx
return <VideoPlayer src={streamUrl(assetId)} title={fileName} ratio="auto" />;
// The wrapper handles media error and shows PreviewError; it does not alter src.
```

- [ ] **Step 4: Run focused Vitest, `npm --prefix web run typecheck`, and authenticated/share visual checks.** Verify a completed generation retains the same asset actions.
- [ ] **Step 5: Commit only image/video presentation files.** Use `feat(chat): frame completed images and video with selected components`.

### Task 3: Dimension-backed gallery and media end-to-end gate

**Files:**
- Create: `web/src/chat/artifacts/renderers/TrustedImageGallery.tsx`, its test
- Modify: the trusted artifact grouping site in `web/src/chat/ExternalStoreChat_messages.tsx` and its focused test; do not group raw tool text
- Add copied Tool UI `image-gallery` source, adapted for Aura asset URLs.
- Add one media Playwright scenario under `web/e2e/` using current authenticated/share fixtures.

**Interfaces:**
- Consumes: two or more trusted `aura.artifact`/completed-generation asset descriptors in one assistant turn and the active `AssetSource`.
- Produces: `groupTrustedImages(items: readonly GalleryCandidate[]): readonly GalleryItem[][]`, where `GalleryCandidate = GalleryItem & { turnId: string }` and `GalleryItem = { assetId: string; src: string; width: number; height: number; alt: string }`. Group by assistant-turn ID only when every member has positive decoded width and height; otherwise render individual image cards. The lightbox and downloads use the same tier's URLs.

- [ ] **Step 1: Write failing gallery tests.** Two trusted images become one gallery after decode; a single image, missing asset ID, rejected MIME, zero dimension, cross-turn pair, or failed image load stays in individual/fallback cards. A public-share lightbox must request its share-scoped URLs, and Escape must close only the lightbox.

```tsx
expect(groupTrustedImages([{turnId:'t1',assetId:'a',width:400,height:300},{turnId:'t1',assetId:'b',width:200,height:500}])).toHaveLength(1);
expect(groupTrustedImages([{turnId:'t1',assetId:'a',width:0,height:300}])).toHaveLength(0);
```

- [ ] **Step 2: Run the gallery Vitest file and confirm failure.**
- [ ] **Step 3: Implement grouping and dimensions.** Derive grouping only from trusted event descriptors already attached to tool parts. Decode image dimensions client-side after authorized load; never write guessed metadata into the asset store. Bound gallery count and image dimensions, preserve source order, and fall back to each image card when any member fails. Adapt Tool UI gallery to natural dimensions, small widths, and Aura lightbox focus/escape behavior.

```ts
type GalleryItem = { assetId: string; src: string; width: number; height: number; alt: string };
type GalleryCandidate = GalleryItem & { turnId: string };
const complete = items.every((item) => item.width > 0 && item.height > 0 && Number.isFinite(item.width * item.height));
if (!complete) return []; // caller renders existing individual cards
```

- [ ] **Step 4: Run focused Vitest, web typecheck and the media Playwright scenario.** Exercise authenticated audio Range seeking, public-share audio, portrait video reload, image failure, and gallery lightbox; record actual stack behavior before PRD edits.
- [ ] **Step 5: Commit only gallery/E2E files and perform a whole-wave review.** Use `feat(chat): group trusted images with real dimensions`.
