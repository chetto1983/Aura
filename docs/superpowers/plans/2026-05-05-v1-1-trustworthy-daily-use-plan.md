# v1.1 Trustworthy Daily Use Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Aura safer and calmer for daily use by removing avoidable panics, surfacing quiet runtime failures, suppressing the packaged Windows console, and documenting dependency/platform watchpoints.

**Architecture:** Keep the current Go package layout and make narrow, testable changes at the failure boundaries. Use explicit errors instead of panic-style helpers, existing structured logging for observability, GoReleaser-only Windows GUI linker flags for packaged console suppression, and focused tests instead of broad refactors.

**Tech Stack:** Go, `log/slog`, SQLite tests with `modernc.org/sqlite`, Telebot v4, Windows tray via `fyne.io/systray`, GoReleaser v2, PowerShell release checks.

---

## File Structure

- Modify: `.planning/REQUIREMENTS.md` - add v1.1 requirements `PANIC-01`, `OBS-01`, `OBS-02`, `OBS-03`, `AUDIT-01`, `DEP-01`, `UX-01`, and `REL-02`.
- Modify: `.planning/ROADMAP.md` - replace the completed v1.0 roadmap with the active v1.1 phase list.
- Modify: `.planning/MILESTONES.md` - mark v1.1 as active and record its hardening boundary.
- Modify: `.planning/PROJECT.md` - add v1.1 to the current milestone section.
- Modify: `.planning/STATE.md` - set current position to v1.1 Phase 1.
- Modify: `docs/implementation-tracker.md` - append a v1.1 kickoff handoff and later task handoffs.
- Modify: `internal/toolsets/toolsets.go` - remove the panic-style resolver from production use.
- Modify: `internal/toolsets/toolsets_test.go` - prove invalid profile handling stays non-panic.
- Modify: `internal/scheduler/agent_job.go` - initialize scheduled-agent allowed tools without `MustResolveProfiles`.
- Modify: `internal/scheduler/agent_job_test.go` - cover allowed tool initialization and invalid enabled toolset behavior.
- Modify: `internal/telegram/bot.go` - log shutdown close failures.
- Modify: `internal/telegram/bot_test.go` or create `internal/telegram/stop_test.go` - cover shutdown close error logging with fakes.
- Modify: `internal/telegram/conversation.go` - log placeholder deletion failures at debug level.
- Modify: `internal/telegram/streaming_test.go` or create `internal/telegram/conversation_cleanup_test.go` - cover cleanup logging without breaking delivery.
- Modify: `internal/tray/tray.go` - expose a small URL validation helper if needed.
- Modify: `internal/tray/tray_windows.go` - validate dashboard URLs and log shell launch failures.
- Modify: `internal/tray/tray_other.go` - make non-Windows headless behavior explicit through the shared logger path if practical.
- Create: `internal/tray/browser.go` - pure helper for validating dashboard URLs.
- Create: `internal/tray/browser_test.go` - cross-platform tests for URL validation.
- Modify: `internal/auth/store.go` - return or log `last_used` audit update failures without rejecting valid tokens.
- Modify: `internal/auth/middleware.go` - log audit write failures if the store returns them separately.
- Modify: `internal/auth/store_test.go` - prove valid token lookup survives audit write failure.
- Modify: `.goreleaser.yml` - split Windows packaging or template linker flags so only packaged Windows `cmd/aura` uses the GUI subsystem.
- Create: `scripts/check-windows-gui-subsystem.ps1` - inspect packaged `aura.exe` PE subsystem and fail unless it is Windows GUI.
- Modify: `INSTALL.md` - explain packaged GUI behavior and where troubleshooting logs live.
- Create: `docs/telebot-v4-monitoring.md` - document pinned Telebot version, upgrade watchpoint, smoke checklist, and rollback expectation.
- Modify: `docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md` - check off tasks as they land.

## Task 1: Milestone State And Requirements

**Files:**
- Modify: `.planning/REQUIREMENTS.md`
- Modify: `.planning/ROADMAP.md`
- Modify: `.planning/MILESTONES.md`
- Modify: `.planning/PROJECT.md`
- Modify: `.planning/STATE.md`
- Modify: `docs/implementation-tracker.md`
- Modify: `docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md`

- [x] **Step 1: Update requirements**

Add a new `## Milestone v1.1 Requirements` section to `.planning/REQUIREMENTS.md` with this exact checklist:

