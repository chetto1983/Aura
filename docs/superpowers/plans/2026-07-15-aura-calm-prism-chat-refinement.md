# Aura Calm Prism Chat Refinement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refine Aura's chat into the approved Calm Prism interface: contained messages, truthful activity and approval states, useful starters, and run-scoped telemetry that remains accessible on desktop and mobile.

**Architecture:** Keep all backend and AG-UI contracts unchanged. `AppShell` remains the owner of draft events and footer state; `ExternalStoreChat` owns streaming lifecycle, approval placement, and the final-approval resume gate; focused helpers isolate approval selection and usage settlement so both large files stay below Aura's 600-line cap. Semantic `data-*` hooks and an explicit Chrome Playwright project make the visual contract measurable without coupling behavior to presentation classes.

**Tech Stack:** React 19, TypeScript 6, assistant-ui 0.14.22, TanStack Query, i18next, Tailwind CSS 4, Vitest/Testing Library, Playwright 1.61 with installed Chrome.

---

## Working contract and file map

Execute from the dedicated worktree `D:\Aura\.worktrees\calm-prism-chat-plan`. Before every task, run `git status --short`, declare the listed paths as owned, and stop if any listed path already has an unrelated change. Never stash, reset, clean, reformat unrelated files, or use `git add -A`.

The implementation is one client-side project because all slices share the same `AppShell`/`ExternalStoreChat` ownership boundary and must converge in one browser flow.

New focused files:

- `web/src/shell/ChatWorkspaceControls.tsx` — normal-flow Voice/Artifacts control lane.
- `web/src/chat/EmptyThreadStarters.tsx` — localized, presentational prompt-starter grid.
- `web/src/approvals/useThreadApprovals.ts` — active-thread selector and final-resolution resume gate.
- `web/src/chat/runUsage.ts` — per-run start/update/settle/clear lifecycle.
- `web/src/chat/__tests__/ExternalStoreChat.approvals.test.tsx` — DOM order, composer lock, focus, and multi-approval re-drive coverage.
- `web/src/chat/__tests__/runUsage.test.tsx` — exact settlement behavior independent of SSE rendering.
- `web/e2e/support/browserHealth.ts` — browser error collectors with a narrow cancellation allowance.
- `web/e2e/chat-calm-prism.spec.ts` — deterministic responsive/theme/visual contract.
- `docs/superpowers/verification/2026-07-15-aura-calm-prism-chat-visual-review.md` — reference/current comparison record.

Large-file guardrails:

- `web/src/AppShell.tsx` starts near 591 lines. Remove `resumeNonce`, `redriveRun`, and the AppShell-level `ThreadApprovalCards` mount while adding the generalized draft callback and the extracted control component.
- `web/src/chat/ExternalStoreChat.tsx` starts near 537 lines. Delete the nonce-driven resume effect, move lifecycle mechanics into `runUsage.ts` and approval mechanics into `useThreadApprovals.ts`, and move the empty state into `EmptyThreadStarters.tsx`.
- Run `bash scripts/check-file-size.sh` after every production-code task. Expected: `check-file-size: all source files within the 600-LOC cap.`

### Progress tracking hard rule

This plan is the source of truth for implementation progress. Every current and future agent working from it must follow these rules:

- Keep a task open while implementation, spec-compliance review, or code-quality review is incomplete.
- Mark every step in a task `[x]` only after the implementation and both reviews pass with no unresolved findings.
- Update the checkboxes immediately after approval; do not defer progress tracking to a later task or session.
- Commit each approved task's checkbox update as a separate docs-only checkpoint so code-review ranges remain exact.
- Never mark a task complete based only on passing tests or an implementer's report.

### Task 1: Land the PRD-first contract as a standalone commit

**Files:**

- Modify: `prd.md:2984-3006`

- [x] **Step 1: Record the clean implementation baseline and owned path**

Run:

```powershell
git status --short
git rev-parse --short HEAD
```

Expected: HEAD includes the approved design spec; `prd.md` has no unrelated modification. Preserve any unrelated status entries.

- [x] **Step 2: Append Amendment #83 after Amendment #82 and before the cross-cutting risk section**

Add this exact contract:

```markdown
> **▶ Amendment #83 (Calm Prism chat refinement pre-execution gate, 2026-07-15) — BLOCKING, lands before any Calm Prism production code.** Aura's existing chat keeps its backend, AG-UI stream, typed-display, artifact, voice, approval-resolution, and composer-effort contracts, while its browser presentation is refined as follows. (1) Workspace Voice/Artifacts controls move from absolute overlay to a reserved normal-flow lane. Assistant roots are `w-full min-w-0`; only plain prose is capped at exactly `48rem`; typed displays, tools, tables, sources, and artifacts remain fluid. Coarse-pointer/touch layouts expose Voice, Artifacts, Edit, Copy, Reload, Speak/Stop, document/audio actions, and both branch arrows at a minimum 44 × 44 CSS px. (2) The existing `aura.chat.reasoning.shown` key and `'1'`/`'0'` encoding remain, but missing, invalid, or unreadable storage now defaults hidden. Readable explicit values stay authoritative across write failures, current-session toggles still work, disclosure IDs are unique, and reasoning uses `aria-expanded` without `aria-pressed`. (3) Tool raw detail preserves the seven-case running/raw/settled/manual state matrix; output remains escaped `<pre>` text and settled duration is locale-aware. (4) Active-thread approvals render after activity and before a visible locked composer. One shared selector controls cards and lock state. `kind="approval"` receives generic approval-risk framing only: the client must not invent skill-install, container, activation, source, hash, preview, or subtype claims and must not render the resume token as a dedicated field. Resolution URLs and `{action, content?}` verb bodies stay unchanged. Intermediate Accept/Decline resolutions do not re-drive; after refetch, the final locally accepted/declined approval triggers exactly one no-message `/agent/run`; Cancel never re-drives. Questions preserve whitespace and overflow safely; confirmation focus/Escape and one-time terminal announcements are required. (5) Empty-thread Research/File/Artifact/Automation starters are client-owned English/Italian copy. They replace and focus the editable composer through AppShell's sole nonce-backed draft event, never submit, and never open the file picker automatically. (6) Usage remains frontend-only: each new/edit/reload/resume run emits an unsettled undefined reset, live updates for that run, and exactly one settlement from its shared `finally`; cancel only aborts and thread changes clear state. Visible telemetry updates live, while a separate non-interactive status region announces only settlement. Mobile starts with labeled Cost/Context plus a visible disclosure cue; desktop remains complete. This amendment supersedes conflicting earlier cockpit presentation language but introduces no dependency, backend field, endpoint, protocol, raw color, or unsafe HTML path.
>
> **Acceptance gates.** Failing-first Vitest coverage pins reasoning storage/IDs, all seven tool states, generic approval framing/token transport, approval order/lock/focus/three-token resume behavior, exact bilingual starters, run-scoped settlement, live-versus-announced telemetry, semantic width/touch hooks, and resource parity. An explicit Playwright `channel: 'chrome'` project collects console, page, request, and same-origin HTTP failures; deterministic 1440 × 1000 and 393 × 852 dark/light states assert bounding-box nonintersection, viewport containment, 44 × 44 coarse targets, and no horizontal overflow. Reference and implementation screenshots are reviewed in the same comparison input, accepted screenshots become `toHaveScreenshot` baselines, the contrast/typecheck/lint/build/file-size gates pass, and the generated `internal/webui/dist` diff is reviewed separately.
```

- [x] **Step 3: Verify that #83 is the latest numbered amendment and only `prd.md` changed**

Run:

```powershell
rg -n "Amendment #83|Calm Prism" prd.md
git diff --check -- prd.md
git diff --stat -- prd.md
```

Expected: the two Amendment #83 lines are found, `git diff --check` is silent, and the stat names only `prd.md`.

- [x] **Step 4: Commit the PRD gate before any source or test change**

```powershell
git add -- prd.md
git diff --cached --check
git commit -m "docs(prd): gate Calm Prism chat refinement"
```

Expected: one docs-only commit. `git show --stat --oneline HEAD` names only `prd.md`.

### Task 2: Reserve the workspace lane and make message geometry measurable

**Files:**

- Create: `web/src/shell/ChatWorkspaceControls.tsx`
- Modify: `web/src/AppShell.tsx:23-35,401-443,507-520`
- Modify: `web/src/chat/ExternalStoreChat_messages.tsx:45-205,227-266`
- Modify: `web/src/chat/BranchPicker.tsx:15-43`
- Modify: `web/src/chat/attachments/AttachmentChip.tsx:13-37`
- Modify: `web/src/chat/voice/VoiceModeToggle.tsx:15-40`
- Modify: `web/src/shell/ArtifactsShell.tsx:22-56`
- Test: `web/src/__tests__/AppShell.shell.test.tsx`
- Test: `web/src/chat/__tests__/ExternalStoreChat.test.tsx`
- Test: `web/src/chat/ExternalStoreChat_messages.speaker.test.tsx`
- Test: `web/src/AppShell.voice.test.tsx`
- Test: `web/src/AppShell.artifacts.test.tsx`

