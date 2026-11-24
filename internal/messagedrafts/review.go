package messagedrafts

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/chetto1983/aura/internal/pimprovider"
)

// reviewArgs are the fields a review shows, from both trusted tools' arguments.
type reviewArgs struct {
	Recipient   string            `json:"recipient"`
	Message     string            `json:"message"`
	AccountID   string            `json:"accountId"`
	To          []string          `json:"to"`
	CC          []string          `json:"cc"`
	Subject     string            `json:"subject"`
	Body        string            `json:"body"`
	BodyFormat  string            `json:"bodyFormat"`
	Attachments []json.RawMessage `json:"attachments"`
}

// reviewed reads what will be sent: the edited arguments when the review fixed them, else the
// original ones.
func (d Draft) reviewed() (reviewArgs, error) {
	raw := d.EffectiveArgs
	if len(raw) == 0 {
		raw = d.OriginalArgs
	}
	if d.Target == legacyEmailTarget {
		var err error
		if raw, err = withoutAction(raw); err != nil {
			return reviewArgs{}, err
		}
	}
	var args reviewArgs
	// Additional fields (such as multipart alternatives and WhatsApp quote metadata)
	// cannot be approved on a surface that only shows these fields.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&args)
	return args, err
}

// withoutAction removes the multiplexed tool's "action" key from a draft stored before the PIM split.
// The strict decode would otherwise stop at it, and jsonb orders keys shortest first, so it stops
// after to, cc and body but before subject and accountId: the history would lose both.
func withoutAction(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	delete(fields, "action")
	return json.Marshal(fields)
}

// InlineContent contains the fields an operator can review on a text-only card.
type InlineContent struct {
	Recipient, CC, Subject, Text, BodyFormat string
	AttachmentCount                          int
	Complete                                 bool
}

// InlineReview marks content complete only when every outbound field can be shown.
func (d Draft) InlineReview() InlineContent {
	args, err := d.reviewed()
	if d.Target.Recipe == "recipe:whatsapp" {
		return InlineContent{Recipient: args.Recipient, Text: args.Message, Complete: err == nil && args.Recipient != "" && args.Message != ""}
	}
	format := strings.ToLower(args.BodyFormat)
	if format == "" {
		format = "html" // the PIM's default
	}
	return InlineContent{
		Recipient: strings.Join(args.To, ", "), CC: strings.Join(args.CC, ", "), Subject: args.Subject,
		Text: args.Body, BodyFormat: format, AttachmentCount: len(args.Attachments),
		Complete: err == nil && d.Target == emailTarget &&
			len(args.To) > 0 && (format == "text" || format == "html") && len(args.Attachments) == 0,
	}
}

// Review is the primary recipient, subject and text used in delivery summaries.
func (d Draft) Review() (recipient, subject, text string) {
	content := d.InlineReview()
	return content.Recipient, content.Subject, content.Text
}

// SendingAccount is the account an email leaves from, "" when the arguments name none: the PIM
// then picks one itself, by the recipient's domain or else its first account.
func (d Draft) SendingAccount() string {
	if d.Target.Recipe == "recipe:whatsapp" {
		return ""
	}
	args, _ := d.reviewed()
	return args.AccountID
}

// Sender is the account an email leaves from, by the name the operator gave it and the address the PIM
// reported when the draft was made; both empty when the draft names no account.
func (d Draft) Sender() (name, address string) {
	account := d.SendingAccount()
	if account == "" {
		return "", ""
	}
	return pimprovider.AccountName(account), d.SenderAddress
}