```markdown
## Milestone v1.1 Requirements

v1.1 is limited to trustworthy daily-use hardening: no avoidable production panics, observable runtime cleanup failures, packaged Windows tray UX, dependency watchpoints, and a focused release gate.

- [ ] **PANIC-01 Toolset Profile Panic Removal:** Invalid or stale toolset profile names must not cause an unstructured process panic in production paths.
- [ ] **OBS-01 Shutdown Close Observability:** Shutdown close failures for long-lived services are logged with enough context to diagnose DB/client close problems.
- [ ] **OBS-02 Tray Browser-Open Observability:** Tray dashboard-open failures are visible to the operator, and invalid dashboard URLs are rejected before shell handoff.
- [ ] **OBS-03 Telegram Cleanup Observability:** Cosmetic Telegram cleanup failures, such as placeholder deletion during streaming, are observable at low severity.
- [ ] **AUDIT-01 Token Audit Update Observability:** Auth token `last_used` write failures are observable without denying an otherwise valid dashboard request.
- [ ] **DEP-01 Telebot Beta Monitoring:** Aura tracks the `gopkg.in/telebot.v4` beta dependency with a pinned-version review checklist and smoke expectations.
- [ ] **UX-01 Packaged Windows Console Suppression:** GoReleaser-produced Windows artifacts build `cmd/aura` as a GUI/tray-first binary without a console window while development and debug commands keep console output.
- [ ] **REL-02 Focused v1.1 Release Gate:** Focused package tests, broad Go verification, Windows GUI-subsystem package inspection, and any required manual smoke pass before tagging v1.1.
```

- [x] **Step 2: Update roadmap**

Replace `.planning/ROADMAP.md` with a v1.1 roadmap that contains these phases:

```markdown
# Roadmap: Aura v1.1 - Trustworthy Daily Use

**Created:** 2026-05-05
**Milestone:** v1.1 Trustworthy Daily Use
**Total phases:** 4

## Milestone Goal

Make Aura safer and calmer for daily use by removing avoidable panics, surfacing quiet runtime failures, suppressing the packaged Windows console, and documenting dependency/platform watchpoints.

## Boundary

In scope:
- Toolset profile panic removal
- Shutdown, tray/browser, Telegram cleanup, and token-audit observability
- Packaged Windows GUI/tray-first binary behavior
- Telebot v4 beta monitoring docs
- Focused v1.1 release gate

Out of scope:
- New user-facing features
- Broad large-file refactors
- Memory-quality upgrades
- Settings at-rest encryption
- A separate `aura-console.exe`

## Phases

### Phase 1: Panic Removal Gate

**Addresses:** PANIC-01
**Depends on:** -
**Success criteria:**
- No production path calls `MustResolveProfiles`.
- Invalid profile names return contextual errors instead of panicking.
- Focused tests cover invalid profile behavior.

### Phase 2: Production Error Observability

**Addresses:** OBS-01, OBS-02, OBS-03, AUDIT-01
**Depends on:** Phase 1
**Success criteria:**
- Shutdown close errors are logged.
- Tray browser-open failures are logged and unsafe URLs are rejected.
- Placeholder deletion failures are logged at low severity.
- Token `last_used` audit write failures are observable without denying valid tokens.

### Phase 3: Platform And Dependency Hygiene

**Addresses:** DEP-01, UX-01
**Depends on:** Phase 2
**Success criteria:**
- Packaged Windows `aura.exe` uses the Windows GUI subsystem.
- Development and debug commands keep console output.
- Telebot v4 beta monitoring docs define upgrade and rollback checks.

### Phase 4: Release Gate Lite

**Addresses:** REL-02
**Depends on:** Phases 1-3
**Success criteria:**
- Focused package tests pass.
- `go test ./...` and `go build ./...` pass.
- Snapshot Windows artifact passes GUI-subsystem inspection.
- Manual Windows smoke runs only where hermetic checks cannot prove behavior.
```

- [x] **Step 3: Update milestone/project/state docs**

Update `.planning/MILESTONES.md`, `.planning/PROJECT.md`, and `.planning/STATE.md` so they agree on:

```markdown
Current milestone: v1.1 Trustworthy Daily Use
Current phase: Phase 1 - Panic Removal Gate
Current focus: remove production panic paths before observability and packaged Windows UX work
```

- [x] **Step 4: Add tracker kickoff**

Append this handoff near the top of `docs/implementation-tracker.md`:

```markdown
2026-05-05 v1.1 kickoff handoff: `v1.1 Trustworthy Daily Use` is the active milestone. Scope is hardening-only: remove `MustResolveProfiles` production panic paths, surface shutdown/tray/Telegram cleanup/token-audit failures, suppress the packaged Windows console through GoReleaser only, document telebot v4 beta monitoring, and run a focused release gate. Next work: Phase 1 Panic Removal Gate.
```

- [x] **Step 5: Verify docs**

Run:

```powershell
git diff --check
```

Expected: exit code 0.

- [x] **Step 6: Commit docs**

```powershell
git add .planning/REQUIREMENTS.md .planning/ROADMAP.md .planning/MILESTONES.md .planning/PROJECT.md .planning/STATE.md docs/implementation-tracker.md docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md
git commit -m "docs: start v1.1 trustworthy daily use"
```

## Task 2: Panic Removal Gate

**Files:**
- Modify: `internal/toolsets/toolsets.go`
- Modify: `internal/toolsets/toolsets_test.go`
- Modify: `internal/scheduler/agent_job.go`
- Modify: `internal/scheduler/agent_job_test.go`
- Modify: `docs/implementation-tracker.md`
- Modify: `docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md`

- [x] **Step 1: Write the toolset non-panic test**

Add this test to `internal/toolsets/toolsets_test.go`:

