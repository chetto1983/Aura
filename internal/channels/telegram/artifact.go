// Package telegram — this file is the artifact consumer (the channel side of
// D-06 / UX-02). The substrate (plan 13-02) emits a channel-agnostic AG-UI CUSTOM
// event named agui.ArtifactEventName carrying a {path, filename, caption}
// descriptor; this file is the Telegram renderer that turns it into a
// sendDocument. Telegram auto-detects the MIME from the file, so there is no
// channel-side MIME plumbing. The caption is ASCII-sanitized before it reaches the
// Bot API (Pitfall 4 / T-13-06-CaptionInject) so a non-ASCII byte never triggers a
// document-caption 400.
package telegram

import (
	"context"
	"strings"
	"unicode"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agui"
)

// artifact renders artifact CUSTOM events to a chat as documents. It implements
// the eventConsumer seam so the per-turn fanout can drive it like the status pane
// / renderer (a third consumer subscribed before Run, plan 13-05 wiring).
type artifact struct {
	bot botSender
	to  tele.Recipient
}

// newArtifact builds an artifact consumer bound to a chat.
func newArtifact(bot botSender, to tele.Recipient) *artifact {
	return &artifact{bot: bot, to: to}
}

// consume drains the subscriber channel, sending a document for every artifact
// CUSTOM event and ignoring the rest. The channel is closed by the Fanout producer
// on source-end/ctx-cancel, so the range terminates without a leak.
func (a *artifact) consume(ctx context.Context, ch <-chan events.Event) {
	for ev := range ch {
		if ctx.Err() != nil {
			return
		}
		a.consumeEvent(ev)
	}
}

// consumeEvent renders ONE event: an artifact CUSTOM event becomes a sendDocument
// (returning the Send RESPONSE — the spike ground truth — and ok=true); any other
// event is ignored (ok=false). A descriptor with no usable path is a no-op.
func (a *artifact) consumeEvent(ev events.Event) (*tele.Message, bool) {
	desc, ok := artifactDescriptor(ev)
	if !ok {
		return nil, false
	}
	path := stringField(desc, "path")
	if path == "" {
		return nil, false
	}
	doc := &tele.Document{
		File:     tele.FromDisk(path),
		FileName: stringField(desc, "filename"),
		Caption:  asciiCaption(stringField(desc, "caption")),
	}
	msg, err := a.bot.Send(a.to, doc)
	if err != nil {
		return nil, false // best-effort: a failed delivery must not wedge the turn
	}
	return msg, true
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
