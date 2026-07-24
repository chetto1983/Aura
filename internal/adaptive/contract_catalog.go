package adaptive

import "strings"

func sensitiveCatalogID(value string) bool {
	normalized := strings.NewReplacer("_", "-", ".", "-").Replace(strings.ToLower(value))
	if strings.Contains(normalized, "api-key") ||
		strings.Contains(normalized, "private-summary") {
		return true
	}
	for _, token := range strings.Split(normalized, "-") {
		for _, sensitive := range []string{
			"apikey",
			"secret",
			"password",
			"credential",
			"bearer",
			"token",
			"content",
		} {
			if strings.HasPrefix(token, sensitive) {
				return true
			}
		}
	}
	return false
}

func validImmutableRevisionID(value string) bool {
	if !validSafeSlug(value, maxRevisionIDLength) || sensitiveCatalogID(value) {
		return false
	}
	for _, segment := range strings.FieldsFunc(value, func(character rune) bool {
		return character == '-' || character == '_' || character == '.'
	}) {
		if len(segment) > 1 && segment[0] == 'v' && allASCIIDigits(segment[1:]) {
			return true
		}
		if len(segment) >= 12 && allLowerHex(segment) {
			return true
		}
	}
	return false
}

func allASCIIDigits(value string) bool {
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return value != ""
}

func allLowerHex(value string) bool {
	for i := range len(value) {
		if (value[i] < '0' || value[i] > '9') &&
			(value[i] < 'a' || value[i] > 'f') {
			return false
		}
	}
	return value != ""
}