```go
func TestMustResolveProfilesIsNotUsedForUnknownProfile(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ResolveProfiles panicked for unknown profile: %v", r)
		}
	}()
	if _, err := ResolveProfiles("not-a-profile"); err == nil {
		t.Fatal("ResolveProfiles unknown profile error = nil, want error")
	}
}
```

- [x] **Step 2: Write the scheduler allowed tools test**

Add this test to `internal/scheduler/agent_job_test.go`:

```go
func TestAgentJobAllowedToolsIncludesSkillsReadWithoutPanic(t *testing.T) {
	for _, want := range []string{"list_skills", "read_skill", "search_skill_catalog"} {
		if !containsString(AgentJobAllowedTools, want) {
			t.Fatalf("AgentJobAllowedTools missing %q: %+v", want, AgentJobAllowedTools)
		}
	}
}
```

- [x] **Step 3: Run tests before implementation**

Run:

```powershell
go test ./internal/toolsets ./internal/scheduler -count=1
```

Expected: tests pass or fail only if existing test files need import adjustment. The panic-risk target is static production usage, not a runtime failure in current tests.

- [x] **Step 4: Remove production `MustResolveProfiles` use**

In `internal/scheduler/agent_job.go`, replace:

```go
var DefaultAgentJobTools = toolsets.SchedulerSafeTools()
var AgentJobAllowedTools = appendUniqueStrings(DefaultAgentJobTools, toolsets.MustResolveProfiles(toolsets.ProfileSkillsRead)...)
```

with:

```go
var DefaultAgentJobTools = toolsets.SchedulerSafeTools()
var AgentJobAllowedTools = buildAgentJobAllowedTools()

func buildAgentJobAllowedTools() []string {
	skillTools, err := toolsets.ResolveProfiles(toolsets.ProfileSkillsRead)
	if err != nil {
		return append([]string(nil), DefaultAgentJobTools...)
	}
	return appendUniqueStrings(append([]string(nil), DefaultAgentJobTools...), skillTools...)
}
```

- [x] **Step 5: Remove the panic helper**

Delete this function from `internal/toolsets/toolsets.go`:

```go
func MustResolveProfiles(names ...string) []string {
	tools, err := ResolveProfiles(names...)
	if err != nil {
		panic(err)
	}
	return tools
}
```

- [x] **Step 6: Prove no production usage remains**

Run:

```powershell
rg "MustResolveProfiles" internal cmd
```

Expected: no output. Actual: local `rg` was unavailable/blocked with `Accesso negato`, so fallback static search was used:

```powershell
Get-ChildItem internal,cmd -Recurse -File | Select-String -Pattern 'MustResolveProfiles'
```

Result: no matches.

- [x] **Step 7: Run focused tests**

Run:

```powershell
go test ./internal/toolsets ./internal/scheduler -count=1
```

Expected: PASS.

- [x] **Step 8: Record handoff and commit**

Append to `docs/implementation-tracker.md`:

```markdown
2026-05-05 v1.1 Phase 1 handoff: `PANIC-01` landed. Production code no longer calls `MustResolveProfiles`; scheduled-agent allowed tools now initialize through `ResolveProfiles` without a bare panic path, and focused tests cover invalid profile behavior plus skills-read allowlist inclusion. Verification: local `rg` was unavailable/blocked with `Accesso negato`; fallback static search `Get-ChildItem internal,cmd -Recurse -File | Select-String -Pattern 'MustResolveProfiles'` returned no matches; `go test ./internal/toolsets ./internal/scheduler -count=1` passed. Next work: Phase 2 Production Error Observability.
```

Then commit:

```powershell
git add internal/toolsets/toolsets.go internal/toolsets/toolsets_test.go internal/scheduler/agent_job.go internal/scheduler/agent_job_test.go docs/implementation-tracker.md docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md
git commit -m "slice 1: remove toolset profile panic path"
```

## Task 3: Shutdown Close Observability

**Files:**
- Modify: `internal/telegram/bot.go`
- Create: `internal/telegram/stop_test.go`
- Modify: `docs/implementation-tracker.md`
- Modify: `docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md`

- [x] **Step 1: Write the shutdown logging test**

Create `internal/telegram/stop_test.go`:

```go
package telegram

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/aura/aura/internal/conversation"
)

func TestStopLogsArchiverCloseFailure(t *testing.T) {
	var logs bytes.Buffer
	b := &Bot{
		logger:   slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		archiver: &closeFailingArchiver{err: errors.New("close boom")},
	}

	b.Stop()

	got := logs.String()
	for _, want := range []string{"telegram shutdown: archiver close failed", `error="close boom"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("log %q does not contain %q", got, want)
		}
	}
}

type closeFailingArchiver struct {
	err error
}

func (f *closeFailingArchiver) Append(context.Context, conversation.Turn) error {
	return nil
}

