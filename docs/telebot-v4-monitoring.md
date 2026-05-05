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