- [x] **Step 1: Add failing semantic layout and full-action target assertions**

Add these assertions to the existing shell/chat tests after rendering representative content:

```tsx
const controls = container.querySelector("[data-chat-workspace-controls]");
expect(controls).not.toBeNull();
expect(controls?.className).not.toContain("absolute");

const assistant = container.querySelector('[data-message-role="assistant"]');
expect(assistant?.className).toContain("w-full");
expect(assistant?.className).toContain("min-w-0");
expect(assistant?.querySelector("[data-message-prose]")?.className).toContain(
  "max-w-[48rem]",
);
expect(
  assistant?.querySelector("[data-message-content]")?.className,
).not.toContain("max-w-[48rem]");

const actionNames = [
  "Edit",
  "Copy",
  "Regenerate",
  "Read aloud",
  "Previous branch",
  "Next branch",
];
for (const name of actionNames) {
  const action = screen.queryByRole("button", { name });
  if (action !== null) {
    expect(action.className).toMatch(/min-h-11|h-11/);
    expect(action.className).toMatch(/min-w-11|w-11/);
  }
}
```

Also assert the attachment remove button and the Voice/Artifacts buttons carry `min-h-11 min-w-11`.

- [x] **Step 2: Run the targeted tests and observe the current overlay/width/target failures**

Run:

```powershell
cd web
npx vitest run src/__tests__/AppShell.shell.test.tsx src/chat/__tests__/ExternalStoreChat.test.tsx src/chat/ExternalStoreChat_messages.speaker.test.tsx src/AppShell.voice.test.tsx src/AppShell.artifacts.test.tsx
```

Expected: FAIL because the workspace controls are absolute, semantic hooks are absent, assistant prose has no exact 48rem surface, and several targets are below 44px.

- [x] **Step 3: Create the normal-flow workspace control lane**

Create `web/src/shell/ChatWorkspaceControls.tsx`:

```tsx
import { VoiceModeToggle } from "../chat/voice/VoiceModeToggle";
import { ArtifactsToggle } from "./ArtifactsShell";

interface ChatWorkspaceControlsProps {
  readonly artifactsActive: boolean;
  readonly onArtifactsToggle: () => void;
}

export function ChatWorkspaceControls({
  artifactsActive,
  onArtifactsToggle,
}: ChatWorkspaceControlsProps) {
  return (
    <div
      data-chat-workspace-controls
      className="flex min-w-0 shrink-0 items-center justify-end gap-1 border-b border-border px-3 py-1.5"
    >
      <VoiceModeToggle />
      <ArtifactsToggle active={artifactsActive} onToggle={onArtifactsToggle} />
    </div>
  );
}
```

Replace AppShell's relative/absolute wrapper with a two-row contained wrapper:

```tsx
<VoiceModeProvider>
  <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)]">
    <ChatWorkspaceControls
      artifactsActive={artifactsActive}
      onArtifactsToggle={toggleArtifacts}
    />
    {workspace}
  </div>
</VoiceModeProvider>
```

- [x] **Step 4: Add semantic message surfaces and 44px actions**

Use these exact contracts in `ExternalStoreChat_messages.tsx`:

```tsx
<MessagePrimitive.Root
  data-message-role="assistant"
  className="w-full min-w-0 space-y-2"
>
  <div data-message-content className="min-w-0 overflow-x-clip">
    <MessagePrimitive.Parts
      components={{
        Text: () => (
          <div data-message-prose className="max-w-[48rem] text-base leading-relaxed text-text">
            <MarkdownText />
          </div>
        ),
      }}
    />
  </div>
  <ActionBarPrimitive.Root
    data-message-actions
    className="flex flex-wrap items-center gap-1 opacity-0 transition-opacity focus-within:opacity-100 hover:opacity-100 [@media(pointer:coarse)]:opacity-100"
  >
```

Apply `data-required-touch-target` and `inline-flex min-h-11 min-w-11 items-center justify-center` to Edit, Copy, Reload, Speak/Stop, attachment remove/download/promote/retry actions, both BranchPicker arrows, Voice, and Artifacts. Preserve localized `aria-label`s. Add `min-w-0 overflow-wrap-anywhere` or `break-words` to filenames/tool-adjacent labels so controls never shrink off-screen. Do not add raw colors.

- [x] **Step 5: Run the layout tests, file cap, and diff checks**

Run:

```powershell
cd web
npx vitest run src/__tests__/AppShell.shell.test.tsx src/chat/__tests__/ExternalStoreChat.test.tsx src/chat/ExternalStoreChat_messages.speaker.test.tsx src/AppShell.voice.test.tsx src/AppShell.artifacts.test.tsx
cd ..
bash scripts/check-file-size.sh
git diff --check -- web/src/AppShell.tsx web/src/shell/ChatWorkspaceControls.tsx web/src/chat/ExternalStoreChat_messages.tsx web/src/chat/BranchPicker.tsx web/src/chat/attachments/AttachmentChip.tsx web/src/chat/voice/VoiceModeToggle.tsx web/src/shell/ArtifactsShell.tsx
```

Expected: targeted tests PASS, every source file is at most 600 lines, and diff check is silent.

- [x] **Step 6: Commit the contained message and control geometry**

```powershell
git add -- web/src/AppShell.tsx web/src/shell/ChatWorkspaceControls.tsx web/src/chat/ExternalStoreChat_messages.tsx web/src/chat/BranchPicker.tsx web/src/chat/attachments/AttachmentChip.tsx web/src/chat/voice/VoiceModeToggle.tsx web/src/shell/ArtifactsShell.tsx web/src/__tests__/AppShell.shell.test.tsx web/src/chat/__tests__/ExternalStoreChat.test.tsx web/src/chat/ExternalStoreChat_messages.speaker.test.tsx web/src/AppShell.voice.test.tsx web/src/AppShell.artifacts.test.tsx
git commit -m "fix(web): contain chat controls and message actions"
```

### Task 3: Default reasoning closed and repair disclosure semantics

**Files:**

- Modify: `web/src/chat/reasoningPref.ts:1-24`
- Modify: `web/src/chat/ReasoningDrawer.tsx:1-59`
- Test: `web/src/chat/__tests__/ReasoningDrawer.test.tsx`
- Modify: `web/src/chat/__tests__/ExternalStoreChat.test.tsx:57-166`
- Modify: `web/playwright.config.ts:82-111`
- Modify: `web/e2e/chat.spec.ts:333-377`
- Modify (review-driven Chrome project compatibility): `web/e2e/documents-library.spec.ts`
- Modify (review-driven Chrome project compatibility): `web/e2e/documents-library-size.spec.ts`
- Modify (review-driven Chrome project compatibility): `web/e2e/phase32-uat.spec.ts`

- [x] **Step 1: Replace the old builder-default tests with the complete storage and ID matrix**

Add tests with these concrete cases:

```tsx
it.each([
  [null, false],
  ["invalid", false],
  ["0", false],
  ["1", true],
])("reads %s as %s", (stored, expected) => {
  if (stored === null) localStorage.removeItem(PREF_KEY);
  else localStorage.setItem(PREF_KEY, stored);
  expect(readReasoningPref()).toBe(expected);
});

it("defaults hidden when storage cannot be read", () => {
  const get = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
    throw new DOMException("blocked");
  });
  expect(readReasoningPref()).toBe(false);
  get.mockRestore();
});

it("keeps the current-session toggle when persistence fails", () => {
  localStorage.setItem(PREF_KEY, "1");
  const set = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new DOMException("quota");
  });
  render(<ReasoningDrawer text="private trace" />);
  fireEvent.click(screen.getByRole("button", { name: "Hide reasoning" }));
  expect(screen.queryByText("private trace")).not.toBeVisible();
  expect(localStorage.getItem(PREF_KEY)).toBe("1");
  set.mockRestore();
});

it("uses the last readable explicit value on the next mount after a failed write", () => {
  localStorage.setItem(PREF_KEY, "1");
  const set = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new DOMException("quota");
  });
  const first = render(<ReasoningDrawer text="first mount" />);
  fireEvent.click(screen.getByRole("button", { name: "Hide reasoning" }));
  first.unmount();
  render(<ReasoningDrawer text="second mount" />);
  expect(screen.getByRole("button").getAttribute("aria-expanded")).toBe("true");
  set.mockRestore();
});

it("gives every disclosure a resolving unique body id without aria-pressed", () => {
  const { container } = render(
    <>
      <ReasoningDrawer text="one" />
      <ReasoningDrawer text="two" />
    </>,
  );
  const buttons = screen.getAllByRole("button");
  const ids = buttons.map((button) => button.getAttribute("aria-controls"));
  expect(new Set(ids).size).toBe(2);
  for (const [index, button] of buttons.entries()) {
    expect(button.hasAttribute("aria-pressed")).toBe(false);
    expect(
      container.querySelector(`#${CSS.escape(ids[index] ?? "")}`),
    ).not.toBeNull();
  }
});
```

- [x] **Step 2: Run tests and verify the expected default/ID/pressed failures**

```powershell
cd web
npx vitest run src/chat/__tests__/ReasoningDrawer.test.tsx src/chat/__tests__/ExternalStoreChat.test.tsx
```

Expected: FAIL because missing/unreadable storage is shown, every body uses `reasoning-body`, collapsed bodies disappear, and `aria-pressed` is present.

- [x] **Step 3: Implement the storage precedence and disclosure contract**

Use this reader:

```ts
const PREF_KEY = "aura.chat.reasoning.shown";

