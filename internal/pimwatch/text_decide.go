package pimwatch

import "strings"

// noneName is the decision model's "none of them" option, in the identity's language.
func noneName(lang Lang) string {
	if lang == LangIT {
		return "Nessuna"
	}
	return "None"
}

// reservedLabelName says whether a label's name would collide with the none option of either
// language, so a language change cannot turn a person's label into it.
func reservedLabelName(name string) bool {
	name = strings.TrimSpace(name)
	return strings.EqualFold(name, noneName(LangIT)) || strings.EqualFold(name, noneName(LangEN))
}
