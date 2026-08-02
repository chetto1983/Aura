// Package telegram — this file is the inbound-media WIRE glue: the two narrow
// telebot surfaces the media handlers need (file download + message reaction) and
// the hard-fail signal.
//
// It used to hold a second speech-to-text client. It does not any more: a voice note
// is now ingested as an asset like every other attachment, and the transcription runs
// in assets.AudioProcessor over the shared multimodal.STTClient — the same code the
// cockpit's uploads go through. What is left here is genuinely Telegram's: the Bot-API
// reaction, which no other channel has.
package telegram

import (
	"io"

	tele "gopkg.in/telebot.v4"
)

// transcribeFailMessage is the user-facing hard-fail copy (always Italian — the
// output-language directive), sent when a voice note cannot be ingested/transcribed.
const transcribeFailMessage = "❌ Trascrizione non disponibile."

// hardFailReaction is the 😵 emoji applied to the voice message on a hard failure (a
// non-intrusive signal the note could not be transcribed).
const hardFailReaction = "😵"

// botFiler is the narrow telebot surface the media handlers need to pull an
// attachment's bytes off the Telegram file server. *tele.Bot satisfies it (Bot.File).
// Declared as a seam so unit tests inject canned bytes with no network.
type botFiler interface {
	File(file *tele.File) (io.ReadCloser, error)
}

// botReactor is the narrow telebot surface for the hard-fail 😵 reaction.
// *tele.Bot satisfies it (Bot.React).
type botReactor interface {
	React(to tele.Recipient, msg tele.Editable, r tele.Reactions) error
}

// reactHardFail applies the 😵 reaction to the inbound voice MESSAGE after a failed
// ingest (the reaction targets the message, not the media). The user-facing text is
// sent separately by the handler, which holds the sender.
func reactHardFail(bot botReactor, to tele.Recipient, msg tele.Editable) error {
	return bot.React(to, msg, tele.Reactions{
		Reactions: []tele.Reaction{{Type: tele.ReactionTypeEmoji, Emoji: hardFailReaction}},
	})
}