export function readReasoningPref(): boolean {
  try {
    const raw = localStorage.getItem(PREF_KEY);
    return raw === "1";
  } catch {
    return false;
  }
}
```

In `ReasoningDrawer`, create `const bodyId = useId()` and render the body in both states:

```tsx
<Button
  type="button"
  variant="ghost"
  onClick={toggle}
  aria-expanded={shown}
  aria-controls={bodyId}
  className="min-h-11 w-full justify-start px-2 text-xs text-text-muted hover:text-text"
>
  <ChevronRight aria-hidden="true" className={shown ? 'rotate-90' : ''} />
  <span>{shown ? t('chat.reasoning.hide') : t('chat.reasoning.show')}</span>
</Button>
<div
  id={bodyId}
  hidden={!shown}
  className="whitespace-pre-wrap border-s border-border px-3 py-2 text-xs leading-relaxed text-text-muted"
>
  {displayText}
</div>
```

Retain `writeReasoningPref` as best-effort and update comments from “builder default shown” to “unsaved browser default hidden.”

- [x] **Step 4: Update integration and chronology checks for collapsed-by-default reasoning**

First rename the desktop project to `chrome` and add `channel: 'chrome'` in `web/playwright.config.ts`; apply the same channel to `mobile-chrome`. Then, in Vitest and Playwright, assert fresh storage is collapsed, click the disclosure, and inspect reasoning text:

```ts
await page.addInitScript(() =>
  localStorage.removeItem("aura.chat.reasoning.shown"),
);
await page.goto(`/c/${CONV_ID}`);
const reasoningToggle = page
  .getByRole("button", { name: "Show reasoning" })
  .first();
