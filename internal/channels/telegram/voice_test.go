package telegram

import (
	"io"
	"strings"
	"sync"
	"testing"

	tele "gopkg.in/telebot.v4"
)

// fileBot is a botFiler/botReactor double: it returns canned bytes for a File
// download and records every React it is asked to apply (the hard-fail UX asserts on
// the recorded 😵). It satisfies neither Send nor Edit.
type fileBot struct {
	ogg []byte

	mu        sync.Mutex
	reactions []string
}

func (b *fileBot) File(_ *tele.File) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(b.ogg))), nil
}

func (b *fileBot) React(_ tele.Recipient, _ tele.Editable, r tele.Reactions) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, re := range r.Reactions {
		b.reactions = append(b.reactions, re.Emoji)
	}
	return nil
}

func (b *fileBot) recordedReactions() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.reactions))
	copy(out, b.reactions)
	return out
}

// TestReactHardFailAppliesTheEmoji covers what is left of the voice leg in this
// package after transcription moved to assets.AudioProcessor: the Bot-API reaction,
// which is Telegram's alone. The retry-then-fail behaviour it used to accompany is
// now asserted in internal/assets (TestAudioProcessorRetriesThenFails).
func TestReactHardFailAppliesTheEmoji(t *testing.T) {
	t.Parallel()
	bot := &fileBot{}
	if err := reactHardFail(bot, tele.ChatID(7), &tele.Message{ID: 1}); err != nil {
		t.Fatalf("reactHardFail: %v", err)
	}
	react := bot.recordedReactions()
	if len(react) != 1 || react[0] != hardFailReaction {
		t.Errorf("hard-fail reaction = %v, want [%q]", react, hardFailReaction)
	}
}

// TestVoiceHardFailMessageIsItalian asserts the user-facing hard-fail copy is the
// locked IT string.
func TestVoiceHardFailMessageIsItalian(t *testing.T) {
	t.Parallel()
	if !strings.Contains(transcribeFailMessage, "Trascrizione non disponibile") {
		t.Errorf("hard-fail message = %q, want the IT copy", transcribeFailMessage)
	}
}
