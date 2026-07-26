package adaptive

import (
	"fmt"
	"strings"
)

// NewRegisteredCatalogDeliveryMetadata validates one ordered tool or skill
// exposure against the candidate catalog frozen before ranking. The catalog
// revision is recorded as a schema revision because it identifies the immutable
// ordering contract, not mutable result content.
func NewRegisteredCatalogDeliveryMetadata(
	kind ResultKind,
	registered []string,
	delivered []string,
	catalogRevision string,
) ([]ResultID, RevisionSet, error) {
	if kind != ResultTool && kind != ResultSkill {
		return nil, RevisionSet{}, fmt.Errorf(
			"adaptive catalog delivery kind %q is invalid",
			kind,
		)
	}
	catalog, err := newResultCatalog(map[ResultKind][]string{
		kind: registered,
	})
	if err != nil {
		return nil, RevisionSet{}, err
	}
	results := make([]ResultID, len(delivered))
	for index, id := range delivered {
		if kind == ResultTool {
			results[index], err = NewToolResultID(id, catalog)
		} else {
			results[index], err = NewSkillResultID(id, catalog)
		}
		if err != nil {
			return nil, RevisionSet{}, err
		}
	}
	if err := validateResultIDs(results); err != nil {
		return nil, RevisionSet{}, err
	}
	revisionCatalog, err := newRevisionCatalog(map[RevisionKind][]string{
		RevisionSchema: {catalogRevision},
	})
	if err != nil {
		return nil, RevisionSet{}, err
	}
	revision, err := NewRegisteredRevision(
		RevisionSchema,
		catalogRevision,
		revisionCatalog,
	)
	if err != nil {
		return nil, RevisionSet{}, err
	}
	revisions, err := NewRevisionSet(revision)
	if err != nil {
		return nil, RevisionSet{}, err
	}
	return results, revisions, nil
}

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
	if !validASCIIID(value, maxRevisionIDLength) || sensitiveCatalogID(value) {
		return false
	}
	for _, segment := range strings.FieldsFunc(value, func(character rune) bool {
		return strings.ContainsRune("-_./@+", character)
	}) {
		if len(segment) > 1 && segment[0] == 'v' && allASCIIDigits(segment[1:]) {
			return true
		}
		if len(segment) >= 12 && allLowerHex(segment) {
			return true
		}
	}
	at := strings.LastIndexByte(value, '@')
	return at > 0 && allASCIIDigits(value[at+1:])
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