func (f *closeFailingArchiver) Close(context.Context) error {
	return f.err
}
```

- [x] **Step 2: Run test to verify it fails**

Run:

```powershell
go test ./internal/telegram -run TestStopLogsArchiverCloseFailure -count=1
```

Expected: FAIL because `Bot.Stop` discards the archiver close error.

- [x] **Step 3: Implement archiver close logging**

In `internal/telegram/bot.go`, replace:

```go
if b.archiver != nil {
	_ = b.archiver.Close(context.Background())
}
for _, c := range b.mcpClients {
	_ = c.Close()
}
```

with:

```go
logger := b.logger
if logger == nil {
	logger = slog.Default()
}
if b.archiver != nil {
	if err := b.archiver.Close(context.Background()); err != nil {
		logger.Error("telegram shutdown: archiver close failed", "error", err)
	}
}
for i, c := range b.mcpClients {
	if c == nil {
		continue
	}
	if err := c.Close(); err != nil {
		logger.Error("telegram shutdown: mcp client close failed", "index", i, "error", err)
	}
}
```

Add `"log/slog"` only if the file does not already import it. `bot.go` already imports `log/slog`, so no import change should be needed.

- [x] **Step 4: Run focused Telegram test**

Run:

```powershell
go test ./internal/telegram -run TestStopLogsArchiverCloseFailure -count=1
```

Expected: PASS.

- [x] **Step 5: Run Telegram package tests**

Run:

```powershell
go test ./internal/telegram -count=1
```

Expected: PASS.

- [x] **Step 6: Record handoff and commit**

Append to `docs/implementation-tracker.md`:

```markdown
2026-05-05 v1.1 Phase 2a handoff: `OBS-01` started. `Bot.Stop` now logs archiver and MCP client close failures instead of discarding them, while successful shutdown remains unchanged. Verification: `go test ./internal/telegram -run TestStopLogsArchiverCloseFailure -count=1` and `go test ./internal/telegram -count=1`. Next work: tray/browser-open observability.
```

Then commit:

```powershell
git add internal/telegram/bot.go internal/telegram/stop_test.go docs/implementation-tracker.md docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md
git commit -m "slice 2a: log telegram shutdown close failures"
```

## Task 4: Tray Browser-Open Observability

**Files:**
- Create: `internal/tray/browser.go`
- Create: `internal/tray/browser_test.go`
- Modify: `internal/tray/tray_windows.go`
- Modify: `internal/tray/tray_other.go`
- Modify: `docs/implementation-tracker.md`
- Modify: `docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md`

- [ ] **Step 1: Write URL validation tests**

Create `internal/tray/browser_test.go`:

```go
package tray

import "testing"

func TestValidateDashboardURLAcceptsHTTPAndHTTPS(t *testing.T) {
	for _, raw := range []string{
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"https://example.local/dashboard",
	} {
		if _, err := validateDashboardURL(raw); err != nil {
			t.Fatalf("validateDashboardURL(%q): %v", raw, err)
		}
	}
}

func TestValidateDashboardURLRejectsUnsafeValues(t *testing.T) {
	for _, raw := range []string{
		"",
		"javascript:alert(1)",
		"file:///C:/Windows/System32/calc.exe",
		"http://",
		"://missing-scheme",
	} {
		if _, err := validateDashboardURL(raw); err == nil {
			t.Fatalf("validateDashboardURL(%q) error = nil, want error", raw)
		}
	}
}
```

- [ ] **Step 2: Run tray tests to verify failure**

Run:

```powershell
go test ./internal/tray -count=1
```

Expected: FAIL because `validateDashboardURL` does not exist.

- [ ] **Step 3: Add URL validation helper**

Create `internal/tray/browser.go`:

```go
package tray

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func validateDashboardURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("dashboard url required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse dashboard url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported dashboard url scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("dashboard url host required")
	}
	return u.String(), nil
}
```

- [ ] **Step 4: Add Windows browser-open logging**

In `internal/tray/tray_windows.go`, add imports:

```go
	"log/slog"
```

Replace:

```go
func openBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
```

with:

```go
func openBrowser(rawURL string) {
	u, err := validateDashboardURL(rawURL)
	if err != nil {
		slog.Warn("tray: refusing to open dashboard url", "url", rawURL, "error", err)
		return
	}
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start(); err != nil {
		slog.Warn("tray: open dashboard failed", "url", u, "error", err)
	}
}
```

- [ ] **Step 5: Make non-Windows tray behavior explicit**

In `internal/tray/tray_other.go`, replace:

```go
import "sync"
```

with:

```go
import (
	"log/slog"
	"sync"
)
```

Replace:

```go
func run(_ Options) error {
	<-stopCh
	return nil
}
```

with:

```go
func run(opts Options) error {
	if opts.DashboardURL != "" {
		slog.Info("tray: platform not supported, running headless", "dashboard_url", opts.DashboardURL)
	} else {
		slog.Info("tray: platform not supported, running headless")
	}
	<-stopCh
	return nil
}
```

- [ ] **Step 6: Run tray tests**

Run:

```powershell
go test ./internal/tray -count=1
```

Expected: PASS.

- [ ] **Step 7: Record handoff and commit**

Append to `docs/implementation-tracker.md`:

```markdown
2026-05-05 v1.1 Phase 2b handoff: `OBS-02` landed. Tray browser-open now validates dashboard URLs before Windows shell handoff, logs refused/failed opens, and non-Windows tray startup logs headless mode. Verification: `go test ./internal/tray -count=1`. Next work: Telegram cleanup observability.
```

Then commit:

```powershell
git add internal/tray/browser.go internal/tray/browser_test.go internal/tray/tray_windows.go internal/tray/tray_other.go docs/implementation-tracker.md docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md
git commit -m "slice 2b: surface tray browser-open failures"
```

## Task 5: Telegram Placeholder Cleanup Observability

**Files:**
- Modify: `internal/telegram/conversation.go`
- Create: `internal/telegram/conversation_cleanup_test.go`
- Modify: `docs/implementation-tracker.md`
- Modify: `docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md`

- [ ] **Step 1: Extract cleanup helper test**

Create `internal/telegram/conversation_cleanup_test.go`:

```go
package telegram

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	tele "gopkg.in/telebot.v4"
)

