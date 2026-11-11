package messagedrafts

import (
	"encoding/json"
	"strings"
)

// Review is what the operator reviews: the edited arguments when the review fixed them, else
// the original ones. WhatsApp names no subject; email joins its recipients.
func (d Draft) Review() (recipient, subject, text string) {
	raw := d.EffectiveArgs
	if len(raw) == 0 {
		raw = d.OriginalArgs
	}
	var args struct {
		Recipient string   `json:"recipient"`
		Message   string   `json:"message"`
		To        []string `json:"to"`
		Subject   string   `json:"subject"`
		Body      string   `json:"body"`
	}
	_ = json.Unmarshal(raw, &args)
	if d.Target.Recipe == "recipe:whatsapp" {
		return args.Recipient, "", args.Message
	}
	return strings.Join(args.To, ", "), args.Subject, args.Body
}
