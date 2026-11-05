package pimwatch

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
)

// correctionWindow is how far apart two corrections must be to teach a rule, so that one burst of
// sorting is one opinion (spec §5.4).
const correctionWindow = 15 * time.Minute

// Correct changes a verdict's tier or label and learns from it. A sender corrected the same way
// twice, outside one burst, gets a rule, and tier and label are learned apart. The sender is read
// live by id, because no verdict keeps one, and a mail that cannot be read changes its verdict and
// teaches nothing. A calendar item teaches nothing: rules do not apply to events. Nor does a mail
// that failed authentication: a rule beats that rung, so one learned from it would let a forged
// sender reach `now`.
func (s *Service) Correct(ctx context.Context, identityID string, c Correction) (Verdict, *WrittenRule, error) {
	if err := s.checkCorrection(ctx, identityID, c); err != nil {
		return Verdict{}, nil, err
	}
	item := s.itemOf(ctx, c)
	sender, _ := SenderKey(item.From)
	var past []PastCorrection
	if sender != "" {
		var err error
		past, err = s.Store.CorrectionsBefore(ctx, identityID, sender, s.now().Add(-correctionWindow))
		if err != nil {
			return Verdict{}, nil, err
		}
	}
	verdict, err := s.Store.ApplyCorrection(ctx, identityID, sender, c)
	if err != nil {
		return Verdict{}, nil, err
	}
	if sender != "" {
		s.teach(ctx, identityID, c, item, verdict)
	}
	tier, label := learned(c, past)
	if verdict.AuthFailed || (tier == "" && label == "") {
		return verdict, nil, nil
	}
	rule, err := s.Store.LearnRule(ctx, identityID, sender, tier, label)
	if err != nil {
		return verdict, nil, err
	}
	return verdict, &rule, nil
}

func (s *Service) checkCorrection(ctx context.Context, identityID string, c Correction) error {
	if c.Tier == "" && c.LabelID == nil {
		return invalid("empty_correction", "a correction changes a tier or a label")
	}
	if c.Tier != "" && !c.Tier.Valid() {
		return invalidTier()
	}
	if c.LabelID == nil || *c.LabelID == "" {
		return nil
	}
	return s.checkLabel(ctx, identityID, *c.LabelID)
}

func (s *Service) checkLabel(ctx context.Context, identityID, labelID string) error {
	labels, err := s.Store.Labels(ctx, identityID)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(labels, func(l Label) bool { return l.ID == labelID }) {
		return invalid("unknown_label", "no such label")
	}
	return nil
}

// itemOf reads a mail live by id, and gives an empty item when it cannot.
func (s *Service) itemOf(ctx context.Context, c Correction) Item {
	if s.PIM == nil || c.Resource != ResourceMail {
		return Item{}
	}
	result, err := s.PIM.Items(ctx, c.AccountID, c.Resource, []ItemRef{{ID: c.ItemID}})
	if err != nil || len(result.Items) == 0 {
		return Item{}
	}
	return result.Items[0]
}

// teach gives the centroid a mail whose sender was read and whose tier the person changed, when
// its row has the facts (it was written after 0152) and it passed authentication: the mails that
// write a correction row. A failure leaves the correction standing and logs no sender.
func (s *Service) teach(ctx context.Context, identityID string, c Correction, item Item, verdict Verdict) {
	if s.Centroid == nil || c.Tier == "" || verdict.AuthFailed || verdict.LadderTier == "" {
		return
	}
	err := s.Centroid.Learn(ctx, identityID, Lesson{
		AccountID: c.AccountID, ItemID: c.ItemID, From: item.From, Subject: item.Subject, Facts: verdict.Facts, Tier: c.Tier,
	})
	if err != nil {
		slog.Warn("mail watch: a correction taught the centroid nothing", "error", err)
	}
}

// learned picks what an earlier correction agrees with. A correction clearing a label teaches
// nothing, because a rule cannot say "no label".
func learned(c Correction, past []PastCorrection) (tier Tier, labelID string) {
	for _, earlier := range past {
		if c.Tier != "" && earlier.Tier == c.Tier {
			tier = c.Tier
		}
		if c.LabelID != nil && *c.LabelID != "" && earlier.LabelID == *c.LabelID {
			labelID = *c.LabelID
		}
	}
	return tier, labelID
}

