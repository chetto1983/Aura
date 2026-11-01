package pimwatch

import (
	"context"
	"fmt"
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
// teaches nothing. A calendar item teaches nothing: rules do not apply to events.
func (s *Service) Correct(ctx context.Context, identityID string, c Correction) (Verdict, *Rule, error) {
	if err := s.checkCorrection(ctx, identityID, c); err != nil {
		return Verdict{}, nil, err
	}
	sender := s.senderOf(ctx, c)
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
	tier, label := learned(c, past)
	if tier == "" && label == "" {
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
		return fmt.Errorf("%w: a correction changes a tier or a label", ErrInvalid)
	}
	if c.Tier != "" && !c.Tier.Valid() {
		return fmt.Errorf("%w: the tier is now, today or later", ErrInvalid)
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
		return fmt.Errorf("%w: no such label", ErrInvalid)
	}
	return nil
}

// senderOf reads a mail's sender and returns its key, or "" when it cannot.
func (s *Service) senderOf(ctx context.Context, c Correction) string {
	if s.PIM == nil || c.Resource != ResourceMail {
		return ""
	}
	result, err := s.PIM.Items(ctx, c.AccountID, c.Resource, []ItemRef{{ID: c.ItemID}})
	if err != nil || len(result.Items) == 0 {
		return ""
	}
	address, _ := SenderKey(result.Items[0].From)
	return address
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
func (s *Service) AddRule(ctx context.Context, identityID string, in RuleInput) (Rule, error) {
	key, kind, err := parseRuleKey(in.Key)
	if err != nil {
		return Rule{}, err
	}
	if in.Tier == "" && in.LabelID == "" {
		return Rule{}, fmt.Errorf("%w: a rule gives a tier or a label", ErrInvalid)
	}
	if in.Tier != "" && !in.Tier.Valid() {
		return Rule{}, fmt.Errorf("%w: the tier is now, today or later", ErrInvalid)
	}
	if in.LabelID != "" {
		if err := s.checkLabel(ctx, identityID, in.LabelID); err != nil {
			return Rule{}, err
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
		return "", "", fmt.Errorf("%w: %q is not an address", ErrInvalid, raw)
	}
	if !strings.Contains(key, ".") || strings.ContainsAny(key, " \t\r\n") {
		return "", "", fmt.Errorf("%w: a rule names an address or a domain", ErrInvalid)
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
	return "", fmt.Errorf("%w: no label is called %q", ErrInvalid, name)
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
	return s.Store.AddLabel(ctx, identityID, strings.TrimSpace(name), tier, position)
}

// UpdateLabel renames a label and sets its tier.
func (s *Service) UpdateLabel(ctx context.Context, identityID, labelID, name string, tier Tier) (Label, error) {
	if err := checkLabelInput(name, tier); err != nil {
		return Label{}, err
	}
	return s.Store.UpdateLabel(ctx, identityID, labelID, strings.TrimSpace(name), tier)
}

// labelNameRunes is what the labels table's CHECK allows.
const labelNameRunes = 60

func checkLabelInput(name string, tier Tier) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > labelNameRunes {
		return fmt.Errorf("%w: a label's name is 1 to %d characters", ErrInvalid, labelNameRunes)
	}
	if tier != "" && !tier.Valid() {
		return fmt.Errorf("%w: the tier is now, today or later", ErrInvalid)
	}
	return nil
}
