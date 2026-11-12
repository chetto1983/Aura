package pimprovider

import "testing"

// The PIM's account id is the tenant's hex, two underscores and the name the person gave the
// account; the hex means nothing on a phone.
func TestAccountNameIsTheNameAfterTheTenant(t *testing.T) {
	t.Parallel()
	for id, want := range map[string]string{
		"448ddbe196ea405d82194a3d52a425c0__personale": "personale",
		"plain":    "plain",
		"tenant__": "tenant__",
		"a__b__c":  "b__c",
	} {
		if got := AccountName(id); got != want {
			t.Errorf("AccountName(%q) = %q, want %q", id, got, want)
		}
	}
}