await expect(reasoningToggle).toHaveAttribute("aria-expanded", "false");
await reasoningToggle.click();
await expect(page.getByText("First timeline reasoning")).toBeVisible();
```

- [x] **Step 5: Verify and commit reasoning behavior**

```powershell
cd web
npx vitest run src/chat/__tests__/ReasoningDrawer.test.tsx src/chat/__tests__/ExternalStoreChat.test.tsx
npx playwright test e2e/chat.spec.ts --project=chrome --grep "chronological stream order"
cd ..
git diff --check -- web/src/chat/reasoningPref.ts web/src/chat/ReasoningDrawer.tsx web/src/chat/__tests__/ReasoningDrawer.test.tsx web/src/chat/__tests__/ExternalStoreChat.test.tsx web/playwright.config.ts web/e2e/chat.spec.ts
git add -- web/src/chat/reasoningPref.ts web/src/chat/ReasoningDrawer.tsx web/src/chat/__tests__/ReasoningDrawer.test.tsx web/src/chat/__tests__/ExternalStoreChat.test.tsx web/playwright.config.ts web/e2e/chat.spec.ts
git commit -m "fix(web): default reasoning disclosures closed"
```

Expected: Vitest and the explicit Chrome chronology test PASS.

### Task 4: Preserve the seven tool states while quieting the card

**Files:**

- Modify: `web/src/chat/ToolActivityCard.tsx:1-244`
- Modify: `web/src/i18n/resources.ts:119-127,396-404`
- Test: `web/src/chat/__tests__/ToolActivityCard.test.tsx`
- Test: `web/src/i18n/__tests__/resources.parity.test.ts`

- [x] **Step 1: Add the two missing state-matrix tests and locale-aware duration checks**

```tsx
it("expands when raw content arrives during a running call before manual intent", () => {
  const { rerender } = render(<ToolActivityCard toolName="delayed" />);
  expect(screen.queryByRole("button")).toBeNull();
  rerender(<ToolActivityCard toolName="delayed" argsText="late args" />);
  expect(screen.getByText("late args")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Hide raw result" })).toBeTruthy();
});

it("does not auto-expand delayed raw after the user has expressed intent", () => {
  const { rerender } = render(
    <ToolActivityCard toolName="manual" argsText="first" />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Hide raw result" }));
  rerender(<ToolActivityCard toolName="manual" argsText="late replacement" />);
  expect(screen.queryByText("late replacement")).toBeNull();
});

it("formats settled decimal duration for the active Italian locale", async () => {
  await i18n.changeLanguage("it");
  render(
    <ToolActivityCard
      toolName="ricerca"
      result="ok"
      startedAt={1000}
      finishedAt={3500}
    />,
  );
  expect(screen.getByTestId("tool-elapsed").textContent).toBe("2,5 s");
  await i18n.changeLanguage("en");
});
```

Keep the existing XSS, settle-once, manual-wins, flicker, child, and timer-leak tests; together they are the seven-case matrix.

- [x] **Step 2: Run the tool tests and observe delayed-raw and locale failures**

```powershell
cd web
npx vitest run src/chat/__tests__/ToolActivityCard.test.tsx src/i18n/__tests__/resources.parity.test.ts
```

Expected: delayed raw remains collapsed and `2.5s` is not localized.

- [x] **Step 3: Implement delayed-raw expansion without weakening manual intent**

Track whether raw has appeared and preserve the current settle-once guard:

```tsx
const hasRaw = raw.length > 0;
const [expanded, setExpanded] = useState(status === "running" && hasRaw);
const userToggled = useRef(false);
const settledOnce = useRef(status !== "running");
const previous = useRef({ status, hasRaw });

useEffect(() => {
  const delayedRaw = status === "running" && hasRaw && !previous.current.hasRaw;
  const settled = previous.current.status === "running" && status !== "running";
  if (!userToggled.current && delayedRaw && !settledOnce.current)
    setExpanded(true);
  if (!userToggled.current && settled && !settledOnce.current)
    setExpanded(false);
  if (settled) settledOnce.current = true;
  previous.current = { status, hasRaw };
}, [hasRaw, status]);
```

On disclosure click, set `userToggled.current = true` before toggling.

- [x] **Step 4: Localize duration and refine hierarchy with semantic tokens**

Add resources:

```ts
duration: {
  seconds: '{{value}} s',
  minutes: '{{minutes}} min {{seconds}} s',
},
```

Format through the active locale:

```ts
function formatElapsed(ms: number, language: string, t: TFunction): string {
  const seconds = Math.max(0, ms / 1000);
  const number = new Intl.NumberFormat(language, {
    maximumFractionDigits: seconds < 10 ? 2 : 0,
  });
  if (seconds < 60)
    return t("chat.tool.duration.seconds", { value: number.format(seconds) });
  const minutes = Math.floor(seconds / 60);
  return t("chat.tool.duration.minutes", {
    minutes: String(minutes),
    seconds: String(Math.floor(seconds % 60)),
  });
}
```

Use one low-contrast `border-border` outer boundary, text plus marker for state, a 44 × 44 disclosure button, `min-w-0 break-words` tool names, and an indented connector for children. Keep raw data exclusively as React text inside `<pre>`. Running elapsed ticks use `aria-hidden="true"`; once settled, the frozen duration becomes ordinary non-live text available to assistive technology. Add a test that rerenders running → settled and verifies this `aria-hidden` transition.

- [x] **Step 5: Verify and commit the tool matrix**

```powershell
cd web
npx vitest run src/chat/__tests__/ToolActivityCard.test.tsx src/i18n/__tests__/resources.parity.test.ts
cd ..
bash scripts/check-file-size.sh
git diff --check -- web/src/chat/ToolActivityCard.tsx web/src/chat/__tests__/ToolActivityCard.test.tsx web/src/i18n/resources.ts
git add -- web/src/chat/ToolActivityCard.tsx web/src/chat/__tests__/ToolActivityCard.test.tsx web/src/i18n/resources.ts
git commit -m "fix(web): preserve truthful tool disclosure states"
```

### Task 5: Put approvals before a locked composer and resume only after the final answer

**Files:**

- Create: `web/src/approvals/useThreadApprovals.ts`
- Modify: `web/src/approvals/useApprovals.ts:19-117`
- Modify: `web/src/approvals/ThreadApprovalCards.tsx:1-45`
- Modify: `web/src/approvals/InlineApprovalCard.tsx:1-315`
- Modify: `web/src/chat/ExternalStoreChat.tsx:55-88,316-406,481-533`
- Modify: `web/src/chat/Composer.tsx:46-80,152-176,302-440`
- Modify: `web/src/AppShell.tsx:139-149,229-232,428-442`
- Modify: `web/src/i18n/resources.ts:210-250,487-528`
- Test: `web/src/approvals/__tests__/InlineApprovalCard.test.tsx`
- Test: `web/src/approvals/__tests__/ThreadApprovalCards.test.tsx`
- Create: `web/src/chat/__tests__/ExternalStoreChat.approvals.test.tsx`
- Test: `web/src/chat/__tests__/Composer.test.tsx`

- [x] **Step 1: Add failing generic-framing, token, whitespace, and terminal-tone tests**

```tsx
const risky = approval({
  token: "resume-secret-123",
  conversation_id: "c-1",
  kind: "approval",
  question: "Run command:\n  rm -rf /tmp/example\nReview scope first.",
});
renderCard(risky);
expect(screen.getByText("Approval required")).toBeTruthy();
expect(
  screen.getByText("Review the scope and consequence before continuing."),
).toBeTruthy();
expect(screen.queryByText(/container|install|activate/i)).toBeNull();
expect(screen.queryByText("resume-secret-123")).toBeNull();
expect(screen.getByText(risky.question).className).toMatch(
  /whitespace-pre-wrap/,
);

fireEvent.click(screen.getByRole("button", { name: "Decline" }));
await waitFor(() =>
  expect(resolveCall.url).toContain("/resume-secret-123/resolve"),
);
expect(resolveCall.body).toEqual({ action: "decline" });
expect(
  screen
    .getByText(/declined/i)
    .closest("[data-tone]")
    ?.getAttribute("data-tone"),
).toBe("neutral");
```

Add parallel pending/terminal/failure sentinel assertions proving the token is absent as visible text in every state while URL transport remains encoded.

- [x] **Step 2: Add failing integration coverage for DOM order, lock, focus, and three approvals**

Create `ExternalStoreChat.approvals.test.tsx` using the existing QueryClient/SSE helpers. The core case must be literal:

```tsx
const pending = [
  row("token-1", "First decision"),
  row("token-2", "Second decision"),
  row("token-3", "Third decision"),
];
const runRequests: RequestInit[] = [];
renderChat(<ExternalStoreChat threadId="c-1" />);

const composer = await screen.findByTestId("chat-composer");
const cards = await screen.findByTestId("thread-approvals");
expect(
  cards.compareDocumentPosition(composer) & Node.DOCUMENT_POSITION_FOLLOWING,
).toBeTruthy();
expect(composer.getAttribute("aria-disabled")).toBe("true");
expect(screen.getByPlaceholderText("Ask Aura")).toBeDisabled();
expect(screen.getByRole("button", { name: "Add files" })).toBeDisabled();

fireEvent.click(screen.getAllByRole("button", { name: "Yes" })[0]);
await waitFor(() => expect(runRequests).toHaveLength(0));
expect(
  document.activeElement
    ?.closest("[data-approval-token]")
    ?.getAttribute("data-approval-token"),
).toBe("token-2");

fireEvent.click(screen.getAllByRole("button", { name: "Yes" })[0]);
await waitFor(() => expect(runRequests).toHaveLength(0));
fireEvent.click(screen.getAllByRole("button", { name: "Yes" })[0]);
await waitFor(() => expect(runRequests).toHaveLength(1));
expect(screen.getByPlaceholderText("Ask Aura")).not.toBeDisabled();
expect(document.activeElement).toBe(screen.getByPlaceholderText("Ask Aura"));
```

Add a second case where Cancel resolves the queue and leaves `runRequests` empty. Add a confirmation case proving focus moves to **Keep running**, Escape restores **Cancel run**, and all confirmation controls are 44px-classed.

- [x] **Step 3: Run the approval/composer tests and verify current false claims and eager re-drive failures**

```powershell
cd web
npx vitest run src/approvals/__tests__/InlineApprovalCard.test.tsx src/approvals/__tests__/ThreadApprovalCards.test.tsx src/chat/__tests__/ExternalStoreChat.approvals.test.tsx src/chat/__tests__/Composer.test.tsx
```

Expected: FAIL because `kind=approval` claims skill/container behavior, renders the token, cards are after the composer, children remain active, and every accept/decline re-drives immediately.

- [x] **Step 4: Create the single active-thread selector and generation-based resume gate**

Create `useThreadApprovals.ts` with these public types and behavior:

```ts
import { useCallback, useEffect, useMemo, useRef } from "react";
import type { Approval, ResolveAction } from "./useApprovals";
import { useApprovals } from "./useApprovals";

export interface ApprovalResolution {
  readonly approval: Approval;
  readonly action: ResolveAction;
}

export function selectThreadApprovals(
  rows: readonly Approval[] | undefined,
  threadId: string,
) {
  if (threadId.length === 0) return [];
  return (rows ?? []).filter((row) => row.conversation_id === threadId);
}

export function useThreadApprovals(
  threadId: string,
  onResume: () => Promise<void>,
  onFocusRequested: (next: Approval | undefined) => void,
) {
  const query = useApprovals();
  const approvals = useMemo(
    () => selectThreadApprovals(query.data, threadId),
    [query.data, threadId],
  );
  const gate = useRef({
    threadId,
    generation: 0,
    hadPending: false,
    resumed: -1,
  });

  useEffect(() => {
    if (gate.current.threadId !== threadId) {
      gate.current = {
        threadId,
        generation: 0,
        hadPending: false,
        resumed: -1,
      };
    }
    if (approvals.length > 0 && !gate.current.hadPending) {
      gate.current.generation += 1;
      gate.current.hadPending = true;
    }
  }, [approvals.length, threadId]);

  const onResolved = useCallback(
    async ({ action }: ApprovalResolution) => {
      const generation = gate.current.generation;
      const refreshed = await query.refetch();
      const remaining = selectThreadApprovals(refreshed.data, threadId);
      if (remaining.length > 0) {
        onFocusRequested(remaining[0]);
        return;
      }
      gate.current.hadPending = false;
      onFocusRequested(undefined);
      if (action === "cancel" || gate.current.resumed === generation) return;
      gate.current.resumed = generation;
      await onResume();
    },
    [onFocusRequested, onResume, query, threadId],
  );

  return { approvals, isPending: approvals.length > 0, onResolved };
}
```

Preserve array order; do not client-sort or infer a subtype. The generation guard makes duplicate final callbacks idempotent.

- [x] **Step 5: Make approval cards truthful, focus-safe, and announcement-safe**

Change `InlineApprovalCard.onResolved` to receive `ApprovalResolution`. Replace `SkillRiskStrip` with generic framing:

```tsx
function ApprovalFrame({ kind }: { readonly kind: string }) {
  const { t } = useTranslation();
  const isApproval = kind === "approval";
  return (
    <div className="flex flex-col gap-1 border-s-2 border-warning ps-3">
      <span className="text-sm font-medium text-text">
        {t(isApproval ? "approval.frame.approval" : "approval.frame.input")}
      </span>
      {isApproval ? (
        <span className="text-xs text-text-muted">
          {t("approval.frame.review")}
        </span>
      ) : null}
    </div>
  );
}
```

Render questions with `whitespace-pre-wrap break-words [overflow-wrap:anywhere] overflow-x-auto`. Give each card `data-approval-token={approval.token}` and `tabIndex={-1}` without rendering the token as text. Answered=`success`, Declined=`neutral`, Cancelled/failure=`danger`, Expired=`warning`; add a `neutral` chip tone using text/border tokens.

Keep announcements alive even when the resolved row disappears after refetch. `ThreadApprovalCards` owns `{ id, text }` announcement state, increments `id` for each local resolution, and renders this node even when `approvals` is empty:

```tsx
<p
  key={announcement.id}
  role="status"
  aria-live="polite"
  aria-atomic="true"
  className="sr-only"
>
  {announcement.text}
</p>
```

The newly keyed node announces repeated intermediate outcomes once each without a timer. Inline request failure remains mounted with `role="status"`; an expired row renders its warning status once on mount.

For cancel confirmation, hold refs to Cancel and Keep-running buttons. On open, focus Keep-running; on Escape, close and refocus Cancel. Give every confirmation control `min-h-11`.

- [x] **Step 6: Move cards inside `ExternalStoreChat` and lock every composer action**

Delete `resumeNonce`, `redriveRun`, and the AppShell-level `ThreadApprovalCards`. In `ExternalStoreChat`, convert the old nonce resume effect into an awaited `resumeRun` callback, then call the hook:

```tsx
const composerInputRef = useRef<HTMLTextAreaElement | null>(null);
const resumeRun = useCallback(async () => {
  if (threadId.length === 0) return;
  await foldResumeRun(threadId);
}, [foldResumeRun, threadId]);
const threadApprovals = useThreadApprovals(threadId, resumeRun, (next) => {
  requestAnimationFrame(() => {
    if (next === undefined) {
      composerInputRef.current?.focus();
      return;
    }
    document
      .querySelector<HTMLElement>(
        `[data-approval-token="${CSS.escape(next.token)}"]`,
      )
      ?.focus();
  });
});
```

Render in this exact order:

```tsx
{isRunning ? <p role="status">{t('chat.running')}</p> : null}
<ThreadApprovalCards
  approvals={threadApprovals.approvals}
  isStreaming={isRunning}
  onResolved={threadApprovals.onResolved}
/>
<Composer
  inputRef={composerInputRef}
  approvalLocked={threadApprovals.isPending}
  uploads={uploads}
  draftPrompt={draftPrompt}
  skills={skills}
  pinnedSkill={pinnedSkill}
  onPinSkill={setPinnedSkill}
  onNewChat={onNewChat}
  effort={effort}
  effortLevels={reasoningCaps.levels}
  onEffortChange={setEffort}
/>
```

In `Composer`, use a stable `const approvalHintId = useId()`, set `data-testid="chat-composer"`, `aria-disabled`, and `aria-describedby` on the root, render the localized hint, and pass `disabled={approvalLocked || existingCondition}` to the textarea, Add files, Mic, Send, Cancel, skill buttons, and effort select. Guard pointer/key handlers with `if (approvalLocked) return;` where a primitive lacks native disabled behavior.

- [x] **Step 7: Add exact bilingual approval copy**

Replace the skill-specific bundle with:

```ts
frame: {
  approval: 'Approval required',
  input: 'Input required',
  review: 'Review the scope and consequence before continuing.',
},
lock: 'Answer the request above to continue.',
```

Italian:

```ts
frame: {
  approval: 'Approvazione richiesta',
  input: 'Input richiesto',
  review: 'Controlla ambito e conseguenze prima di continuare.',
},
lock: 'Rispondi alla richiesta qui sopra per continuare.',
```

Change Answered copy so it does not claim the run resumed before the final pending item; use `Answered.` / `Risposta inviata.`. Remove skill/container/token keys only after all imports are gone.

- [x] **Step 8: Verify the approval gate, file cap, and transport contract**

```powershell
cd web
npx vitest run src/approvals/__tests__/InlineApprovalCard.test.tsx src/approvals/__tests__/ThreadApprovalCards.test.tsx src/chat/__tests__/ExternalStoreChat.approvals.test.tsx src/chat/__tests__/Composer.test.tsx src/i18n/__tests__/resources.parity.test.ts
cd ..
bash scripts/check-file-size.sh
git diff --check -- web/src/approvals web/src/chat/ExternalStoreChat.tsx web/src/chat/Composer.tsx web/src/AppShell.tsx web/src/i18n/resources.ts
```

Expected: all tests PASS; three accepts cause one `/agent/run`, cancel causes zero, URL token transport and JSON verb bodies are unchanged, and both large files stay under 600 lines.

- [x] **Step 9: Commit the approval hierarchy**

```powershell
git add -- web/src/approvals/useThreadApprovals.ts web/src/approvals/useApprovals.ts web/src/approvals/ThreadApprovalCards.tsx web/src/approvals/InlineApprovalCard.tsx web/src/chat/ExternalStoreChat.tsx web/src/chat/Composer.tsx web/src/AppShell.tsx web/src/i18n/resources.ts web/src/approvals/__tests__/InlineApprovalCard.test.tsx web/src/approvals/__tests__/ThreadApprovalCards.test.tsx web/src/chat/__tests__/ExternalStoreChat.approvals.test.tsx web/src/chat/__tests__/Composer.test.tsx
git commit -m "fix(web): gate chat on pending approvals"
```

### Task 6: Add exact bilingual prompt starters through the sole draft owner

**Files:**

- Create: `web/src/chat/EmptyThreadStarters.tsx`
- Create: `web/src/chat/__tests__/EmptyThreadStarters.test.tsx`
- Modify: `web/src/chat/ExternalStoreChat.tsx:55-88,481-513`
- Modify: `web/src/chat/Composer.tsx:46-80,170-176,379-395`
- Modify: `web/src/AppShell.tsx:23,147-149,265-285,428-436`
- Modify: `web/src/i18n/resources.ts:94-100,371-377`
- Test: `web/src/__tests__/AppShell.conversation.test.tsx`
- Test: `web/src/chat/__tests__/Composer.test.tsx`
- Test: `web/src/i18n/__tests__/resources.parity.test.ts`

- [ ] **Step 1: Add failing exact-copy and non-submit starter tests**

```tsx
const requests: string[] = [];
vi.stubGlobal(
  "fetch",
  vi.fn((input: RequestInfo | URL) => {
    const url = String(input);
    requests.push(url);
    return Promise.resolve(messagesSnapshotResponse([]));
  }),
);
renderShellAt("/c/conv-1");

fireEvent.click(
  await screen.findByRole("button", { name: "Research a topic" }),
);
const input = screen.getByPlaceholderText("Ask Aura");
expect(input).toHaveValue(
  "Research [topic] and compare the most reliable sources.",
);
expect(document.activeElement).toBe(input);
expect(requests.filter((url) => url.includes("/agent/run"))).toEqual([]);

fireEvent.change(input, { target: { value: "temporary edit" } });
fireEvent.click(screen.getByRole("button", { name: "Research a topic" }));
expect(input).toHaveValue(
  "Research [topic] and compare the most reliable sources.",
);
```

Switch i18n to Italian and assert all four exact labels and inserted bodies.

- [ ] **Step 2: Run starter/composer/resource tests and observe missing controls/focus**

```powershell
cd web
npx vitest run src/chat/__tests__/EmptyThreadStarters.test.tsx src/__tests__/AppShell.conversation.test.tsx src/chat/__tests__/Composer.test.tsx src/i18n/__tests__/resources.parity.test.ts
```

Expected: FAIL because the empty thread has no starters and draft application does not focus the input.

- [ ] **Step 3: Create the presentational starter grid**

```tsx
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";

const STARTERS = ["research", "file", "artifact", "automation"] as const;

export function EmptyThreadStarters({
  onRequestDraftPrompt,
}: {
  readonly onRequestDraftPrompt: (text: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <div
      aria-label={t("chat.empty.suggestionsLabel")}
      className="grid w-full max-w-2xl grid-cols-1 gap-2 sm:grid-cols-2"
    >
      {STARTERS.map((starter) => (
        <Button
          key={starter}
          type="button"
          variant="outline"
          className="min-h-11 h-auto min-w-0 justify-start whitespace-normal px-3 py-2 text-start"
          onClick={() =>
            onRequestDraftPrompt(
              t(`chat.empty.thread.starters.${starter}.body`),
            )
          }
        >
          {t(`chat.empty.thread.starters.${starter}.label`)}
        </Button>
      ))}
    </div>
  );
}
```

Render it below the empty-state body and keep the sidebar empty state unchanged.

- [ ] **Step 4: Generalize AppShell's nonce-backed draft event**

Rename `documentDraftPrompt` to `composerDraftPrompt`. Add:

```tsx
const requestComposerDraft = useCallback(
  (text: string) => {
    setComposerDraftPrompt((current) => ({
      text,
      nonce: (current?.nonce ?? 0) + 1,
    }));
    setSurface("chat");
    closeNav();
  },
  [closeNav, setSurface],
);
```

Make `askDocument` calculate its current localized text and call `requestComposerDraft(text)`. Pass both `draftPrompt={composerDraftPrompt}` and `onRequestDraftPrompt={requestComposerDraft}` to `ExternalStoreChat`. Every click increments nonce, including the same starter twice.

In Composer's draft effect, after `setText`, call `inputRef.current?.focus()`; do not submit and do not open the attachment picker.

- [ ] **Step 5: Add the normative English and Italian starter copy**

```ts
starters: {
  research: {
    label: 'Research a topic',
    body: 'Research [topic] and compare the most reliable sources.',
  },
  file: {
    label: 'Analyze a file',
    body: "Analyze the file I'll attach and summarize the key findings.",
  },
  artifact: {
    label: 'Create an artifact',
    body: 'Create a [report/table/document] about [topic].',
  },
  automation: {
    label: 'Automate a task',
    body: 'Help me plan and automate [repeatable task].',
  },
},
```

```ts
starters: {
  research: {
    label: 'Cerca un argomento',
    body: 'Cerca [argomento] e confronta le fonti più affidabili.',
  },
  file: {
    label: 'Analizza un file',
    body: 'Analizza il file che allegherò e riassumi i risultati principali.',
  },
  artifact: {
    label: 'Crea un artefatto',
    body: 'Crea un [rapporto/tabella/documento] su [argomento].',
  },
  automation: {
    label: "Automatizza un'attività",
    body: 'Aiutami a pianificare e automatizzare [attività ripetitiva].',
  },
},
```

- [ ] **Step 6: Verify and commit the starters**

```powershell
cd web
npx vitest run src/chat/__tests__/EmptyThreadStarters.test.tsx src/__tests__/AppShell.conversation.test.tsx src/chat/__tests__/Composer.test.tsx src/i18n/__tests__/resources.parity.test.ts
cd ..
bash scripts/check-file-size.sh
git diff --check -- web/src/chat/EmptyThreadStarters.tsx web/src/chat/ExternalStoreChat.tsx web/src/chat/Composer.tsx web/src/AppShell.tsx web/src/i18n/resources.ts
git add -- web/src/chat/EmptyThreadStarters.tsx web/src/chat/__tests__/EmptyThreadStarters.test.tsx web/src/chat/ExternalStoreChat.tsx web/src/chat/Composer.tsx web/src/AppShell.tsx web/src/i18n/resources.ts web/src/__tests__/AppShell.conversation.test.tsx web/src/chat/__tests__/Composer.test.tsx web/src/i18n/__tests__/resources.parity.test.ts
git commit -m "feat(web): add Calm Prism prompt starters"
```

### Task 7: Make usage state run-scoped and announcements settlement-only

**Files:**

- Create: `web/src/chat/runUsage.ts`
- Create: `web/src/chat/__tests__/runUsage.test.tsx`
- Modify: `web/src/chat/ExternalStoreChat.tsx:55-61,116-220,316-406`
- Modify: `web/src/AppShell.tsx:139-160,251-258,428-436,533-535`
- Modify: `web/src/chat/RuntimeFooter.tsx:32-194`
- Modify: `web/src/i18n/resources.footer.ts:1-34`
- Test: `web/src/chat/__tests__/ExternalStoreChat.test.tsx`
- Test: `web/src/chat/__tests__/RuntimeFooter.test.tsx`

- [ ] **Step 1: Add failing lifecycle tests for reset, update, settle-once, cancel, and clear**

```tsx
it("resets every run and settles it exactly once with its own last usage", () => {
  const events: RunUsageEvent[] = [];
  const { result } = renderHook(() =>
    useRunUsageLifecycle((event) => events.push(event)),
  );
  const first = result.current.start();
  result.current.update(first, usage(10));
  result.current.settle(first);
  result.current.settle(first);
  const second = result.current.start();
  result.current.settle(second);

  expect(events).toEqual([
    { runId: 1, phase: "running", usage: undefined },
    { runId: 1, phase: "running", usage: usage(10) },
    { runId: 1, phase: "settled", usage: usage(10) },
    { runId: 2, phase: "running", usage: undefined },
    { runId: 2, phase: "settled", usage: undefined },
  ]);
});

it("ignores stale updates and clears on thread change", () => {
  const events: RunUsageEvent[] = [];
  const { result } = renderHook(() =>
    useRunUsageLifecycle((event) => events.push(event)),
  );
  const oldRun = result.current.start();
  const newRun = result.current.start();
  result.current.update(oldRun, usage(999));
  result.current.clear();
  expect(events.at(-1)).toEqual({
    runId: newRun,
    phase: "idle",
    usage: undefined,
  });
});
```

Extend ExternalStoreChat integration tests to cover normal send, edit, reload, resume, failure, and AbortError. Each started run must finish with one `phase:'settled'`; the cancel handler only aborts, so the stream `finally` performs settlement.

- [ ] **Step 2: Add failing footer tests for live visual values and one hidden announcement**

```tsx
const running = { runId: 7, phase: "running" as const, usage: usage(200) };
const { container, rerender } = renderFooter({ usageState: running });
expect(screen.getByTestId("footer-visible-metrics").textContent).toMatch(/200/);
const announcer = screen.getByTestId("footer-settled-status");
expect(announcer.textContent).not.toMatch(/200/);

rerender(
  <RuntimeFooter
    conversationId="c-1"
    usageState={{ ...running, phase: "settled" }}
  />,
);
expect(announcer.textContent).toMatch(/200/);
const settledText = announcer.textContent;
fireEvent.click(screen.getByRole("button", { name: "Show telemetry details" }));
expect(announcer.textContent).toBe(settledText);
```

Also assert the mobile button visibly labels Cost and Context, has a chevron/cue, and the gauge sits inside expanded detail on mobile while remaining visible at `sm`.

- [ ] **Step 3: Run lifecycle/footer tests and observe prior-turn reuse and live-region coupling**

```powershell
cd web
npx vitest run src/chat/__tests__/runUsage.test.tsx src/chat/__tests__/ExternalStoreChat.test.tsx src/chat/__tests__/RuntimeFooter.test.tsx
```

Expected: FAIL because there is no run ID/phase, new runs do not emit a reset, settlement is implicit, and visible metrics are latched inside the live region.

- [ ] **Step 4: Implement the isolated run lifecycle**

Create `runUsage.ts`:

```ts
import { useCallback, useMemo, useRef } from "react";
import type { TurnUsage } from "./sseAdapter";

export type RunUsagePhase = "idle" | "running" | "settled";
export interface RunUsageEvent {
  readonly runId: number;
  readonly phase: RunUsagePhase;
  readonly usage: TurnUsage | undefined;
}

export function useRunUsageLifecycle(onUsage?: (event: RunUsageEvent) => void) {
  const sequence = useRef(0);
  const active = useRef<{
    runId: number;
    usage: TurnUsage | undefined;
    settled: boolean;
  } | null>(null);
  const start = useCallback(() => {
    const runId = ++sequence.current;
    active.current = { runId, usage: undefined, settled: false };
    onUsage?.({ runId, phase: "running", usage: undefined });
    return runId;
  }, [onUsage]);
  const update = useCallback(
    (runId: number, usage: TurnUsage | undefined) => {
      if (active.current?.runId !== runId || active.current.settled) return;
      if (usage !== undefined) active.current.usage = usage;
      onUsage?.({ runId, phase: "running", usage: active.current.usage });
    },
    [onUsage],
  );
  const settle = useCallback(
    (runId: number) => {
      if (active.current?.runId !== runId || active.current.settled) return;
      active.current.settled = true;
      onUsage?.({ runId, phase: "settled", usage: active.current.usage });
    },
    [onUsage],
  );
  const clear = useCallback(() => {
    const runId = active.current?.runId ?? sequence.current;
    active.current = null;
    onUsage?.({ runId, phase: "idle", usage: undefined });
  }, [onUsage]);
  return useMemo(
    () => ({ start, update, settle, clear }),
    [clear, settle, start, update],
  );
}
```

- [ ] **Step 5: Wire every stream path through start/update/finally-settle**

Change `ExternalStoreChat.onUsage` to `(event: RunUsageEvent) => void`. For new, edit/reload, and approval-resume paths use this exact pattern:

```ts
const usageRunId = usageLifecycle.start();
try {
  await streamPost({
    url,
    body,
    signal: controller.signal,
    onUpdate: (assistant, usage) => {
      usageLifecycle.update(usageRunId, usage);
      applyAssistant(assistant);
    },
  });
} catch (error) {
  if (!(error instanceof DOMException && error.name === "AbortError"))
    showStreamError();
} finally {
  usageLifecycle.settle(usageRunId);
  finishRunningState();
}
```

Call `usageLifecycle.clear()` when `threadId` changes. `onCancel` only calls `abortRef.current?.abort()`; it does not emit usage. AppShell stores the whole event and initializes `{runId: 0, phase: 'idle', usage: undefined}`.

- [ ] **Step 6: Split visible telemetry from the settled-only status**

Change RuntimeFooter to receive `usageState: RunUsageEvent`. Render visible values from the current `liveCluster` in `data-testid="footer-visible-metrics"`. Render a separate non-interactive announcer:

```tsx
const settled = useSettledAnnouncement(liveCluster, usageState);

<div data-testid="footer-visible-metrics" className="flex flex-wrap items-center gap-x-6 gap-y-2">
  {/* mobile summary and desktop/current metrics use liveCluster */}
</div>
<div
  data-testid="footer-settled-status"
  role="status"
  aria-live="polite"
  aria-atomic="true"
  className="sr-only"
>
  {settled === null ? '' : t('footer.settledAnnouncement', settled)}
</div>
```

The hook keys announcements by `runId` and only latches when `phase === 'settled'`:

```ts
function useSettledAnnouncement(live: NumericCluster, event: RunUsageEvent) {
  const [value, setValue] = useState<NumericCluster | null>(null);
  const announcedRun = useRef<number | null>(null);
  useEffect(() => {
    if (event.phase !== "settled" || announcedRun.current === event.runId)
      return;
    announcedRun.current = event.runId;
    setValue(live);
  }, [event.phase, event.runId, live]);
  return value;
}
```

Memoize `liveCluster` so this effect is stable. Add `showDetails`/`hideDetails` labels and a settled announcement template to both footer resource bundles.

- [ ] **Step 7: Verify all usage paths and commit**

```powershell
cd web
npx vitest run src/chat/__tests__/runUsage.test.tsx src/chat/__tests__/ExternalStoreChat.test.tsx src/chat/__tests__/RuntimeFooter.test.tsx src/i18n/__tests__/resources.parity.test.ts
cd ..
bash scripts/check-file-size.sh
git diff --check -- web/src/chat/runUsage.ts web/src/chat/ExternalStoreChat.tsx web/src/chat/RuntimeFooter.tsx web/src/AppShell.tsx web/src/i18n/resources.footer.ts
git add -- web/src/chat/runUsage.ts web/src/chat/__tests__/runUsage.test.tsx web/src/chat/ExternalStoreChat.tsx web/src/chat/__tests__/ExternalStoreChat.test.tsx web/src/chat/RuntimeFooter.tsx web/src/chat/__tests__/RuntimeFooter.test.tsx web/src/AppShell.tsx web/src/i18n/resources.footer.ts
git commit -m "fix(web): scope telemetry to each chat run"
```

### Task 8: Add explicit Chrome Playwright health and visual contracts

**Files:**

- Modify: `web/playwright.config.ts:1-126`
- Modify: `web/e2e/chat.spec.ts:333-456`
- Create: `web/e2e/support/browserHealth.ts`
- Create: `web/e2e/chat-calm-prism.spec.ts`
- Create: `web/e2e/__screenshots__/chat-calm-prism.spec.ts/calm-prism-desktop-dark.png`
- Create: `web/e2e/__screenshots__/chat-calm-prism.spec.ts/calm-prism-desktop-light.png`
- Create: `web/e2e/__screenshots__/chat-calm-prism.spec.ts/calm-prism-mobile-dark.png`
- Create: `web/e2e/__screenshots__/chat-calm-prism.spec.ts/calm-prism-mobile-light.png`
- Create: `docs/superpowers/verification/2026-07-15-aura-calm-prism-chat-visual-review.md`
- Create: `docs/superpowers/verification/2026-07-15-aura-calm-prism-chat/reference-desktop-dark.png`
- Create: `docs/superpowers/verification/2026-07-15-aura-calm-prism-chat/reference-mobile-dark.png`
- Create: `docs/superpowers/verification/2026-07-15-aura-calm-prism-chat/implementation-desktop-dark.png`
- Create: `docs/superpowers/verification/2026-07-15-aura-calm-prism-chat/implementation-mobile-dark.png`
- Create: `docs/superpowers/verification/2026-07-15-aura-calm-prism-chat/comparison-desktop-dark.png`
- Create: `docs/superpowers/verification/2026-07-15-aura-calm-prism-chat/comparison-mobile-dark.png`

- [ ] **Step 1: Pin Playwright to installed Chrome and stable snapshot paths**

Keep the explicit Chrome projects introduced in Task 3 and add stable snapshot paths:

```ts
snapshotPathTemplate: '{testDir}/__screenshots__/{testFilePath}/{arg}{ext}',
projects: [
  { name: 'chrome', use: { ...devices['Desktop Chrome'], channel: 'chrome' } },
  {
    name: 'mobile-chrome',
    use: { ...devices['Pixel 5'], channel: 'chrome' },
  },
  ...(HTTPS_ORIGIN === undefined ? [] : [{
    name: 'mobile-safari',
    use: { ...devices['iPhone 13'], baseURL: HTTPS_ORIGIN },
  }]),
],
```

Update any command/document reference from `--project=chromium` to `--project=chrome`.

- [ ] **Step 2: Create the browser health collector before navigation**

```ts
import { expect, type Page } from "@playwright/test";

interface AllowedHttpFailure {
  readonly method: string;
  readonly pathname: string;
  readonly status: number;
}

export function collectBrowserHealth(
  page: Page,
  origin: string,
  allowedHttp: readonly AllowedHttpFailure[] = [],
) {
  const problems: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error") problems.push(`console: ${message.text()}`);
  });
  page.on("pageerror", (error) => problems.push(`pageerror: ${error.message}`));
  page.on("requestfailed", (request) => {
    const failure = request.failure()?.errorText ?? "unknown";
    const intentionalAbort =
      request.url().endsWith("/agent/run") && failure.includes("ERR_ABORTED");
    if (!intentionalAbort)
      problems.push(
        `requestfailed: ${request.method()} ${request.url()} ${failure}`,
      );
  });
  page.on("response", (response) => {
    const url = new URL(response.url());
    const request = response.request();
    const allowed = allowedHttp.some(
      (entry) =>
        entry.method === request.method() &&
        entry.pathname === url.pathname &&
        entry.status === response.status(),
    );
    if (url.origin === origin && response.status() >= 400 && !allowed) {
      problems.push(`http: ${response.status()} ${url.pathname}`);
    }
  });
  return {
    assertClean() {
      expect(problems).toEqual([]);
    },
  };
}
```

Call `collectBrowserHealth(page, new URL(baseURL).origin)` before every `goto`. The approval-error test may pass exactly `[{ method: 'POST', pathname: '/api/approvals/error-token/resolve', status: 409 }]`; do not allowlist auth, polling, voice, artifact, asset, wildcard paths, or status ranges.

- [ ] **Step 3: Build deterministic Calm Prism fixtures and geometry helpers**

In `chat-calm-prism.spec.ts`, fix locale, theme, density, reduced motion, font readiness, long questions, long tool names, typed display, artifact data, and three approvals through `page.addInitScript` plus `page.route`. Use stable viewport cases:

```ts
const CASES = [
  {
    name: "desktop-dark",
    viewport: { width: 1440, height: 1000 },
    theme: "dark",
  },
  {
    name: "desktop-light",
    viewport: { width: 1440, height: 1000 },
    theme: "light",
  },
  { name: "mobile-dark", viewport: { width: 393, height: 852 }, theme: "dark" },
  {
    name: "mobile-light",
    viewport: { width: 393, height: 852 },
    theme: "light",
  },
] as const;

const isMobileCase = (name: string) => name.startsWith("mobile-");

async function expectInsideViewport(locator: Locator, page: Page) {
  const box = await locator.boundingBox();
  if (box === null) throw new Error("required rectangle is not rendered");
  const viewport = page.viewportSize();
  if (viewport === null) throw new Error("viewport is unavailable");
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.y).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(viewport.width);
  expect(box.y + box.height).toBeLessThanOrEqual(viewport.height);
}