func TestLogPlaceholderDeleteFailure(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	logPlaceholderDeleteFailure(logger, "123", &tele.Message{ID: 99}, errors.New("telegram delete failed"))

	got := logs.String()
	for _, want := range []string{
		"telegram cleanup: placeholder delete failed",
		"user_id=123",
		"message_id=99",
		`error="telegram delete failed"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log %q does not contain %q", got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run:

```powershell
go test ./internal/telegram -run TestLogPlaceholderDeleteFailure -count=1
```

Expected: FAIL because `logPlaceholderDeleteFailure` does not exist.

- [ ] **Step 3: Add helper and use it**

In `internal/telegram/conversation.go`, add this helper near `handleConversation`:

```go
func logPlaceholderDeleteFailure(logger *slog.Logger, userID string, placeholder *tele.Message, err error) {
	if err == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	args := []any{"user_id", userID, "error", err}
	if placeholder != nil {
		args = append(args, "message_id", placeholder.ID)
	}
	logger.Debug("telegram cleanup: placeholder delete failed", args...)
}
```

Then replace:

```go
if placeholder != nil {
	_ = c.Bot().Delete(placeholder)
}
```

with:

```go
if placeholder != nil {
	if err := c.Bot().Delete(placeholder); err != nil {
		logPlaceholderDeleteFailure(b.logger, userID, placeholder, err)
	}
}
```

- [ ] **Step 4: Run focused tests**

Run:

```powershell
go test ./internal/telegram -run "TestLogPlaceholderDeleteFailure|TestConsumeStream" -count=1
```

Expected: PASS.

- [ ] **Step 5: Run Telegram package tests**

Run:

```powershell
go test ./internal/telegram -count=1
```

Expected: PASS.

- [ ] **Step 6: Record handoff and commit**

Append to `docs/implementation-tracker.md`:

```markdown
2026-05-05 v1.1 Phase 2c handoff: `OBS-03` landed. Placeholder deletion failures during non-streamed response cleanup now log at debug level with user/message context without failing delivery. Verification: `go test ./internal/telegram -run "TestLogPlaceholderDeleteFailure|TestConsumeStream" -count=1` and `go test ./internal/telegram -count=1`. Next work: auth token audit observability.
```

Then commit:

```powershell
git add internal/telegram/conversation.go internal/telegram/conversation_cleanup_test.go docs/implementation-tracker.md docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md
git commit -m "slice 2c: log telegram cleanup failures"
```

## Task 6: Auth Token Audit Observability

**Files:**
- Modify: `internal/auth/store.go`
- Modify: `internal/auth/middleware.go`
- Modify: `internal/auth/store_test.go`
- Modify: `docs/implementation-tracker.md`
- Modify: `docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md`

- [ ] **Step 1: Add audit-error type and test**

Add this test to `internal/auth/store_test.go`:

```go
func TestLookupReturnsUserWhenLastUsedUpdateFails(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tok, err := s.Issue(ctx, "u1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `DROP TABLE api_tokens`); err != nil {
		t.Fatalf("drop api_tokens: %v", err)
	}

	userID, err := s.Lookup(ctx, tok)
	if err == nil {
		t.Fatal("lookup err = nil, want query failure after dropped table")
	}
	if userID != "" {
		t.Fatalf("userID = %q, want empty when primary lookup fails", userID)
	}
}
```

This test documents that primary lookup failure still fails. The next test covers audit update failure through a hook.

Add a second test:

```go
func TestLookupSurfacesLastUsedAuditFailureWithoutDenyingToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tok, err := s.Issue(ctx, "u1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	s.updateLastUsed = func(context.Context, string, string) error {
		return errors.New("database locked")
	}

	userID, err := s.Lookup(ctx, tok)
	if userID != "u1" {
		t.Fatalf("userID = %q, want u1", userID)
	}
	var auditErr *AuditUpdateError
	if !errors.As(err, &auditErr) {
		t.Fatalf("err = %v, want AuditUpdateError", err)
	}
}
```

- [ ] **Step 2: Run auth tests to verify failure**

Run:

```powershell
go test ./internal/auth -run TestLookupSurfacesLastUsedAuditFailureWithoutDenyingToken -count=1
```

Expected: FAIL because `updateLastUsed` and `AuditUpdateError` do not exist.

- [ ] **Step 3: Add audit update hook and error type**

In `internal/auth/store.go`, add this type near the existing error vars:

```go
type AuditUpdateError struct {
	UserID string
	Err    error
}

func (e *AuditUpdateError) Error() string {
	return "auth: token audit update failed"
}

func (e *AuditUpdateError) Unwrap() error {
	return e.Err
}
```

Change `Store` from:

```go
type Store struct {
	db       *sql.DB
	now      func() time.Time
	tokenTTL time.Duration
	owned    bool
}
```

to:

```go
type Store struct {
	db             *sql.DB
	now            func() time.Time
	tokenTTL       time.Duration
	owned          bool
	updateLastUsed func(context.Context, string, string) error
}
```

Add this method:

```go
func defaultUpdateLastUsed(db *sql.DB) func(context.Context, string, string) error {
	return func(ctx context.Context, now, hash string) error {
		_, err := db.ExecContext(ctx, `UPDATE api_tokens SET last_used = ? WHERE token_hash = ?`, now, hash)
		return err
	}
}
```

Update both constructors to set `updateLastUsed: defaultUpdateLastUsed(db)`.

- [ ] **Step 4: Return audit error without denying the user**

In `Lookup`, replace:

```go
_, _ = s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used = ? WHERE token_hash = ?`, now, hash)
return userID, nil
```

with:

```go
if s.updateLastUsed == nil {
	s.updateLastUsed = defaultUpdateLastUsed(s.db)
}
if err := s.updateLastUsed(ctx, now, hash); err != nil {
	return userID, &AuditUpdateError{UserID: userID, Err: err}
}
return userID, nil
```

- [ ] **Step 5: Make middleware log audit errors and continue**

In `internal/auth/middleware.go`, after:

```go
userID, err := store.Lookup(r.Context(), token)
if err != nil {
```

add this first branch:

```go
	var auditErr *AuditUpdateError
	if errors.As(err, &auditErr) {
		logger.Warn("auth: token audit update failed", "user_id", auditErr.UserID, "error", auditErr.Err)
		err = nil
	}
```

Then wrap the existing error handling in:

```go
if err != nil {
	// existing expired/invalid handling
}
```

Expected final shape:

```go
userID, err := store.Lookup(r.Context(), token)
if err != nil {
	var auditErr *AuditUpdateError
	if errors.As(err, &auditErr) {
		logger.Warn("auth: token audit update failed", "user_id", auditErr.UserID, "error", auditErr.Err)
		err = nil
	}
}
if err != nil {
	// existing expired/invalid handling
}
```

- [ ] **Step 6: Run focused auth tests**

Run:

```powershell
go test ./internal/auth -run "TestLookup.*Audit|TestIssueLookup_RoundTrip" -count=1
```

Expected: PASS.

- [ ] **Step 7: Run API/auth package tests**

Run:

```powershell
go test ./internal/auth ./internal/api -count=1
```

Expected: PASS.

- [ ] **Step 8: Record handoff and commit**

Append to `docs/implementation-tracker.md`:

```markdown
2026-05-05 v1.1 Phase 2d handoff: `AUDIT-01` landed. Auth token lookup now surfaces `last_used` audit update failures through `AuditUpdateError`; middleware logs that warning and continues valid requests without leaking token material. Verification: `go test ./internal/auth -run "TestLookup.*Audit|TestIssueLookup_RoundTrip" -count=1` and `go test ./internal/auth ./internal/api -count=1`. Next work: packaged Windows console suppression and dependency monitoring.
```

Then commit:

```powershell
git add internal/auth/store.go internal/auth/middleware.go internal/auth/store_test.go docs/implementation-tracker.md docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md
git commit -m "slice 2d: surface token audit update failures"
```

## Task 7: Packaged Windows Console Suppression

**Files:**
- Modify: `.goreleaser.yml`
- Create: `scripts/check-windows-gui-subsystem.ps1`
- Modify: `INSTALL.md`
- Modify: `docs/implementation-tracker.md`
- Modify: `docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md`

- [ ] **Step 1: Create the PE subsystem check script**

Create `scripts/check-windows-gui-subsystem.ps1`:

```powershell
param(
  [Parameter(Mandatory=$true)]
  [string]$Path
)

$resolved = Resolve-Path -LiteralPath $Path
$bytes = [System.IO.File]::ReadAllBytes($resolved)
if ($bytes.Length -lt 0x100) {
  throw "file too small to be a PE executable: $resolved"
}

$peOffset = [BitConverter]::ToInt32($bytes, 0x3C)
if ($peOffset -lt 0 -or $peOffset + 0x5C -ge $bytes.Length) {
  throw "invalid PE header offset in $resolved"
}

$signature = [System.Text.Encoding]::ASCII.GetString($bytes, $peOffset, 4)
if ($signature -ne "PE`0`0") {
  throw "missing PE signature in $resolved"
}

$optionalHeaderOffset = $peOffset + 24
$subsystemOffset = $optionalHeaderOffset + 68
$subsystem = [BitConverter]::ToUInt16($bytes, $subsystemOffset)

switch ($subsystem) {
  2 { "windows gui subsystem ok: $resolved"; exit 0 }
  3 { throw "windows console subsystem found in $resolved" }
  default { throw "unexpected PE subsystem $subsystem in $resolved" }
}
```

- [ ] **Step 2: Update GoReleaser builds**

In `.goreleaser.yml`, replace the single `builds` entry with two build entries:

```yaml
builds:
  - id: aura
    main: ./cmd/aura
    binary: aura
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w
      - -X main.auraVersion={{.Version}}
      - -X main.commit={{.Commit}}
      - -X main.date={{.Date}}

  - id: aura-windows
    main: ./cmd/aura
    binary: aura
    env:
      - CGO_ENABLED=0
    goos:
      - windows
    goarch:
      - amd64
    ldflags:
      - -s -w
      - -H=windowsgui
      - -X main.auraVersion={{.Version}}
      - -X main.commit={{.Commit}}
      - -X main.date={{.Date}}
```

Then add the build IDs to the archive:

```yaml
archives:
  - id: aura
    builds:
      - aura
      - aura-windows
```

Keep the existing archive `name_template`, `format_overrides`, and `files` under the same archive entry.

- [ ] **Step 3: Update install troubleshooting**

In `INSTALL.md`, replace the Windows run instruction:

```markdown
**Windows (PowerShell):** `.\aura_windows_amd64.exe`
```

with:

```markdown
**Windows:** double-click `aura.exe` from the extracted release folder, or run `.\aura.exe` from PowerShell. The packaged Windows app runs as a tray-first GUI binary, so it does not keep a console window open during normal use.
```

In the Troubleshooting section, add:

```markdown
**Windows: I do not see a console window**
This is expected for packaged releases. Aura writes logs under the configured `LOG_DIR` (default: `./logs`). For development troubleshooting, run from source with `go run ./cmd/aura` or use the debug commands; those still print to the console.
```

- [ ] **Step 4: Build snapshot package**

Run:

```powershell
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
```

Expected: PASS and a Windows ZIP appears under `dist`.

- [ ] **Step 5: Inspect packaged subsystem**

Run:

```powershell
$zip = Get-ChildItem dist -Filter 'aura_*_windows_x86_64.zip' | Select-Object -First 1
$dest = Join-Path $env:TEMP 'aura-gui-subsystem-check'
Remove-Item -LiteralPath $dest -Recurse -Force -ErrorAction SilentlyContinue
Expand-Archive -LiteralPath $zip.FullName -DestinationPath $dest -Force
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\check-windows-gui-subsystem.ps1 -Path (Join-Path $dest 'aura.exe')
```

Expected: prints `windows gui subsystem ok: <path>`.

- [ ] **Step 6: Verify dev build remains console-friendly**

Run:

```powershell
go build -o .codex\tmp\aura-dev-console.exe ./cmd/aura
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\check-windows-gui-subsystem.ps1 -Path .codex\tmp\aura-dev-console.exe
```

Expected: the check fails with `windows console subsystem found`, proving local developer builds did not inherit the packaged GUI subsystem flag. Do not treat this expected failure as a task failure.

- [ ] **Step 7: Record handoff and commit**

Append to `docs/implementation-tracker.md`:

```markdown
2026-05-05 v1.1 Phase 3a handoff: `UX-01` landed. GoReleaser now builds packaged Windows `aura.exe` with the GUI subsystem while local developer builds remain console-friendly. Added PE subsystem inspection script and INSTALL troubleshooting notes. Verification: GoReleaser snapshot release passed, packaged `aura.exe` printed `windows gui subsystem ok`, and a local dev build intentionally failed the GUI check as `windows console subsystem found`. Next work: telebot beta monitoring docs.
```

Then commit:

```powershell
git add .goreleaser.yml scripts/check-windows-gui-subsystem.ps1 INSTALL.md docs/implementation-tracker.md docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md
git commit -m "slice 3a: suppress packaged windows console"
```

## Task 8: Telebot Beta Monitoring Docs

**Files:**
- Create: `docs/telebot-v4-monitoring.md`
- Modify: `.planning/codebase/CONCERNS.md`
- Modify: `docs/implementation-tracker.md`
- Modify: `docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md`

- [ ] **Step 1: Create monitoring doc**

Create `docs/telebot-v4-monitoring.md`:

```markdown
# Telebot v4 Monitoring

Date: 2026-05-05
Dependency: `gopkg.in/telebot.v4 v4.0.0-beta.7`

## Why This Is Tracked

Telegram is Aura's primary user interface. Aura currently uses a beta Telebot v4 release, so upgrades must be deliberate and smoke-tested instead of automatic.

## Pinned Version

`go.mod` pins `gopkg.in/telebot.v4 v4.0.0-beta.7`.

Do not upgrade this dependency as part of unrelated feature work.

## Upgrade Watchpoint

Before upgrading Telebot:

1. Read upstream release notes or commit history for breaking API changes.
2. Run `go test ./internal/telegram -count=1`.
3. Run `go test ./...`.
4. Run a live Telegram smoke with `/start`, normal text conversation, streaming response, document upload, dashboard token request, and generated artifact delivery.
5. Keep the previous `go.mod` and `go.sum` diff available for rollback.

## Rollback Expectation

If live Telegram smoke fails after an upgrade, revert only the Telebot dependency change and any required API adaptation commit. Do not revert unrelated Aura feature work.
```

- [ ] **Step 2: Update concerns status**

In `.planning/codebase/CONCERNS.md`, change the Telebot dependency section from an open recommendation to:

```markdown
**Beta Telegram library:**
- Status: **Monitored for v1.1.** See `docs/telebot-v4-monitoring.md`.
- Issue: `gopkg.in/telebot.v4 v4.0.0-beta.7` powers all user interaction and is still a beta release.
- Risk: API-breaking changes can require significant refactoring with no deprecation period.
- Current policy: keep the pinned beta version until an explicit upgrade slice runs the documented Telegram smoke checklist.
```

- [ ] **Step 3: Verify doc references current dependency**

Run:

```powershell
Select-String -Path go.mod,docs\telebot-v4-monitoring.md -Pattern 'gopkg.in/telebot.v4'
```

Expected: both files show `v4.0.0-beta.7`.

- [ ] **Step 4: Record handoff and commit**

Append to `docs/implementation-tracker.md`:

```markdown
2026-05-05 v1.1 Phase 3b handoff: `DEP-01` landed. Telebot v4 beta usage is now intentionally tracked with pinned-version notes, upgrade smoke expectations, and rollback policy. Verification: `Select-String -Path go.mod,docs\telebot-v4-monitoring.md -Pattern 'gopkg.in/telebot.v4'`. Next work: v1.1 release gate.
```

Then commit:

```powershell
git add docs/telebot-v4-monitoring.md .planning/codebase/CONCERNS.md docs/implementation-tracker.md docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md
git commit -m "docs: monitor telebot beta dependency"
```

## Task 9: v1.1 Release Gate Lite

**Files:**
- Modify: `.planning/REQUIREMENTS.md`
- Modify: `.planning/MILESTONES.md`
- Modify: `.planning/PROJECT.md`
- Modify: `.planning/STATE.md`
- Modify: `docs/implementation-tracker.md`
- Modify: `docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md`

- [ ] **Step 1: Run focused package tests**

Run:

```powershell
go test ./internal/toolsets ./internal/scheduler ./internal/telegram ./internal/tray ./internal/auth ./internal/api -count=1
```

Expected: PASS.

- [ ] **Step 2: Run broad Go verification**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1
```

Expected: PASS for `go fmt ./...`, `go test ./...`, `go build ./...`, and `go vet ./...`.

- [ ] **Step 3: Run snapshot packaging**

Run:

```powershell
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
```

Expected: PASS.

- [ ] **Step 4: Inspect Windows GUI subsystem**

Run:

```powershell
$zip = Get-ChildItem dist -Filter 'aura_*_windows_x86_64.zip' | Select-Object -First 1
$dest = Join-Path $env:TEMP 'aura-v1-1-gate'
Remove-Item -LiteralPath $dest -Recurse -Force -ErrorAction SilentlyContinue
Expand-Archive -LiteralPath $zip.FullName -DestinationPath $dest -Force
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\check-windows-gui-subsystem.ps1 -Path (Join-Path $dest 'aura.exe')
```

Expected: prints `windows gui subsystem ok: <path>`.

- [ ] **Step 5: Mark v1.1 requirements complete**

In `.planning/REQUIREMENTS.md`, mark all v1.1 requirements `[x]` only after the relevant tasks and release gate pass.

- [ ] **Step 6: Mark milestone complete**

Update `.planning/MILESTONES.md`, `.planning/PROJECT.md`, `.planning/STATE.md`, and `docs/implementation-tracker.md` with:

```markdown
v1.1 Trustworthy Daily Use: complete.
Release gates passed:
- focused package tests
- full Go verifier
- GoReleaser snapshot package
- Windows GUI subsystem inspection
```

- [ ] **Step 7: Commit closure docs**

```powershell
git add .planning/REQUIREMENTS.md .planning/MILESTONES.md .planning/PROJECT.md .planning/STATE.md docs/implementation-tracker.md docs/superpowers/plans/2026-05-05-v1-1-trustworthy-daily-use-plan.md
git commit -m "docs: complete v1.1 trustworthy daily use"
```

## Self-Review

- Spec coverage: `PANIC-01` is Task 2; `OBS-01` is Task 3; `OBS-02` is Task 4; `OBS-03` is Task 5; `AUDIT-01` is Task 6; `UX-01` is Task 7; `DEP-01` is Task 8; `REL-02` is Task 9.
- Placeholder scan: no unresolved placeholder markers or vague implementation instructions.
- Type consistency: planned function names are `validateDashboardURL`, `logPlaceholderDeleteFailure`, `AuditUpdateError`, and `defaultUpdateLastUsed`; later tasks use the same names.
- Scope check: the plan stays hardening-only and does not add memory-quality features, settings encryption, broad file splits, or a separate console binary.
