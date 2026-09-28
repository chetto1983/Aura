// Package messagedrafts validates the exact arguments an operator reviews before
// a trusted PIM or WhatsApp send. No model-supplied field can widen the target.
package messagedrafts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/mail"
	"strings"
)

const maxDraftArgsBytes = 256 * 1024
const maxDraftJSONDepth = 32

var errInvalidDraftArgs = errors.New("message draft arguments are invalid")

// Target is host-owned provenance, copied from the trusted managed recipe after
// the bridge has selected the exact server tool. The client never supplies it.
type Target struct {
	Recipe string
	Tool   string
	Action string
}

// MergeApprovedArgs applies only schema-supported editable fields, then checks
// the whole effective call. The returned JSON is the sole argument body that may
// be fingerprinted and dispatched after a durable one-shot claim.
func MergeApprovedArgs(target Target, original, overrides json.RawMessage) (json.RawMessage, error) {
	base, err := strictObject(original)
	if err != nil {
		return nil, err
	}
	if len(overrides) == 0 {
		overrides = json.RawMessage(`{}`)
	}
	edits, err := strictObject(overrides)
	if err != nil {
		return nil, err
	}
	var allowedOriginal, editable map[string]bool
	switch target {
	case Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"}:
		allowedOriginal = fieldSet("action", "accountId", "to", "cc", "subject", "body", "bodyFormat", "textBody", "htmlBody", "attachments")
		editable = fieldSet("to", "cc", "subject", "body", "textBody", "htmlBody")
	case Target{Recipe: "recipe:whatsapp", Tool: "send_message"}:
		allowedOriginal = fieldSet("recipient", "message", "quoted_message_id", "quoted_sender_jid", "quoted_content")
		editable = fieldSet("recipient", "message")
	default:
		return nil, errInvalidDraftArgs
	}
	for key := range base {
		if !allowedOriginal[key] {
			return nil, errInvalidDraftArgs
		}
	}
	for key, value := range edits {
		if !editable[key] {
			return nil, errInvalidDraftArgs
		}
		base[key] = value
	}
	if target.Recipe == "recipe:calendar" {
		if err := validateEmail(base, edits); err != nil {
			return nil, err
		}
	} else if err := validateWhatsApp(base); err != nil {
		return nil, err
	}
	out, err := json.Marshal(base)
	if err != nil || len(out) > maxDraftArgsBytes {
		return nil, errInvalidDraftArgs
	}
	return out, nil
}

func fieldSet(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[name] = true
	}
	return out
}

// strictObject rejects duplicate keys at every depth, including fixed attachment
// and quote metadata, so the review UI and the sidecar cannot disagree over a
// last-key-wins interpretation. It also rejects trailing JSON values.
func strictObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > maxDraftArgsBytes {
		return nil, errInvalidDraftArgs
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := checkValue(dec, 0); err != nil {
		return nil, errInvalidDraftArgs
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errInvalidDraftArgs
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, errInvalidDraftArgs
	}
	return object, nil
}

func checkValue(dec *json.Decoder, depth int) error {
	if depth > maxDraftJSONDepth {
		return errInvalidDraftArgs
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return errInvalidDraftArgs
			}
			seen[key] = true
			if err := checkValue(dec, depth+1); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	case '[':
		for dec.More() {
			if err := checkValue(dec, depth+1); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	default:
		return errInvalidDraftArgs
	}
}

func validateEmail(base, edits map[string]json.RawMessage) error {
	if value, err := requiredString(base, "action", 32); err != nil || value != "send_email" {
		return errInvalidDraftArgs
	}
	to, err := emailList(base, "to", true)
	if err != nil || len(to) == 0 {
		return errInvalidDraftArgs
	}
	if _, err := emailList(base, "cc", false); err != nil {
		return err
	}
	subject, err := requiredString(base, "subject", 998)
	if err != nil || strings.ContainsAny(subject, "\r\n") {
		return errInvalidDraftArgs
	}
	if err := optionalString(base, "accountId", 256); err != nil {
		return err
	}
	format := "html"
	if value, ok := base["bodyFormat"]; ok {
		if json.Unmarshal(value, &format) != nil {
			return errInvalidDraftArgs
		}
		format = strings.ToLower(format)
		if format != "html" && format != "text" && format != "multipart" {
			return errInvalidDraftArgs
		}
	}
	for _, field := range []string{"body", "textBody", "htmlBody"} {
		if err := optionalString(base, field, 64*1024); err != nil {
			return err
		}
	}
	if format == "multipart" {
		if _, edited := edits["body"]; edited {
			return errInvalidDraftArgs // the sidecar ignores body for multipart
		}
		if _, err := requiredString(base, "textBody", 64*1024); err != nil {
			return err
		}
		if _, err := requiredString(base, "htmlBody", 64*1024); err != nil {
			return err
		}
	} else if _, edited := edits["textBody"]; edited {
		return errInvalidDraftArgs
	} else if _, edited := edits["htmlBody"]; edited {
		return errInvalidDraftArgs
	}
	if attachments, ok := base["attachments"]; ok {
		var items []map[string]json.RawMessage
		if json.Unmarshal(attachments, &items) != nil || len(items) > 20 {
			return errInvalidDraftArgs
		}
		attachmentFields := fieldSet("name", "contentType", "base64Content", "attachmentId")
		for _, item := range items {
			if item == nil {
				return errInvalidDraftArgs
			}
			for key := range item {
				if !attachmentFields[key] {
					return errInvalidDraftArgs
				}
			}
		}
	}
	return nil
}

func emailList(base map[string]json.RawMessage, key string, required bool) ([]string, error) {
	raw, ok := base[key]
	if !ok {
		if required {
			return nil, errInvalidDraftArgs
		}
		return nil, nil
	}
	var addresses []string
	if json.Unmarshal(raw, &addresses) != nil || len(addresses) > 50 || (required && len(addresses) == 0) {
		return nil, errInvalidDraftArgs
	}
	for _, address := range addresses {
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address || len(address) > 320 || strings.ContainsAny(address, "\r\n") {
			return nil, errInvalidDraftArgs
		}
	}
	return addresses, nil
}

func validateWhatsApp(base map[string]json.RawMessage) error {
	recipient, err := requiredString(base, "recipient", 256)
	if err != nil || strings.ContainsAny(recipient, " \t\r\n") {
		return errInvalidDraftArgs
	}
	if _, err := requiredString(base, "message", 64*1024); err != nil {
		return err
	}
	for _, key := range []string{"quoted_message_id", "quoted_sender_jid", "quoted_content"} {
		if err := optionalString(base, key, 64*1024); err != nil {
			return err
		}
	}
	return nil
}

func requiredString(base map[string]json.RawMessage, key string, limit int) (string, error) {
	raw, ok := base[key]
	if !ok {
		return "", errInvalidDraftArgs
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value) == "" || len(value) > limit {
		return "", errInvalidDraftArgs
	}
	return value, nil
}

func optionalString(base map[string]json.RawMessage, key string, limit int) error {
	raw, ok := base[key]
	if !ok {
		return nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || len(value) > limit {
		return errInvalidDraftArgs
	}
	return nil
}