function intersects(a: BoundingBox, b: BoundingBox) {
  return (
    a.x < b.x + b.width &&
    a.x + a.width > b.x &&
    a.y < b.y + b.height &&
    a.y + a.height > b.y
  );
}
```

Fixtures must expose: empty starters; reasoning plus running/done/error tools; ordinary input plus generic approval with option/free-text/terminal/error states; typed result and artifact; collapsed/expanded footer; long filenames and questions. Route responses must be 2xx except the intentional resolve-error case scoped to that test.

- [ ] **Step 4: Assert nonintersection, containment, target size, and overflow**

```ts
const controls = page.locator("[data-chat-workspace-controls]");
const firstContent = page.locator("[data-message-content]").first();
const actionRows = page.locator("[data-message-actions]");
const prose = page.locator("[data-message-prose]");
const controlsBox = await controls.boundingBox();
const contentBox = await firstContent.boundingBox();
if (controlsBox === null || contentBox === null)
  throw new Error("geometry hooks missing");
expect(intersects(controlsBox, contentBox)).toBe(false);

for (const row of await actionRows.all()) await expectInsideViewport(row, page);
for (const block of await prose.all()) await expectInsideViewport(block, page);
expect(
  await page.evaluate(
    () =>
      document.documentElement.scrollWidth <=
      document.documentElement.clientWidth,
  ),
).toBe(true);