// RuleInput is a rule as a person gives it: a sender or a domain, then a tier and/or a label.
type RuleInput struct {
	Key     string
	Tier    Tier
	LabelID string
}

// AddRule writes a person's rule.
func (s *Service) AddRule(ctx context.Context, identityID string, in RuleInput) (WrittenRule, error) {
	key, kind, err := parseRuleKey(in.Key)
	if err != nil {
		return WrittenRule{}, err
	}
	if in.Tier == "" && in.LabelID == "" {
		return WrittenRule{}, invalid("empty_rule", "a rule gives a tier or a label")
	}
	if in.Tier != "" && !in.Tier.Valid() {
		return WrittenRule{}, invalidTier()
	}
	if in.LabelID != "" {
		if err := s.checkLabel(ctx, identityID, in.LabelID); err != nil {
			return WrittenRule{}, err
		}
	}
	return s.Store.PutRule(ctx, identityID, Rule{Key: key, Kind: kind, Tier: in.Tier, LabelID: in.LabelID})
}

// parseRuleKey reads a whole address as a sender and anything else as a domain.
func parseRuleKey(raw string) (string, KeyKind, error) {
	key := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "@")
	if strings.Contains(key, "@") {
		if address, _ := SenderKey(key); address != "" {
			return address, KeySender, nil
		}
		return "", "", invalid("invalid_address", fmt.Sprintf("%q is not an address", raw))
	}
	if !strings.Contains(key, ".") || strings.ContainsAny(key, " \t\r\n") {
		return "", "", invalid("invalid_rule_key", "a rule names an address or a domain")
	}
	return key, KeyDomain, nil
}

// Rules lists the identity's rules.
func (s *Service) Rules(ctx context.Context, identityID string) ([]Rule, error) {
	return s.Store.Rules(ctx, identityID)
}

// RemoveRule removes a rule.
func (s *Service) RemoveRule(ctx context.Context, identityID, ruleID string) error {
	return s.Store.RemoveRule(ctx, identityID, ruleID)
}

// Labels lists the identity's labels in order.
func (s *Service) Labels(ctx context.Context, identityID string) ([]Label, error) {
	return s.Store.Labels(ctx, identityID)
}

// RemoveLabel removes a label.
func (s *Service) RemoveLabel(ctx context.Context, identityID, labelID string) error {
	return s.Store.RemoveLabel(ctx, identityID, labelID)
}

// LabelID finds a label by its name, the way a person says it.
func (s *Service) LabelID(ctx context.Context, identityID, name string) (string, error) {
	labels, err := s.Store.Labels(ctx, identityID)
	if err != nil {
		return "", err
	}
	for _, label := range labels {
		if strings.EqualFold(label.Name, strings.TrimSpace(name)) {
			return label.ID, nil
		}
	}
	return "", invalid("unknown_label", fmt.Sprintf("no label is called %q", name))
}

// AddLabel appends a label to the list.
func (s *Service) AddLabel(ctx context.Context, identityID, name string, tier Tier) (Label, error) {
	if err := checkLabelInput(name, tier); err != nil {
		return Label{}, err
	}
	labels, err := s.Store.Labels(ctx, identityID)
	if err != nil {
		return Label{}, err
	}
	position := 0
	for _, label := range labels {
		position = max(position, label.Position+1)
	}
	return s.Store.AddLabel(ctx, identityID, LabelInput{Name: strings.TrimSpace(name), Tier: tier}, position)
}

// UpdateLabel renames a label and sets its tier.
func (s *Service) UpdateLabel(ctx context.Context, identityID, labelID, name string, tier Tier) (Label, error) {
	if err := checkLabelInput(name, tier); err != nil {
		return Label{}, err
	}
	return s.Store.UpdateLabel(ctx, identityID, labelID, LabelInput{Name: strings.TrimSpace(name), Tier: tier})
}

// labelNameRunes is what the labels table's CHECK allows.
const labelNameRunes = 60

func checkLabelInput(name string, tier Tier) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > labelNameRunes {
		return invalid("invalid_label_name", fmt.Sprintf("a label's name is 1 to %d characters", labelNameRunes))
	}
	if tier != "" && !tier.Valid() {
		return invalidTier()
	}
	return nil
}
