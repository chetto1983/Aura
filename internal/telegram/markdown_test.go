package telegram

import "testing"

func TestRenderForTelegram(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "hello world", "hello world"},
		{"bold star", "**hi**", "<b>hi</b>"},
		{"bold under", "__hi__", "<b>hi</b>"},
		{"italic star", "an *italic* word", "an <i>italic</i> word"},
		{"italic under", "an _italic_ word", "an <i>italic</i> word"},
		{"strike", "~~gone~~", "<s>gone</s>"},
		{"inline code", "use `go run`", "use <code>go run</code>"},
		{"fenced code", "```\nfoo()\n```", "<pre>foo()</pre>"},
		{"fenced lang", "```go\nfunc x() {}\n```", "<pre>func x() {}</pre>"},
		{"link http", "see [docs](https://example.com)", `see <a href="https://example.com">docs</a>`},
		{"link unsafe", "click [here](javascript:alert(1))", `click [here](javascript:alert(1))`},
		{"heading 1", "# Header", "<b>Header</b>"},
		{"heading 2", "## Header", "<b>Header</b>"},
		{"heading 3", "### Header", "<b>Header</b>"},
		{"bullet dash", "- item", "• item"},
		{"bullet star", "* item", "• item"},
		{"bullet not bold", "* item", "• item"},
		{"escape lt", "5 < 6", "5 &lt; 6"},
		{"escape gt", "7 > 4", "7 &gt; 4"},
		{"escape amp", "Tom & Jerry", "Tom &amp; Jerry"},
		{"code preserves chars", "use `<html>`", "use <code>&lt;html&gt;</code>"},
		{
			"mixed",
			"# Tip\n\n**Important:** use `go test` to run.",
			"<b>Tip</b>\n\n<b>Important:</b> use <code>go test</code> to run.",
		},
		{
			"bullet list",
			"Things:\n- one\n- two\n- three",
			"Things:\n• one\n• two\n• three",
		},
		{
			"numeric not italic",
			"price: 2*3 = 6",
			"price: 2*3 = 6",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderForTelegram(tc.in)
			if got != tc.want {
				t.Errorf("renderForTelegram(%q)\n  got  %q\n  want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRenderForTelegramDoesNotDoubleEscape(t *testing.T) {
	// Producing a link puts &quot; into the href; we shouldn't escape
	// &quot; back to &amp;quot; in the post-pass.
	in := `see [hi](https://x.com/?q=a&b)`
	got := renderForTelegram(in)
	want := `see <a href="https://x.com/?q=a&amp;b">hi</a>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
