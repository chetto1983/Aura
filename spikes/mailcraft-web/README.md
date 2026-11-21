# Spike: MailCraft's web build as the cockpit's mail interface

Date: 2026-11-21. Asked by the operator: "Potremmo riusare la Ui di mail craft?", then "Sì, procedi
con lo spike", after the decision not to replace `aura-pim-mcp` with MailCraft and not to build a
mail interface of Aura's own first.

MailCraft (UCS-Care-Group/mailcraft, MIT OR Apache-2.0) is a Craft-family mail, calendar and
contacts client. Its browser build reaches only JMAP servers (RFC 8620, RFC 8621), and it offers a
hook for a page that serves it and has signed the user in itself: `window.mailcraftJmap`
(`apps/mailcraft-web/src/jmap.rs` upstream), which names the accounts and hands over a bearer token on
demand. The idea measured here: Aura serves the official build like the other Craft apps, declares
the identity's PIM accounts through that hook, and answers JMAP on its own origin from
`aura-pim-mcp`.

## Questions

1. Does the official web build start framed in the cockpit's page policy (`internal/staticapp`)?
2. Does `window.mailcraftJmap` work from a same-origin page: no credential asked, the token taken
   from the page?
3. Which JMAP calls, with which arguments, does it make to list, read, mark, move and send?
4. Which of them can `aura-pim-mcp` answer as it is, and what is missing?

## Method

- The official `mailcraft-web-0.14.0.zip` from the v0.14.0 release, checked against its
  `SHA256SUMS.txt`, unpacked as `docker/aura/Dockerfile` unpacks the Craft zips (`_headers` removed,
  wasm and glue gzipped), with one line added to its `index.html`:
  `<script src="/spike/mailcraft-config.js"></script>` before the module script.
- `serve/` serves the build through `internal/staticapp` under `/apps/mailcraft/`, the config script
  and a token on the same origin, a host page that frames the app as `CraftFrame` does, and a
  minimal JMAP server (`serve/jmap.go`, `serve/methods.go`, in memory, one account, six standard
  mailboxes, three messages) that writes every exchange to a JSON Lines file.
- The reference server was meant to be Stalwart. `stalwartlabs/stalwart:latest` is v1.1, whose
  setup is an interactive wizard (the documented non-interactive routes were not found); the legacy
  `stalwartlabs/mail-server` image could not be pulled (Docker Hub 429, GHCR denied). MailCraft's
  own strict test server (`crates/jmap/src/standin.rs`) cannot be put behind a socket from outside
  the crate (`HttpRequest::new` is `pub(crate)`). So the spike's server is its own, written from RFC
  8620/8621 and the stand-in's contract.
- `drive.mjs` drives the host page in headless Chromium (SwiftShader), `it-IT`.
- The PIM side was read from the fork (`chetto1983/aura-pim-mcp` at `bb87b828`): `IProviderService`,
  `EmailMessage`, `GetEmailsTool`; and from Aura (`internal/agent/mcptools/bridge_risk.go`, the
  deferred manifest's `calendar__calendar`).

```sh
go build -o /tmp/mcserve ./spikes/mailcraft-web/serve
/tmp/mcserve -apps <dir holding mailcraft/> -log calls.jsonl
node spikes/mailcraft-web/drive.mjs --out <dir> --steps 'wait:20000,shot:inbox,click:0.31:0.333,wait:3000,shot:open'
```

## Findings

See `FINDINGS.md`.
