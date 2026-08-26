---
status: testing
phase: 52-mid-turn-steering
source: [52-VALIDATION.md]
started: 2026-08-26
updated: 2026-08-26
---

## Current Test

number: 1
name: A plain-text Telegram message redirects a live turn
expected: |
  While a turn is visibly running for your chat, send a plain text message
  (e.g. "aspetta, fallo in inglese"). The bot replies with the steer echo
  (turnSteeredMessage) rather than the old busy copy, and Aura's NEXT round
  acts on the new instruction. No tool is killed mid-execution.
awaiting: user response

## Tests

### 1. Plain-text Telegram message redirects a live turn
requirement: STEER-05 / SC#5
expected: bot replies with the turnSteeredMessage echo (not busy copy); next round acts on the new instruction
result: [pending]

### 2. A photo sent during a live turn is queued AND its turn actually runs
requirement: STEER-05 (D-05 media queue)
expected: |
  Send a photo from your phone while a turn is running. The bot replies
  turnQueuedForNextTurnMessage (distinct from the steer echo). When the live
  turn ends, a SECOND turn actually EXECUTES on the photo — verify a real
  conversation-turn row exists, not merely a reply promising it.
result: [pending]

### 3. /cancel with a queued photo announces it was not delivered
requirement: STEER-05 (D-05)
expected: |
  Repeat test 2, then /cancel before the live turn ends. The bot announces
  turnQueuedNotDeliveredMessage rather than going silent.
result: [pending]

## Summary

total: 3
passed: 0
issues: 0
pending: 3
skipped: 0
blocked: 0

## Gaps

Structural, not a defect: the bot long-polls getUpdates against the real
Telegram Bot API. No local-bot-api sidecar, no Telethon/Pyrogram session, no
API_ID/API_HASH in .env — an inbound message cannot be scripted as a real
Telegram user from this host. Only a human with their own Telegram client can
close these three.

Separately unproven (does NOT need a human, needs instrumentation):
STEER-03's user_message_fallback delivery branch. Every attempt to land a
steer while the history tail was not a tool result raced drain-point A and
lost, landing as auto_delivery_next_turn. Would need a test-only instrumented
delay in drain-point A, or a scenario shape where the tail is reliably a plain
assistant message.
