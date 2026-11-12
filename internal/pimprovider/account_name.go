package pimprovider

import "strings"

// AccountName is the name a person gave a PIM account. The PIM's id is the tenant's hex, two
// underscores and that name, and the hex means nothing on a phone. Tools still need the whole id.
func AccountName(accountID string) string {
	if _, name, ok := strings.Cut(accountID, "__"); ok && name != "" {
		return name
	}
	return accountID
}
