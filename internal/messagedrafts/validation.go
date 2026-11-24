// Package messagedrafts validates the exact arguments an operator reviews before
// a trusted PIM or WhatsApp send. No model-supplied field can widen the target.
package messagedrafts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
)

const maxDraftArgsBytes = 256 * 1024
const maxDraftJSONDepth = 32

var errInvalidDraftArgs = errors.New("message draft arguments are invalid")

// ErrInvalidArgs is returned when editable fields do not match the trusted
// message schema. Callers may map it to a client error without exposing content.
var ErrInvalidArgs = errInvalidDraftArgs

// Target is host-owned provenance, copied from the trusted managed recipe after
// the bridge has selected the exact server tool. The client never supplies it.
// Action is empty for every target made since the PIM split its tools; it still
// carries "send_email" on drafts stored in the multiplexed shape, which the
// message_drafts CHECK keeps (migration 0157) and no send accepts.
type Target struct {
	Recipe string
	Tool   string
	Action string
}

var (
	emailTarget       = Target{Recipe: "recipe:calendar", Tool: "send_email"}
	legacyEmailTarget = Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"}
	whatsAppTarget    = Target{Recipe: "recipe:whatsapp", Tool: "send_message"}
)

// EmailTarget is the PIM's send_email tool, the target of every email draft.
func EmailTarget() Target { return emailTarget }

// LegacyEmailTarget is the multiplexed calendar tool's send_email action, the target email drafts
// stored before the PIM split its tools. Resolved drafts keep it as history.
func LegacyEmailTarget() Target { return legacyEmailTarget }

// TrustedOutboundTarget matches only the managed bridge's host-owned recipe and
// raw MCP tool identity. The registered name may be truncated with a hash.
func TrustedOutboundTarget(spec tools.Spec) (Target, bool) {
	for _, target := range []Target{emailTarget, whatsAppTarget} {
		if spec.TrustedRecipeSource == target.Recipe && spec.TrustedRecipeTool == target.Tool {
			return target, true
		}
	}
	return Target{}, false
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
	case emailTarget:
		allowedOriginal = fieldSet("accountId", "to", "cc", "subject", "body", "bodyFormat", "textBody", "htmlBody", "attachments")
		editable = fieldSet("to", "cc", "subject", "body", "textBody", "htmlBody")
	case whatsAppTarget:
		allowedOriginal = fieldSet("recipient", "message", "quoted_message_id", "quoted_sender_jid", "quoted_content")
		editable = fieldSet("recipient", "message")
	default:
		return nil, errInvalidDraftArgs
	}
	if err := rejectUnknownFields(base, allowedOriginal); err != nil {
		return nil, err
	}
	for key, value := range edits {
		if !editable[key] {
			return nil, errInvalidDraftArgs
		}
		base[key] = value
	}
	if target == emailTarget {
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

// rejectUnknownFields names every key the send does not accept, and the ones it does: the error
// reaches the model that made the call, which can only fix the fields it is told about.
func rejectUnknownFields(base map[string]json.RawMessage, allowed map[string]bool) error {
	var unknown []string
	for key := range base {
		if !allowed[key] {
			unknown = append(unknown, fmt.Sprintf("%q", key))
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	slices.Sort(unknown)
	accepted := make([]string, 0, len(allowed))
	for key := range allowed {
		accepted = append(accepted, key)
	}
	slices.Sort(accepted)
	return fmt.Errorf("%w: fields %s are not accepted; send only %s",
		errInvalidDraftArgs, strings.Join(unknown, ", "), strings.Join(accepted, ", "))
}

// fieldError names the field to change and never its value, since the reason reaches the model.
func fieldError(key, problem string) error {
	return fmt.Errorf("%w: field %q %s", errInvalidDraftArgs, key, problem)
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
	if _, err := emailList(base, "to", true); err != nil {
		return err
	}
	if _, err := emailList(base, "cc", false); err != nil {
		return err
	}
	subject, err := requiredString(base, "subject", 998)
	if err != nil {
		return err
	}
	if strings.ContainsAny(subject, "\r\n") {
		return fieldError("subject", "must be one line")
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
			return nil, fieldError(key, "is required")
		}
		return nil, nil
	}
	var addresses []string
	if json.Unmarshal(raw, &addresses) != nil || len(addresses) > 50 || (required && len(addresses) == 0) {
		return nil, fieldError(key, "must be a list of 1 to 50 email addresses")
	}
	for _, address := range addresses {
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address || len(address) > 320 || strings.ContainsAny(address, "\r\n") {
			return nil, fieldError(key, "must hold bare email addresses, such as name@example.com")
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
		return "", fieldError(key, "is required")
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value) == "" || len(value) > limit {
		return "", fieldError(key, fmt.Sprintf("must be a non-empty string of at most %d bytes", limit))
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
		return fieldError(key, fmt.Sprintf("must be a string of at most %d bytes", limit))
	}
	return nil
}
