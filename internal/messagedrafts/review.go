package messagedrafts

import (
	"encoding/json"
	"strings"

	"github.com/chetto1983/aura/internal/pimprovider"
)

// reviewArgs are the fields a review shows, from both trusted tools' arguments.
type reviewArgs struct {
	Recipient string   `json:"recipient"`
	Message   string   `json:"message"`
	AccountID string   `json:"accountId"`
	To        []string `json:"to"`
	Subject   string   `json:"subject"`
	Body      string   `json:"body"`
}

// reviewed reads what will be sent: the edited arguments when the review fixed them, else the
// original ones.
func (d Draft) reviewed() reviewArgs {
	raw := d.EffectiveArgs
	if len(raw) == 0 {
		raw = d.OriginalArgs
	}
	var args reviewArgs
	_ = json.Unmarshal(raw, &args)
	return args
}

// Review is what the operator reviews. WhatsApp names no subject; email joins its recipients.
func (d Draft) Review() (recipient, subject, text string) {
	args := d.reviewed()
	if d.Target.Recipe == "recipe:whatsapp" {
		return args.Recipient, "", args.Message
	}
	return strings.Join(args.To, ", "), args.Subject, args.Body
}

// SendingAccount is the account an email leaves from, "" when the arguments name none: the PIM
// then picks one itself, by the recipient's domain or else its first account.
func (d Draft) SendingAccount() string {
	if d.Target.Recipe == "recipe:whatsapp" {
		return ""
	}
	return d.reviewed().AccountID
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
