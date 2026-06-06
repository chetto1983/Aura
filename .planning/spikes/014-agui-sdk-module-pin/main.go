//go:build spike_agui

// Spike 014 — agui-sdk-module-pin.
// First-import proof of github.com/ag-ui-protocol/ag-ui/sdks/community/go
// pinned at e9e910b230b9329c905e31ca024b4114dedf7918 (2026-05-14).
//
// Requires the dependency in go.mod (the spike session ran:
//
//	go get github.com/ag-ui-protocol/ag-ui/sdks/community/go@e9e910b230b9329c905e31ca024b4114dedf7918
//
// which is reverted at session end — re-run it before re-running this harness).
//
// Run: go run -tags spike_agui ./.planning/spikes/014-agui-sdk-module-pin
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

const wantVersion = "v0.0.0-20260514093510-e9e910b230b9"

func logf(cat, format string, args ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), cat, fmt.Sprintf(format, args...))
}

func main() {
	fail := func(format string, args ...any) {
		logf("SUMMARY", "INVALIDATED — "+format, args...)
		os.Exit(1)
	}

	// 1. The module resolves and this binary embeds the pinned pseudo-version.
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		fail("no build info")
	}
	got := ""
	for _, dep := range bi.Deps {
		if dep.Path == "github.com/ag-ui-protocol/ag-ui/sdks/community/go" {
			got = dep.Version
		}
	}
	logf("MODULE", "resolved version: %s", got)
	if got != wantVersion {
		fail("version %q != pinned %q", got, wantVersion)
	}

	// 2. The API is usable: construct + validate + marshal one event per family probe.
	ev := events.NewTextMessageContentEvent("msg-1", "Ciao")
	if err := ev.Validate(); err != nil {
		fail("TextMessageContent Validate: %v", err)
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		fail("marshal: %v", err)
	}
	logf("EVENT", "TEXT_MESSAGE_CONTENT wire: %s", raw)

	run := events.NewRunStartedEvent("thread-1", "run-1")
	rawRun, err := json.Marshal(run)
	if err != nil {
		fail("marshal run: %v", err)
	}
	logf("EVENT", "RUN_STARTED wire: %s", rawRun)

	// 3. REASONING_* family exists at this pin (amendment #33 dependency).
	logf("EVENT", "reasoning family constants: %s %s %s",
		events.EventTypeReasoningStart, events.EventTypeReasoningMessageContent, events.EventTypeReasoningEnd)

	logf("SUMMARY", "VALIDATED — pin %s resolves, builds, events construct+marshal", wantVersion)
}
