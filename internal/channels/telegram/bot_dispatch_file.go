package telegram

import (
	"io"

	tele "gopkg.in/telebot.v4"
)

// downloadFile pulls a media file's bytes off the Telegram file server via the
// narrow botFiler seam (the same surface voice.go downloads through). The Bot-API
// getFile endpoint caps a download at the 20MB file ceiling, so the read is bounded
// upstream (T-13-10-MediaDoS); the document size tiers add the convert-side guard.
func downloadFile(filer botFiler, file *tele.File) ([]byte, error) {
	rc, err := filer.File(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}