if (testInfo.project.name === "mobile-chrome") {
  const required = page.locator("[data-required-touch-target]");
  for (const control of await required.all()) {
    const box = await control.boundingBox();
    if (box === null) throw new Error("touch target missing");
    expect(box.width).toBeGreaterThanOrEqual(44);
    expect(box.height).toBeGreaterThanOrEqual(44);
  }
}
```

At 320px and 768px, run dedicated reflow cases without screenshots and repeat overflow/viewport assertions. On fine-pointer Chrome, assert an action row becomes visible on hover and focus; on mobile Chrome, assert it is visible before interaction. Measure controls against the first user bubble, attachment, action row, and prose, and measure each message action row against its owning content rectangle; every pair must report `intersects(...) === false`. Record 200% Chrome zoom, forced colors, reduced motion, keyboard-only, one screen-reader pass, and touch/coarse behavior in the review document; do not claim full WCAG conformance from automation.

- [ ] **Step 5: Capture current implementation goldens with `toHaveScreenshot`**

```ts
for (const visual of CASES) {
  test(`Calm Prism ${visual.name}`, async ({ page, baseURL }, testInfo) => {
    test.skip(
      isMobileCase(visual.name) !== (testInfo.project.name === "mobile-chrome"),
    );
    await page.setViewportSize(visual.viewport);
    const health = collectBrowserHealth(
      page,
      new URL(baseURL ?? "http://127.0.0.1:9080").origin,
    );
    await installCalmPrismFixture(page, visual.theme);
    await page.goto(`/c/${CONV_ID}`);
    await page.evaluate(() => document.fonts.ready);
    await expect(page).toHaveScreenshot(`calm-prism-${visual.name}.png`, {
      animations: "disabled",
      caret: "hide",
      fullPage: false,
    });
    health.assertClean();
  });
}
```

Generate the initial accepted baselines only after geometry and health assertions pass:

```powershell
cd web
npx playwright test e2e/chat-calm-prism.spec.ts --project=chrome --update-snapshots
npx playwright test e2e/chat-calm-prism.spec.ts --project=mobile-chrome --update-snapshots
npx playwright test e2e/chat-calm-prism.spec.ts --project=chrome
npx playwright test e2e/chat-calm-prism.spec.ts --project=mobile-chrome
```

Expected: four stable screenshots are written under `web/e2e/__screenshots__/chat-calm-prism.spec.ts/`; the second pair of runs PASS without updating them.

- [ ] **Step 6: Capture the same fixture from baseline commit `de700c425` and the implementation**

Create a read-only reference worktree and start its Vite server without showing a console window:

```powershell
git worktree add D:\Aura\.worktrees\calm-prism-chat-reference de700c425
cd D:\Aura\.worktrees\calm-prism-chat-reference\web
npm ci
$reference = Start-Process -FilePath npm.cmd -ArgumentList 'run','dev','--','--host','127.0.0.1','--port','4174' -WorkingDirectory (Get-Location) -WindowStyle Hidden -PassThru
```

Start the implementation Vite server on 4175 the same way. Run the screenshot-only capture mode from the implementation test suite twice, once with `AURA_E2E_ORIGIN=http://127.0.0.1:4174` and `AURA_VISUAL_LABEL=reference`, then with port 4175 and `AURA_VISUAL_LABEL=implementation`. The capture branch writes the exact six verification PNG paths listed in this task. Stop both saved processes with `Stop-Process -Id $reference.Id` and the implementation process ID; remove the reference worktree with `git worktree remove D:\Aura\.worktrees\calm-prism-chat-reference` only after verifying its path.

