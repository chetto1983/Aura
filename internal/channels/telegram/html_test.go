package telegram

import (
	"strings"
	"testing"
)

func TestRenderTelegramHTMLEscapesRawTagsAndKeepsAllowedFormatting(t *testing.T) {
	got := RenderTelegramHTML("*bold* _italic_ `code` <script>&")
	for _, want := range []string{"<b>bold</b>", "<i>italic</i>", "<code>code</code>", "&lt;script&gt;&amp;"} {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderTelegramHTML missing %q in %q", want, got)
		}
	}
}

func TestRenderTelegramHTMLNormalizesUnsupportedBlockMarkdown(t *testing.T) {
	got := RenderTelegramHTML("Ecco le notizie:\n\n---\n\n### Auto contro un cancello\nDettaglio.\n\n### Meteo\nSole.\n\n---")

	for _, bad := range []string{"###", "---"} {
		if strings.Contains(got, bad) {
			t.Fatalf("RenderTelegramHTML leaked block markdown marker %q in %q", bad, got)
		}
	}
	for _, want := range []string{"<b>Auto contro un cancello</b>", "<b>Meteo</b>", "Dettaglio.", "Sole."} {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderTelegramHTML missing %q in %q", want, got)
		}
	}
}
