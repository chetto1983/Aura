package pimwatch

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/examplebank"
)

// The urgency centroid as measured on 2026-11-02 and 2026-11-03
// (docs/superpowers/verification/2026-11-02-mail-classification-spike.md): sender and subject,
// four facts at 0.3, a correction worth six seeds. A different text, fact or weight is another
// arm, and is measured before it ships.
const (
	mailTierDomain   = "mail_tier"
	correctionWeight = 6
	factWeight       = 0.3
	// shadowTimeout bounds what the shadow adds to a tick or a correction: the alert waits for the
	// tick, so a hung embedder must not hold it longer than this.
	shadowTimeout = 5 * time.Second
)

// mailTierSeeds are the pilot's 200 invented mails, every sender moved under .example and 13
// subjects rewritten to name no real company, with their frozen tiers. Measured with semindex's
// math on 2026-11-03: the same 56.7 macro F1 as the spike's seeds.
//
//go:embed seeds/mail_tier.json
var mailTierSeeds []byte

// Centroid is the shadow's port: what the identity's centroid would say about a page's mails, what
// it learns from a correction, and what it forgets with an account. Nil switches the shadow off.
type Centroid interface {
	Judge(ctx context.Context, identityID string, mails []ShadowMail) ([]CentroidAnswer, error)
	Learn(ctx context.Context, identityID string, lesson Lesson) error
	Forget(ctx context.Context, identityID, accountID string) error
}

// ShadowMail is what the centroid reads of a mail. From is the PIM's, display name and all.
type ShadowMail struct {
	From, Subject string
	Facts         MailFacts
}

// CentroidAnswer is the centroid's tier and its top-2 margin. Tier is "" when it could not answer.
type CentroidAnswer struct {
	Tier   Tier
	Margin float64
}

// Lesson is a correction as the centroid learns it: the mail, read live and dropped, and its tier.
type Lesson struct {
	AccountID, ItemID, From, Subject string
	Facts                            MailFacts
	Tier                             Tier
}

// OwnerLister lists the accounts whose examples still count. *Store satisfies it.
type OwnerLister interface {
	CorrectedAccounts(ctx context.Context, identityID string) ([]string, error)
}

// CentroidShadow is the Centroid on the shared example bank.
type CentroidShadow struct {
	bank    *examplebank.Bank
	domain  examplebank.Domain
	timeout time.Duration
}

// mailTierSeed is one line of the seeds file.
type mailTierSeed struct {
	From              string `json:"from"`
	Subject           string `json:"subject"`
	WrittenTo         bool   `json:"written_to"`
	ProviderImportant bool   `json:"provider_important"`
	AuthFailed        bool   `json:"auth_failed"`
	Bulk              bool   `json:"bulk"`
	Tier              Tier   `json:"tier"`
}

// NewCentroidShadow reads the shipped seeds and builds the mail-tier domain on bank.
func NewCentroidShadow(bank *examplebank.Bank, owners OwnerLister) (*CentroidShadow, error) {
	var raw []mailTierSeed
	if err := json.Unmarshal(mailTierSeeds, &raw); err != nil {
		return nil, fmt.Errorf("read the mail tier seeds: %w", err)
	}
	seeds := make([]examplebank.Seed, 0, len(raw))
	for _, seed := range raw {
		if !seed.Tier.Valid() {
			return nil, fmt.Errorf("a mail tier seed has the tier %q", seed.Tier)
		}
		facts := MailFacts{WrittenTo: seed.WrittenTo, ProviderImportant: seed.ProviderImportant, AuthFailed: seed.AuthFailed, Bulk: seed.Bulk}
		seeds = append(seeds, examplebank.Seed{Text: mailText(seed.From, seed.Subject), Extra: facts.extra(), Label: string(seed.Tier)})
	}
	return &CentroidShadow{bank: bank, timeout: shadowTimeout, domain: examplebank.Domain{
		Name: mailTierDomain, Seeds: seeds, ExampleWeight: correctionWeight,
		Owners: func(ctx context.Context, identityID string) (map[string]bool, error) {
			accounts, err := owners.CorrectedAccounts(ctx, identityID)
			if err != nil {
				return nil, err
			}
			alive := make(map[string]bool, len(accounts))
			for _, account := range accounts {
				alive[account] = true
			}
			return alive, nil
		},
	}}, nil
}

// Judge answers for every mail of a page with one embedding call. The classifier comes first, so a
// page is never embedded while the seeds warm: on a CPU sidecar that call would compete with the
// seeds' own and be thrown away.
func (c *CentroidShadow) Judge(ctx context.Context, identityID string, mails []ShadowMail) ([]CentroidAnswer, error) {
	if len(mails) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	live, err := c.bank.Embedder.Space(ctx)
	if err != nil {
		return nil, err
	}
	classifier, err := c.bank.Classifier(ctx, identityID, c.domain, live.ID)
	if err != nil {
		return nil, err
	}
	texts := make([]string, len(mails))
	for i, mail := range mails {
		texts[i] = mailText(mail.From, mail.Subject)
	}
	vectors, space, err := c.bank.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if space != live.ID {
		return nil, errors.New("pimwatch: the embedding space changed while judging a page")
	}
	answers := make([]CentroidAnswer, len(mails))
	for i, mail := range mails {
		if verdict := classifier.RankVecs(examplebank.Row(vectors[i], mail.Facts.extra())); verdict.Ok {
			answers[i] = CentroidAnswer{Tier: Tier(verdict.Label), Margin: verdict.Margin}
		}
	}
	return answers, nil
}

// Learn stores a correction as an example of its account: a second one of the same mail replaces it.
func (c *CentroidShadow) Learn(ctx context.Context, identityID string, lesson Lesson) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	vectors, space, err := c.bank.Embed(ctx, []string{mailText(lesson.From, lesson.Subject)})
	if err != nil {
		return fmt.Errorf("embed the correction: %w", err)
	}
	err = c.bank.Put(ctx, identityID, c.domain, arcadedb.Example{
		Domain: mailTierDomain, Owner: lesson.AccountID, Ref: lesson.ItemID, Label: string(lesson.Tier),
		Vector: examplebank.Row(vectors[0], lesson.Facts.extra()), Space: space, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("store the correction's example: %w", err)
	}
	return nil
}

// Forget removes an account's examples. It runs inside the cockpit's account removal, so it is
// bounded like the rest of the shadow: what a timeout leaves, the next build forgets as an orphan.
func (c *CentroidShadow) Forget(ctx context.Context, identityID, accountID string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.bank.ForgetOwner(ctx, identityID, c.domain, accountID)
}

// factsOf reads the four facts from what the PIM said. An unknown WrittenTo is false.
func factsOf(s Signals) MailFacts {
	return MailFacts{
		WrittenTo: s.WrittenTo != nil && *s.WrittenTo, ProviderImportant: s.ProviderImportant,
		AuthFailed: s.AuthFailed, Bulk: s.Bulk,
	}
}

// extra is the facts as the vector's last four components, in a fixed order.
func (f MailFacts) extra() []float64 {
	out := make([]float64, 0, 4)
	for _, on := range []bool{f.WrittenTo, f.ProviderImportant, f.AuthFailed, f.Bulk} {
		value := 0.0
		if on {
			value = factWeight
		}
		out = append(out, value)
	}
	return out
}

// mailText is the text the centroid embeds: the sender's bare, lower-case address as the seeds
// carry it, then the subject. A From that does not parse is embedded as it came.
func mailText(from, subject string) string {
	if address, _ := SenderKey(from); address != "" {
		from = address
	}
	return from + "\n" + subject
}