Use Playwright to build each same-input comparison: read the reference/current PNGs as base64, `page.setContent` a two-column figure labelled Reference and Calm Prism, and capture `comparison-*.png`. Inspect both comparison images before writing the verdict.

- [ ] **Step 7: Write the visual-review record with measured evidence**

Use this complete structure:

```markdown
# Calm Prism Chat Visual Review — 2026-07-15

## Inputs

- Baseline commit: `de700c425`
- Implementation commit: the `git rev-parse --short HEAD` value captured immediately before this review.
- Browser: installed Google Chrome through Playwright `channel: 'chrome'`
- States: desktop 1440 × 1000 dark/light; mobile 393 × 852 dark/light; 320px reflow

## Same-input comparisons

| State        | Reference                    | Implementation                    | Combined comparison           | Verdict                                                                                  |
| ------------ | ---------------------------- | --------------------------------- | ----------------------------- | ---------------------------------------------------------------------------------------- |
| Desktop dark | `reference-desktop-dark.png` | `implementation-desktop-dark.png` | `comparison-desktop-dark.png` | PASS after inspecting overlap, measure, hierarchy, focus, borders, radii, and typography |
| Mobile dark  | `reference-mobile-dark.png`  | `implementation-mobile-dark.png`  | `comparison-mobile-dark.png`  | PASS after inspecting containment, target size, footer cue, wrapping, and overflow       |

## Automated evidence

- Workspace/message rectangles: no intersections.
- Required mobile controls: all measured at least 44 × 44 CSS px.
- Horizontal overflow: none at 320, 393, 768, or 1440 CSS px.
- Browser health: no unallowlisted console, page, request, auth, polling, asset, or HTTP errors.

## Manual evidence limits

- 200% Chrome zoom/reflow: PASS, no lost content or two-dimensional page scroll.
- Keyboard-only and Escape/focus restoration: PASS.
- Reduced motion: PASS.
- Forced colors: PASS with visible focus and status text.
- Screen-reader smoke pass: settled telemetry announces once; approvals read before locked composer.
- Touch/coarse-pointer pass: all required actions visible without hover.

## Final verdict

PASS — no unresolved crop, overlap, spacing, typography, border, radius, focus, hierarchy, or truthfulness defect in the tested states. This is not a claim of full WCAG conformance.
```

