# Phase03 Source Audit

| Source | Decision Supported | Adopt | Reject / Avoid | Status |
| --- | --- | --- | --- | --- |
| `D:/Aura/prd.md` Phase 3 | Channel migration order | Use Chat as normalized layer | Direct channel-to-agent paths | read |
| `D:/Aura/docs/chat-interface-prd.md` | Chat Hub contracts | Preserve normalized inbound/outbound model | UI-only interpretation | read during Phase03/Phase01B repairs |
| `D:/Aura/internal/chat/` | Current chat hub | Extend existing package | Rename churn | read during Phase03/Phase01B repairs |
| `D:/Aura/internal/channels/telegram/`, `internal/channels/web/` | Adapter targets | Route behind tests/flags | Default-on migration | read during Phase03/Phase01B/Phase01C repairs |
| `D:/Aura/internal/api/` | `/api/chat` shape | Keep JSON stable | API breaking change | read during Phase01B web Hub repair |

## Missing Source Questions

None for the closed Telegram-streaming arc. Future channel work must name its
own source rows before changing default behavior.
