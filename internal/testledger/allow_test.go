package testledger

import (
	"strings"
	"testing"
)

func TestParseAllow(t *testing.T) {
	src := `# Comments and blank lines are free.

go | internal/llm/openai_compat TestOllamaLiveE2E | paid | spends Ollama cloud credit
playwright | web/e2e/remote-access-live.spec.ts | outward | an owned Cloudflare zone and an OTP to a real inbox
playwright | web/e2e/attachment-filename.spec.ts [mobile-chrome] | platform | one upload path, proven on desktop
`
	entries, err := ParseAllow(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("want three entries, got %+v", entries)
	}
	first := entries[0]
	if first.Suite != "go" || first.Selector != "internal/llm/openai_compat TestOllamaLiveE2E" ||
		first.Class != "paid" || first.Reason != "spends Ollama cloud credit" || first.Line != 3 {
		t.Errorf("first entry: %+v", first)
	}
}

func TestParseAllowRefuses(t *testing.T) {
	for name, src := range map[string]string{
		"an unknown class":     "go | internal/x TestX | pending | later\n",
		"no reason":            "go | internal/x TestX | paid |\n",
		"three fields":         "go | internal/x TestX | paid\n",
		"an unknown suite":     "jest | a.test.js | paid | money\n",
		"an empty selector":    "go |  | paid | money\n",
		"the same entry twice": "script | scripts/a_test.sh | platform | x\nscript | scripts/a_test.sh | measure | y\n",
	} {
		if _, err := ParseAllow(strings.NewReader(src)); err == nil {
			t.Errorf("%s must be a parse error", name)
		}
	}
}
