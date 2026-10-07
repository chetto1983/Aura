# How Aura compares

Checked on 2026-10-03 against each project's own documentation. Open WebUI and
LibreChat are mature, much larger projects; this table shows where Aura differs, not
that it is ahead.

| | Aura | Open WebUI | LibreChat |
|---|---|---|---|
| **Backend** | Go, one binary + Compose appliance | Python | Node.js |
| **License** | MIT | Open WebUI License (BSD-3 up to v0.6.5; branding must stay above 50 users) | MIT |
| **Long-term memory** | Temporal knowledge graph: facts with sources and validity windows, one ArcadeDB database per identity | Facts and notes the model can search and update | Memory with per-agent partitions |
| **Scheduled work** | `task` tool (`at`, `every`, `cron`) running full agent jobs | Scheduled prompts | Scheduled Chats (beta) |
| **Tool approval (HITL)** | Yes | Not documented | Yes (v0.8.8) |
| **Messaging channels** | Telegram for two-way chat; WhatsApp and e-mail send messages and deliver scheduled-job results, but you cannot chat with Aura through them | Not documented | Not documented |
| **Video** | Generation plus a multi-track editor | Voice and video calls | Not documented |
| **Single sign-on** | No: email/password (TOTP for enrolled accounts) | SSO/OIDC, LDAP, SCIM | OAuth2, SAML, LDAP |
| **Community** | Small, one maintainer | Very large | Large |

Choose Open WebUI or LibreChat for a polished multi-model chat front end with SSO and
a large ecosystem. Choose Aura for a long-running personal agent that remembers over
time, works on a schedule and reaches you on Telegram or WhatsApp.
