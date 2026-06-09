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
