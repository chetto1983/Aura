---
status: complete
quick_id: 260604-l9u
commit: 286bb47e
date: 2026-06-04
---

# Quick Task 260604-l9u Summary

Implemented `aura mcp doctor whatsapp` bridge preflight visibility.

## Changes

- Added a WhatsApp-only doctor health line after the MCP stdio server starts and lists tools.
- The health line probes `GET /api/send` on the local whatsmeow bridge and treats HTTP 405 as REST reachability success.
- Added `AURA_MCP_WHATSAPP_BRIDGE_URL` as a test/operator override; default remains `http://127.0.0.1:8080`.
- Reports connected-state as unavailable because the current bridge exposes no status endpoint.
- Added a focused `httptest` unit test for the new doctor output.

## Verification

- RED observed: `go test ./cmd/aura -run TestMCPDoctorWhatsAppReportsBridgeHealth -count=1` failed because the output lacked the WhatsApp bridge line.
- GREEN observed: `go test ./cmd/aura -run TestMCPDoctorWhatsAppReportsBridgeHealth -count=1` passed.
- Regression check: `go test ./cmd/aura -run TestMCPDoctorAndToolsStartConfiguredServer -count=1` passed.
- Package check: `go test ./cmd/aura` passed.
- Pre-commit hook on code commit ran gofmt, vet, and file-size checks successfully.

## Commit

- `286bb47e` - `fix(quick-260604-l9u): add whatsapp mcp doctor health`
