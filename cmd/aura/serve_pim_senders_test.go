package main

import "testing"

// The PIM's list_accounts names each account's sending address (aura-pim-mcp #4). An account it
// does not list is unknown; one it lists without an address is known, with none.
func TestTheSenderIsReadFromTheAccountListing(t *testing.T) {
	listing := `{"accounts":[
	  {"accountId":"t__lavoro","provider":"imap","displayName":"tecnico","email":"tecnico@example.com"},
	  {"accountId":"t__calendario","provider":"ics","displayName":"Ferie"}]}`
	cases := []struct {
		account, address string
		known            bool
	}{
		{"t__lavoro", "tecnico@example.com", true},
		{"t__calendario", "", true},
		{"t__inventato", "", false},
	}
	for _, tc := range cases {
		address, known, err := senderIn(listing, tc.account)
		if err != nil || address != tc.address || known != tc.known {
			t.Errorf("%s: senderIn = %q, %v, %v", tc.account, address, known, err)
		}
	}
	if _, _, err := senderIn("not json", "t__lavoro"); err == nil {
		t.Fatal("an unreadable listing must be an error, not an unknown account")
	}
}
