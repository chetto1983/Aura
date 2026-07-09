// shouldSpeak — the client-side auto-speak predicate, a direct port of Telegram's
// ShouldSpeak (internal/channels/telegram/tts.go:24 → `voiceMode || inboundWasVoice`).
// Auto-speak a new assistant reply when the session voice-mode toggle is on OR the
// user produced that turn by dictation (echo-modality parity with Telegram's
// inboundWasVoice). Pure and side-effect free so it is trivially unit- and
// mutation-tested (37C-06 Stryker gate).
export function shouldSpeak(voiceMode: boolean, turnWasDictated: boolean): boolean {
  return voiceMode || turnWasDictated;
}
