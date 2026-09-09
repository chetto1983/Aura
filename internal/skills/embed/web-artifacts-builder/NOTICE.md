# Aura maintenance notes

Adapted from https://github.com/anthropics/skills/tree/main/skills/web-artifacts-builder
on 2026-09-08. The upstream Apache-2.0 license and component archive are retained.

Aura changes: remove generated favicon references, use relative TypeScript paths
without deprecated baseUrl, match the bundled component APIs to compatible
dependency versions, require the full project build, and smoke-test the exact
HTML at desktop/mobile sizes before publishing bundle.html. Failed builds remove
the old bundle instead of leaving a stale deliverable.

The browser smoke does not prove data accuracy or all interactions. The author
must inspect screenshots and test the requested behavior. The smoke check mirrors
the policy Aura actually serves: https fetch/XHR passes (connect-src is open because
the preview holds an opaque origin), plain-http fetch fails as mixed content, and
every other external resource must be bundled.
