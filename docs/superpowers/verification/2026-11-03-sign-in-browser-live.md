# Sign-in browser — 2026-11-03 live acceptance

Status: remaining resource, crash-cleanup and release gates in progress.

## Operator acceptance and retained scope

The appliance acceptance revision is `dd053799714beb60515e9976cd110a2d89ad877a`.
The operator accepted Linear and Notion and confirmed that the requested Canva
trial already works and has been tried (“funziona già provato”). Reuse those
accepted trials; do not infer additional unreported interactions. Saved grants,
38/72 discovered tools and removal of all temporary containers are recorded in
the [existing verification](2026-11-02-sign-in-browser-spike.md).

The operator excluded GitHub and keeps Google's working Web-client flow through
the Cloudflare Google callback. Tasks 8–10 and the Google Desktop/next-day-refresh
trial are withdrawn, not passed. The Google relay remains live.

## Certificate failure in the actual cockpit bundle

Ran `node D:/tmp/signin-spike/task-6-tls-negative-edge.mjs` against the built
cockpit bundle in an isolated headless Edge instance. Browser TLS validation was
enabled (`ignoreHTTPSErrors: false`); the dedicated HTTPS fixture presented an
untrusted certificate and produced a real certificate request failure.

The page displayed the exact dedicated-origin link in a new tab, the retry
button and the own-browser action. No iframe was shown as ready. The run passed
in 2.682 seconds. The fixture used synthetic session/flow IDs and no provider
sign-in. Its parent was HTTP on loopback, a secure browser context; this run does
not establish a new trusted HTTPS-parent certificate installation. Successful
ordinary Edge framing is the separate operator acceptance above. No renewed
certificate-import attempt is used as evidence.

## Resource baseline

At 11:46:45 UTC the lab VM had 15,798,872 KiB RAM, eight CPUs and
14,658,224,128 bytes free on its 61,106,941,952-byte root filesystem (75% used).
It is the available appliance under test; smaller hosts remain unmeasured.

The configured Chromium digest remains
`sha256:b147169cc5ff21c62df977f314598537eecf566591c13ad0726b1e5cbd0d01ca`.
Docker reports 4,661,934,168 image bytes. Its earlier cold pull took 225 seconds;
this already-cached image has not been deleted merely to repeat that pull.
Compressed layer size is 1,166,237,615 bytes, measured from its amd64 manifest.
The ongoing native-backend resource probe will add peak cgroup usage and disk
after cleanup here. No minimum-host capacity claim follows from this baseline.
