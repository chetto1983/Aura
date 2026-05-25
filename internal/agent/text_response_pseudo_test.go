package agent

import "testing"

func TestExtractTextResponsePseudoCall(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "standalone double quoted call",
			in:   `text_response(text="Fatto.")`,
			want: "Fatto.",
			ok:   true,
		},
		{
			name: "last line after prose",
			in:   "Ho creato il file.\n\ntext_response(text=\"Ho creato il file.\")",
			want: "Ho creato il file.",
			ok:   true,
		},
		{
			name: "single quoted call",
			in:   `text_response(text='Fatto anche qui.')`,
			want: "Fatto anche qui.",
			ok:   true,
		},
		{
			name: "not standalone",
			in:   `usa text_response(text="ciao") come esempio`,
			ok:   false,
		},
		{
			name: "wrong tool stays untouched",
			in:   `create_document(format="xlsx")`,
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extractTextResponsePseudoCall(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("extractTextResponsePseudoCall(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}
