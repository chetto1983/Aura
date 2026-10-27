package activesources

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func TestEncodeDecodeRoundTripIsCanonical(t *testing.T) {
	t.Parallel()
	a := Source{ConversationID: "conversation-a", TurnID: "turn-1"}
	b := Source{ConversationID: "conversation-b", TurnID: "turn-1"}
	forward, err := Encode([]Source{a, b})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	reversed, err := Encode([]Source{b, a})
	if err != nil {
		t.Fatalf("Encode reversed: %v", err)
	}
	if forward != reversed {
		t.Fatalf("equivalent source sets encoded twice: %q vs %q", forward, reversed)
	}
	got, err := Decode(forward)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("Decode = %+v, want [a b] in canonical order", got)
	}
}

func TestEncodeRefusesUnusableSources(t *testing.T) {
	t.Parallel()
	ok := Source{ConversationID: "c", TurnID: "t"}
	tooMany := make([]Source, 0, MaxSources+1)
	for index := range MaxSources + 1 {
		tooMany = append(tooMany, Source{ConversationID: fmt.Sprintf("c-%02d", index), TurnID: "t"})
	}
	for name, sources := range map[string][]Source{
		"none":             nil,
		"over the count":   tooMany,
		"duplicate":        {ok, ok},
		"empty id":         {{ConversationID: "", TurnID: "t"}},
		"padded id":        {{ConversationID: " c", TurnID: "t"}},
		"control rune":     {{ConversationID: "c\x00", TurnID: "t"}},
		"over the id cap":  {{ConversationID: strings.Repeat("c", maxIDRunes+1), TurnID: "t"}},
		"over the payload": overPayload(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if encoded, err := Encode(sources); err == nil {
				t.Fatalf("Encode accepted %s: %q", name, encoded)
			}
		})
	}
}

// overPayload is MaxSources pairs of maximum-length ids: valid one by one, too large together.
func overPayload() []Source {
	sources := make([]Source, 0, MaxSources)
	for index := range MaxSources {
		sources = append(sources, Source{
			ConversationID: fmt.Sprintf("%0*d", maxIDRunes, index), TurnID: strings.Repeat("t", maxIDRunes),
		})
	}
	return sources
}

// Decode accepts only the bytes Encode would produce for the same sources, so a header that
// carries the right pairs in another shape cannot slip past the server.
func TestDecodeRefusesEveryNonCanonicalShape(t *testing.T) {
	t.Parallel()
	b64 := base64.RawURLEncoding.EncodeToString
	for name, header := range map[string]string{
		"empty":             "",
		"surrounding space": " " + b64([]byte(`{"version":1,"sources":[{"conversation_id":"c","turn_id":"t"}]}`)),
		"not base64url":     "%%%",
		"padded base64":     paddedCanonical(t),
		"unknown field":     b64([]byte(`{"version":1,"sources":[{"conversation_id":"c","turn_id":"t"}],"x":1}`)),
		"trailing json":     b64([]byte(`{"version":1,"sources":[{"conversation_id":"c","turn_id":"t"}]}{}`)),
		"other version":     b64([]byte(`{"version":2,"sources":[{"conversation_id":"c","turn_id":"t"}]}`)),
		"unsorted":          b64([]byte(`{"version":1,"sources":[{"conversation_id":"d","turn_id":"t"},{"conversation_id":"c","turn_id":"t"}]}`)),
		"spaced json":       b64([]byte(`{"version": 1, "sources":[{"conversation_id":"c","turn_id":"t"}]}`)),
		"over the header":   strings.Repeat("a", maxEncodedSize+1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if sources, err := Decode(header); err == nil {
				t.Fatalf("Decode accepted %s: %+v", name, sources)
			}
		})
	}
}

// paddedCanonical is a canonical header re-encoded WITH padding. The source is chosen so the
// payload length is not a multiple of three, or there would be no padding to refuse.
func paddedCanonical(t *testing.T) string {
	t.Helper()
	encoded, err := Encode([]Source{{ConversationID: "cc", TurnID: "t"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	padded := base64.URLEncoding.EncodeToString(raw)
	if !strings.HasSuffix(padded, "=") {
		t.Fatalf("fixture %q carries no padding; pick another source", padded)
	}
	return padded
}
