package messagedrafts

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"
)

// edits.go names what a review changed. The model is told a send's result only, so after the
// operator rewrote the recipient and the text it still believed its own call had gone out
// (measured 2026-11-27). The note goes ahead of the result, where the model reads it and the
// tool row shows it.

// editedValueLimit bounds one field's value in the note, in bytes.
const editedValueLimit = 2000

// editableFields are the fields a review may change, in the order the card shows them.
func editableFields(target Target) []string {
	switch target {
	case emailTarget:
		return []string{"to", "cc", "subject", "body", "textBody", "htmlBody"}
	case whatsAppTarget:
		return []string{"recipient", "message"}
	default:
		return nil
	}
}

// OperatorEdits is the note for the fields the review changed, with the values that went out, or
// "" when the message is the model's own. An empty value and an absent field are the same.
func (d Draft) OperatorEdits() string {
	if len(d.EffectiveArgs) == 0 {
		return ""
	}
	var original, effective map[string]json.RawMessage
	if json.Unmarshal(d.OriginalArgs, &original) != nil || json.Unmarshal(d.EffectiveArgs, &effective) != nil {
		return ""
	}
	var changed []string
	for _, field := range editableFields(d.Target) {
		after := effective[field]
		if sameValue(original[field], after) {
			continue
		}
		changed = append(changed, field+" = "+clipValue(after))
	}
	if len(changed) == 0 {
		return ""
	}
	return "The operator edited your message before approving it. These fields differ from your call: " +
		strings.Join(changed, "; ") + "."
}

func sameValue(a, b json.RawMessage) bool {
	return reflect.DeepEqual(decodedValue(a), decodedValue(b))
}

// decodedValue reads a field's value, with null, "" and [] read as absent.
func decodedValue(raw json.RawMessage) any {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil
		}
	case []any:
		if len(v) == 0 {
			return nil
		}
	}
	return value
}

// clipValue is the field's JSON as it was sent, cut at editedValueLimit bytes on a rune boundary.
func clipValue(raw json.RawMessage) string {
	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil {
		return "(unreadable)"
	}
	text := compact.String()
	if len(text) <= editedValueLimit {
		return text
	}
	cut := editedValueLimit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}
