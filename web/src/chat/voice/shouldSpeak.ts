// shouldSpeak — the client-side auto-speak predicate for the COMPOSER lane: a reply is
// read aloud when the person produced that turn by dictating it (echo-modality parity
// with Telegram's inboundWasVoice, internal/channels/telegram/tts.go:24).
//
// It is deliberately FALSE while voice mode is on. Voice mode opens the hands-free
// overlay, and the overlay speaks the reply itself as part of its loop — leaving this
// predicate a plain OR made the same answer play twice, once through each path.
// Pure and side-effect free so it stays trivially unit- and mutation-tested.
export function shouldSpeak(voiceMode: boolean, turnWasDictated: boolean): boolean {
  return turnWasDictated && !voiceMode;
}