Replace that descriptive implementation-commit line with the literal output of `git rev-parse --short HEAD` before committing the review record.

- [ ] **Step 8: Run the browser suite and commit Playwright evidence**

```powershell
cd web
npx playwright test e2e/chat.spec.ts e2e/chat-calm-prism.spec.ts --project=chrome
npx playwright test e2e/chat.spec.ts e2e/chat-calm-prism.spec.ts --project=mobile-chrome
cd ..
git diff --check -- web/playwright.config.ts web/e2e docs/superpowers/verification
git add -- web/playwright.config.ts web/e2e/chat.spec.ts web/e2e/support/browserHealth.ts web/e2e/chat-calm-prism.spec.ts web/e2e/__screenshots__ docs/superpowers/verification/2026-07-15-aura-calm-prism-chat-visual-review.md docs/superpowers/verification/2026-07-15-aura-calm-prism-chat
git commit -m "test(web): lock Calm Prism Chrome visuals"
```

Expected: both Chrome projects PASS and the commit includes only Playwright configuration/tests, accepted goldens, and verification evidence.

### Task 9: Run repository gates, build the embedded UI, and review only owned diffs

**Files:**

- Generated: `internal/webui/dist/**`
- Review: every file committed in Tasks 1-8

- [ ] **Step 1: Run targeted suites once more without snapshot updates**

```powershell
cd web
npx vitest run src/chat/__tests__/ReasoningDrawer.test.tsx src/chat/__tests__/ToolActivityCard.test.tsx src/approvals/__tests__/InlineApprovalCard.test.tsx src/approvals/__tests__/ThreadApprovalCards.test.tsx src/chat/__tests__/ExternalStoreChat.approvals.test.tsx src/chat/__tests__/EmptyThreadStarters.test.tsx src/chat/__tests__/runUsage.test.tsx src/chat/__tests__/RuntimeFooter.test.tsx src/chat/__tests__/Composer.test.tsx src/chat/__tests__/ExternalStoreChat.test.tsx src/i18n/__tests__/resources.parity.test.ts
```

Expected: all named suites PASS.

- [ ] **Step 2: Run all web static, unit, contrast, and browser gates**

```powershell
npm run typecheck
npm run lint
npm run format:check
npm run contrast
npm test
npm run test:e2e -- e2e/chat.spec.ts e2e/chat-calm-prism.spec.ts --project=chrome
npm run test:e2e -- e2e/chat.spec.ts e2e/chat-calm-prism.spec.ts --project=mobile-chrome
```

Expected: every command exits 0; Vitest retains the repository coverage threshold; Playwright reports no health collector failures or screenshot mismatches.

- [ ] **Step 3: Build the production bundle and inspect generated output separately**

```powershell
npm run build
cd ..
git status --short -- internal/webui/dist
git diff --stat -- internal/webui/dist
git diff --check -- internal/webui/dist
```

Expected: build exits 0, only expected generated bundle files change, and diff check is silent. Inspect the manifest and chunk names to ensure no new dependency or unexpected large main-chunk regression appeared.

- [ ] **Step 4: Run repository file-size and Go regression gates sequentially**

```powershell
bash scripts/check-file-size.sh
go test -p=2 ./...
```

Expected: file-size passes and all Go packages PASS. Keep Go and web coverage runs sequential on Windows to avoid paging-file/linker contention.

- [ ] **Step 5: Audit the complete commit and path ordering**

```powershell
git log --oneline --decorate -10
git diff --check de700c425..HEAD
git diff --name-only de700c425..HEAD
git status --short
```

Expected:

- The first implementation commit after the plan is the standalone Amendment #83 PRD commit.
- No backend/API/protocol/dependency file changed.
- Only owned source, tests, visual evidence, and generated `internal/webui/dist` paths appear.
- Unrelated user changes remain untouched.

- [ ] **Step 6: Commit the reviewed generated bundle**

```powershell
git add -- internal/webui/dist
git diff --cached --check
git commit -m "build(web): refresh Calm Prism cockpit bundle"
```

Expected: one generated-output-only commit.

- [ ] **Step 7: Perform the final no-regression check**

```powershell
git status --short
git log --oneline --decorate -12
cd web
npm run test:e2e -- e2e/chat-calm-prism.spec.ts --project=chrome
cd ..
```

Expected: no owned files remain uncommitted, unrelated pre-existing status is unchanged, and the final Chrome golden route PASSes against the built implementation.

## Final acceptance trace

- Amendment #83 and commit order: Task 1.
- Reserved controls, exact 48rem prose, fluid displays, containment, coarse targets: Task 2 plus Task 8 geometry.
- Storage precedence, closed default, unique disclosure IDs: Task 3.
- Seven tool states, XSS-safe raw text, locale duration: Task 4.
- Approval truthfulness, token transport, composer lock, focus, terminal tones, final-only resume: Task 5.
- Exact bilingual starters and sole nonce-backed draft owner: Task 6.
- Reset/live/settled telemetry for new/edit/reload/resume/cancel/failure/thread change: Task 7.
- Chrome collectors, responsive/theme states, screenshots, same-input review, manual evidence limits: Task 8.
- Typecheck, lint, format, contrast, full Vitest, Go regression, file cap, build, generated diff, owned-path audit: Task 9.
