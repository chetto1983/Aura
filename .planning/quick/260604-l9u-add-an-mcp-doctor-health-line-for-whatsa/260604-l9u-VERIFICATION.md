---
status: passed
quick_id: 260604-l9u
date: 2026-06-04
---

# Quick Task 260604-l9u Verification

## Goal

Add an MCP doctor health line for WhatsApp REST `:8080` plus connected-state visibility.

## Result

Passed.

## Evidence

- `cmd/aura/mcp.go` now appends a WhatsApp-only bridge health line from `mcpDoctor`.
- The probe targets `/api/send`, where HTTP 405 means the REST bridge is reachable.
- For the WSL-based WhatsApp recipe, the probe runs inside WSL, matching the MCP server's own view of `localhost:8080`.
- The output explicitly says connected-state is unavailable because the bridge has no status endpoint.
- `cmd/aura/mcp_test.go` covers both the direct HTTP override and WSL probe selection.

## Tests

- `go test ./cmd/aura -run TestMCPDoctorWhatsAppReportsBridgeHealth -count=1` passed after implementation.
- `go test ./cmd/aura -run TestProbeWhatsAppBridgeUsesWSLForWSLRecipe -count=1` passed after the E2E follow-up fix.
- `go run ./cmd/aura mcp doctor whatsapp` passed live side-effect-free E2E: MCP started with 12 tools and the WSL REST bridge returned 405.
- `go test ./cmd/aura -run TestMCPDoctorAndToolsStartConfiguredServer -count=1` passed.
- `go test ./cmd/aura` passed.
