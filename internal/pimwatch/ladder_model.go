package pimwatch

import (
	"cmp"
	"slices"
	"strings"
)

// maxDescribedLabels keeps the question within llama.cpp's 26 one-letter codes, the none option
// included.
const maxDescribedLabels = 24

// tierRank orders the tiers by how soon they reach the person: later 1, today 2, now 3.
func tierRank(t Tier) int {
	switch t {
	case TierNow:
		return 3
	case TierToday:
		return 2
	case TierLater:
		return 1
	default:
		return 0
	}
}

// ApplyModel lays the decision model's label over what the ladder decided
// (docs/superpowers/specs/2026-11-03-decision-model-labels-design.md, decision 1 as amended). The
// label always stays, and its tier or its discard moves the mail only above the people floor: a
// rule's tier, failed authentication, and anyone the account has written to, or may have, are
// never lowered or discarded. decided_by becomes `model` only when the label moved the tier.
func ApplyModel(book Book, ladder Decision, s Signals, labelID string) Decision {
	label := book.labels[labelID]
	d := ladder
	d.LabelID = labelID
	switch {
	case ladder.By == ByRule, s.AuthFailed:
		return d
	case !s.Bulk && (s.WrittenTo == nil || *s.WrittenTo):
		if label.Tier != "" && tierRank(label.Tier) > tierRank(d.Tier) {
			d.Tier, d.By = label.Tier, ByModel
		}
		return d
	case label.Discard:
		d.Tier, d.By, d.Discarded = TierLater, ByModel, true
	case label.Tier != "" && label.Tier != d.Tier:
		d.Tier, d.By = label.Tier, ByModel
	}
	return d
}

// DescribedLabels are the labels the decision model may choose: those with a description and a name
// that is not the none option's, by position then name, at most maxDescribedLabels.
func (b Book) DescribedLabels() []Label {
	labels := make([]Label, 0, len(b.labels))
	for _, label := range b.labels {
		if strings.TrimSpace(label.Description) != "" && !reservedLabelName(label.Name) {
			labels = append(labels, label)
		}
	}
	slices.SortFunc(labels, func(a, b Label) int {
		return cmp.Or(cmp.Compare(a.Position, b.Position), strings.Compare(a.Name, b.Name))
	})
	return labels[:min(len(labels), maxDescribedLabels)]
}
