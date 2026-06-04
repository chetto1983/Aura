---
status: complete
quick_id: 260604-l9u
commit: 74790921
date: 2026-06-04
---

# Quick Task 260604-l9u Summary

Implemented `aura mcp doctor whatsapp` bridge preflight visibility.

## Changes

- Added a WhatsApp-only doctor health line after the MCP stdio server starts and lists tools.
- The health line probes `GET /api/send` on the local whatsmeow bridge and treats HTTP 405 as REST reachability success.
- Added `AURA_MCP_WHATSAPP_BRIDGE_URL` as a test/operator override; default remains `http://127.0.0.1:8080`.
- Follow-up E2E fix: when the configured WhatsApp MCP server runs through `wsl.exe`, the doctor probes REST `:8080` from inside WSL so Windows host port collisions do not hide a healthy bridge.
- Reports connected-state as unavailable because the current bridge exposes no status endpoint.
- Added focused tests for the direct HTTP override and WSL-recipe probe path.

## Verification

- RED observed: `go test ./cmd/aura -run TestMCPDoctorWhatsAppReportsBridgeHealth -count=1` failed because the output lacked the WhatsApp bridge line.
- GREEN observed: `go test ./cmd/aura -run TestMCPDoctorWhatsAppReportsBridgeHealth -count=1` passed.
- Regression check: `go test ./cmd/aura -run TestMCPDoctorAndToolsStartConfiguredServer -count=1` passed.
- E2E validation before follow-up fix: `go run ./cmd/aura mcp doctor whatsapp` started the MCP server and listed 12 tools, but host-side `127.0.0.1:8080` returned 404 while WSL-side `127.0.0.1:8080/api/send` returned 405.
- Follow-up regression check: `go test ./cmd/aura -run TestProbeWhatsAppBridgeUsesWSLForWSLRecipe -count=1` passed.
- Live E2E after follow-up fix: `go run ./cmd/aura mcp doctor whatsapp` printed `ok: whatsapp started; 12 tools` and `whatsapp bridge: REST :8080 in WSL reachable (GET /api/send -> 405); connected-state unavailable (bridge exposes no status endpoint)`.
- Package check: `go test ./cmd/aura` passed.
- Pre-commit hook on code commit ran gofmt, vet, and file-size checks successfully.

## Commit

- `286bb47e` - `fix(quick-260604-l9u): add whatsapp mcp doctor health`
- `74790921` - `fix(quick-260604-l9u): probe whatsapp bridge from wsl`
