// Package telegram — this file is the artifact consumer (the channel side of
// D-06 / UX-02). The substrate (plan 13-02) emits a channel-agnostic AG-UI CUSTOM
// event named agui.ArtifactEventName carrying a {path, filename, caption,
// mime_type, …} descriptor; this file is the Telegram renderer that turns it into
// the best delivery Telegram offers for those bytes — an inline photo, a streamable
// video, or a document. The caption is ASCII-sanitized and length-bounded before it
// reaches the Bot API (Pitfall 4 / T-13-06-CaptionInject) so neither a byte > 0x7F
// nor an over-long caption triggers a 400.
package telegram

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"unicode"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agui"
)

// Bot API upload ceilings for a NATIVE photo / video, deliberately the conservative
// decimal ones Telegram documents (10 MB photo, 50 MB file) rather than binary MB.
// Aura's own asset ceiling is 52428800 bytes, so a generated clip can legitimately sit
// ABOVE the upload ceiling and below the cap: it exists in the cockpit and the chat is
// told where to find it.
const (
	telegramPhotoUploadCap = 10_000_000
	telegramVideoUploadCap = 50_000_000
)

// videoInCockpitMessage is what the chat gets for a clip Telegram will not carry. Like
// every other user-facing string in this package (turnBusyMessage, the status-pane
// labels) it is written straight in Italian — the channel has no localization layer.
const videoInCockpitMessage = "Il video è disponibile nel cockpit."

// artifact renders artifact CUSTOM events to a chat. It implements the
// eventConsumer seam so the per-turn fanout can drive it like the status pane /
// renderer (a third consumer subscribed before Run, plan 13-05 wiring).
type artifact struct {
	bot botSender
	to  tele.Recipient
}

// newArtifact builds an artifact consumer bound to a chat.
func newArtifact(bot botSender, to tele.Recipient) *artifact {
	return &artifact{bot: bot, to: to}
}

// consume drains the subscriber channel, delivering every artifact CUSTOM event and
// ignoring the rest. The channel is closed by the Fanout producer on
// source-end/ctx-cancel, so the range terminates without a leak.
func (a *artifact) consume(ctx context.Context, ch <-chan events.Event) {
	for ev := range ch {
		if ctx.Err() != nil {
			return
		}
		a.consumeEvent(ev)
	}
}

// consumeEvent renders ONE event: an artifact CUSTOM event becomes the best Telegram
// delivery for the file (photo / video / document, or the cockpit announcement for a
// clip Telegram will not take), returning the Send RESPONSE — the spike ground truth
// — and ok=true. Any other event is ignored (ok=false), as is a descriptor whose file
// is not there to send.
func (a *artifact) consumeEvent(ev events.Event) (*tele.Message, bool) {
	desc, ok := artifactDescriptor(ev)
	if !ok {
		return nil, false
	}
	payload, ok := artifactPayload(desc)
	if !ok {
		return nil, false
	}
	msg, err := a.bot.Send(a.to, payload)
	if err == nil {
		return msg, true
	}
	doc, ok := rejectedPhotoFallback(payload, stringField(desc, "filename"), err)
	if !ok {
		return nil, false // best-effort: a failed delivery must not wedge the turn
	}
	if msg, err = a.bot.Send(a.to, doc); err != nil {
		return nil, false
	}
	return msg, true
}

// artifactPayload picks the Telegram sendable for an artifact descriptor: a native
// photo or streamable video when the MIME says so and the file fits the Bot API's
// upload ceiling, the cockpit announcement for a clip that does not, and a document
// for everything else — an unknown or absent MIME included, which is what a generic
// send_file delivery looks like.
//
// The size comes from os.Stat, NEVER from the descriptor's size_bytes: that field is
// the producer's claim about a file it staged earlier, and a wrong claim would push
// an oversized upload at the Bot API. A file that cannot be stat'd is not deliverable
// at all (ok=false) — the upload would fail anyway, so the round trip is skipped.
func artifactPayload(desc map[string]any) (any, bool) {
	path := stringField(desc, "path")
	if path == "" {
		return nil, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, false
	}
	size := info.Size()
	mimeType := strings.ToLower(stringField(desc, "mime_type"))
	filename := stringField(desc, "filename")
	caption := capRunes(asciiCaption(stringField(desc, "caption")), telegramCaptionCap)
	switch {
	case strings.HasPrefix(mimeType, "image/") && size <= telegramPhotoUploadCap:
		return &tele.Photo{File: tele.FromDisk(path), Caption: caption}, true
	case strings.HasPrefix(mimeType, "video/") && size <= telegramVideoUploadCap:
		return &tele.Video{
			File: tele.FromDisk(path), FileName: filename, MIME: mimeType,
			Caption: caption, Streaming: true,
		}, true
	case strings.HasPrefix(mimeType, "video/"):
		return videoInCockpitMessage, true
	default:
		return &tele.Document{File: tele.FromDisk(path), FileName: filename, Caption: caption}, true
	}
}

