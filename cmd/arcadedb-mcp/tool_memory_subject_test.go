package main

// MEM-04's canonicalSubject tests, split out of tool_memory_test.go per the
// file-size gate (658 LOC, over the 600-LOC no-god-class cap) -- the same
// <name>_<concern>.go convention this package already follows.

import (
	"context"
	"strings"
	"testing"
)

// MEM-04 (D-19): a subject naming the operator -- by identity UUID or by the
// configured display name -- canonicalizes to one form; TrimSpace +
// case-insensitive equality against exactly those two identifiers, nothing
// else.
func TestCanonicalSubject(t *testing.T) {
	const identityID = "b130c94d-a213-463a-a797-ec124104363a"
	const displayName = "Davide"
	cases := []struct {
		name    string
		subject string
		want    string
	}{
		{"byte-equal to the identity UUID", identityID, displayName},
		{"byte-equal to the display name", displayName, displayName},
		{"identity UUID with different case", strings.ToUpper(identityID), displayName},
		{"display name with surrounding whitespace", "  Davide  ", displayName},
		{"display name with different case", "davide", displayName},
		{"identity UUID with surrounding whitespace and case", "  " + strings.ToUpper(identityID) + "  ", displayName},
		{"an unrelated subject passes through unchanged", "Torino", "Torino"},
		{"a subject merely containing the display name is untouched", "Davide Marchetti", "Davide Marchetti"},
		{"a subject merely containing the identity UUID is untouched", "not-" + identityID, "not-" + identityID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalSubject(tc.subject, identityID, displayName); got != tc.want {
				t.Fatalf("canonicalSubject(%q) = %q, want %q", tc.subject, got, tc.want)
			}
		})
	}
}

// A blank or whitespace-only subject is never canonicalized: Fact.validate
// rejects it downstream, and inventing a subject here would hide that
// rejection behind a silent rewrite.
func TestCanonicalSubjectNeverInventsABlankSubject(t *testing.T) {
	for _, blank := range []string{"", "   ", "\t\n"} {
		if got := canonicalSubject(blank, "b130c94d-a213-463a-a797-ec124104363a", "Davide"); got != blank {
			t.Fatalf("canonicalSubject(%q) = %q, want it unchanged", blank, got)
		}
	}
}

// PROBE EDGE (idempotency, D-19): the canonical form canonicalizes to
// itself, or a rehydrated turn would drift a fact to a second name and
// reopen the exact split MEM-04 exists to close.
func TestCanonicalSubjectIsIdempotent(t *testing.T) {
	const identityID = "b130c94d-a213-463a-a797-ec124104363a"
	const displayName = "Davide"
	for _, input := range []string{identityID, displayName, "  davide  ", "Torino", ""} {
		once := canonicalSubject(input, identityID, displayName)
		twice := canonicalSubject(once, identityID, displayName)
		if once != twice {
			t.Fatalf("canonicalSubject(%q) = %q, canonicalSubject(that) = %q -- not idempotent",
				input, once, twice)
		}
	}
}

// D-19: canonicalization runs in memoryUpsertFactHandler before
// arcadedb.Fact is built, so it covers every write path -- not just the
// pure function in isolation. A subject byte-equal to the caller's own
// user_identifier is what the CREATE EDGE statement actually receives.
func TestUpsertFactCanonicalizesOperatorSubject(t *testing.T) {
	client, rec := newRecordingDB(t)
	in := validFactInput()
	in.Subject = testIdentity // the identity UUID, not the display name
	_, _, err := memoryUpsertFactHandler(singleTenant(t, client), testClock, "Davide")(
		context.Background(), reqWithParentActor(testIdentity, testParentRunID), in)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	_, params, ok := rec.find("CREATE EDGE")
	if !ok {
		t.Fatal("no CREATE EDGE issued")
	}
	if params["subject_name"] != "Davide" {
		t.Fatalf("subject_name = %v, want the canonicalized display name", params["subject_name"])
	}
}
