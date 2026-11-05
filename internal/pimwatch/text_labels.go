package pimwatch

// defaultLabels are the labels an identity starts with. The descriptions are the ones gate G1 measured
// (docs/superpowers/specs/2026-11-03-decision-model-labels-design.md, `spikes/d1-email-labeling/labels5/g1.py`):
// the Italian texts verbatim, and the English ones by position.
var defaultLabels = map[Lang][]Label{
	LangIT: {
		{Name: "Cliente", Description: "Clienti o potenziali clienti che chiedono preventivi, informazioni, assistenza o fanno reclami."}, //nolint:misspell // Italian "complaints", not English "reclaim".
		{Name: "Lavoro", Description: "Colleghi e collaboratori: progetti, riunioni, turni, attività del gruppo."},
		{Name: "Personale", Description: "Vita privata: famiglia, amici, scuola, salute, tempo libero."},
		{Name: "Opportunità", Description: "Proposte di lavoro, incarichi, collaborazioni o partnership rivolte a me."}, //nolint:misspell // Italian "proposals", not English "propose".
		{Name: "Pubblicità", Tier: TierLater, Description: "Promozioni, sconti, newsletter e offerte mandate a una lista."},
	},
	LangEN: {
		{Name: "Client", Description: "Customers or prospective customers asking for quotes, information or support, or complaining."},
		{Name: "Work", Description: "Colleagues and collaborators: projects, meetings, shifts, team tasks."},
		{Name: "Personal", Description: "Private life: family, friends, school, health, leisure."},
		{Name: "Opportunity", Description: "Job offers, assignments, collaborations or partnerships addressed to me."},
		{Name: "Advertising", Tier: TierLater, Description: "Promotions, discounts, newsletters and offers sent to a list."},
	},
}

// DefaultLabels is the set an identity starts with, in its language (spec §5.8).
func DefaultLabels(lang Lang) []Label {
	labels, ok := defaultLabels[lang]
	if !ok {
		labels = defaultLabels[LangEN]
	}
	out := make([]Label, len(labels))
	for i, label := range labels {
		label.Position = i
		out[i] = label
	}
	return out
}