// rejectedPhotoFallback re-offers a REFUSED photo as a plain file. It fires only on a
// Bot API 400: the request reached Telegram, was parsed and was refused, so nothing
// was delivered and a second send cannot duplicate a message — the SVG or exotic
// format Telegram will not process as an image still travels fine as a document.
//
// Everything else is ambiguous (a timeout, a reset connection, a 5xx: the message may
// already be in the chat) and is never retried. Only a photo has a cheaper lane to
// fall back to; a refused video or document is simply reported.
func rejectedPhotoFallback(payload any, filename string, err error) (*tele.Document, bool) {
	photo, ok := payload.(*tele.Photo)
	if !ok {
		return nil, false
	}
	var apiErr *tele.Error
	if !errors.As(err, &apiErr) || apiErr.Code != http.StatusBadRequest {
		return nil, false
	}
	return &tele.Document{File: photo.File, FileName: filename, Caption: photo.Caption}, true
}

// artifactDescriptor extracts the {path,filename,caption} descriptor from an
// AG-UI CUSTOM event named agui.ArtifactEventName. Any other event (or a
// differently-named CUSTOM event, or a non-map value) returns ok=false.
func artifactDescriptor(ev events.Event) (map[string]any, bool) {
	ce, ok := ev.(*events.CustomEvent)
	if !ok || ce.Name != agui.ArtifactEventName {
		return nil, false
	}
	desc, ok := ce.Value.(map[string]any)
	if !ok {
		return nil, false
	}
	return desc, true
}

// stringField reads a string descriptor field, tolerating an absent/non-string
// value (returns "").
func stringField(desc map[string]any, key string) string {
	if v, ok := desc[key].(string); ok {
		return v
	}
	return ""
}

// asciiCaption renders a caption ASCII-safe (Pitfall 4): an accented Latin rune is
// folded to its base ASCII letter where trivially possible, any other non-ASCII
// rune is dropped. The substrate already sanitizes in send_file, but the channel
// re-applies it defensively so a caption reaching the Bot API never carries a byte
// > 0x7F (the document-caption 400 trigger).
func asciiCaption(caption string) string {
	var b strings.Builder
	b.Grow(len(caption))
	for _, r := range caption {
		switch {
		case r <= unicode.MaxASCII:
			b.WriteRune(r)
		default:
			if base := foldLatinToASCII(r); base != 0 {
				b.WriteByte(base)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// foldLatinToASCII maps a common accented Latin rune to its base ASCII letter, or
// 0 when there is no trivial single-byte equivalent (the rune is then dropped). It
// covers the Latin-1/Latin-Extended-A letters an Italian/European caption is most
// likely to carry — deliberately small, not a full transliteration table.
func foldLatinToASCII(r rune) byte {
	switch r {
	case 'à', 'á', 'â', 'ã', 'ä', 'å', 'ā', 'ă', 'ą':
		return 'a'
	case 'è', 'é', 'ê', 'ë', 'ē', 'ĕ', 'ė', 'ę', 'ě':
		return 'e'
	case 'ì', 'í', 'î', 'ï', 'ī', 'ĭ', 'į':
		return 'i'
	case 'ò', 'ó', 'ô', 'õ', 'ö', 'ø', 'ō', 'ŏ', 'ő':
		return 'o'
	case 'ù', 'ú', 'û', 'ü', 'ū', 'ŭ', 'ů', 'ű':
		return 'u'
	case 'ç', 'ć', 'č', 'ĉ', 'ċ':
		return 'c'
	case 'ñ', 'ń', 'ň':
		return 'n'
	case 'ÿ', 'ý':
		return 'y'
	case 'À', 'Á', 'Â', 'Ã', 'Ä', 'Å', 'Ā', 'Ă', 'Ą':
		return 'A'
	case 'È', 'É', 'Ê', 'Ë', 'Ē', 'Ĕ', 'Ė', 'Ę', 'Ě':
		return 'E'
	case 'Ì', 'Í', 'Î', 'Ï', 'Ī', 'Ĭ', 'Į':
		return 'I'
	case 'Ò', 'Ó', 'Ô', 'Õ', 'Ö', 'Ø', 'Ō', 'Ŏ', 'Ő':
		return 'O'
	case 'Ù', 'Ú', 'Û', 'Ü', 'Ū', 'Ŭ', 'Ů', 'Ű':
		return 'U'
	case 'Ç', 'Ć', 'Č', 'Ĉ', 'Ċ':
		return 'C'
	case 'Ñ', 'Ń', 'Ň':
		return 'N'
	default:
		return 0
	}
}
