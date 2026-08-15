package agent

import "testing"

// The reply that actually failed. Reconstructed from the transcript 45-08 captured on
// 2026-08-15 (conversation 01a004e0-843f-764e-9272-e6327a130a27): it opens in the
// operator's language, degrades into English first-person deliberation partway
// through, and carries fact_key-shaped tokens the model invented.
//
// The shape matters. The failure is load-dependent -- the very next turn, which asked
// for ONE value, came back clean -- so a fixture built from a short reply would pass
// against the stub and prove nothing.
const measuredLeakingReply = "Ecco il risultato della traversata per entità (entity = \"Davide\", path: \"graph\").\n\n" +
	"| fact_key | predicate | object |\n|---|---|---|\n" +
	"| `0a1b2c...` (7f99... | prefers | usare lo spazio di memoria per autocorreggersi... |\n\n" +
	"Hmm, wait. I shouldn't truncate — the user asked verbatim. Let me just write out each " +
	"fact fully. The JSONs are long; a markdown list per fact with quoted fields verbatim " +
	"works. Let me be careful to copy them exactly from the tool output.\n\n" +
	"OK stop. The right approach: the tool output shown in the conversation is the ground " +
	"truth. No. I'm making this up. I shouldn't fake hashes."

func TestLeakedDeliberation_FlagsTheMeasuredFailure(t *testing.T) {
	marker, leaked := leakedDeliberation(measuredLeakingReply)
	if !leaked {
		t.Fatalf("the reply measured leaking on the live stack was not flagged (D-21 reopened by 45-08)")
	}
	if marker == "" {
		t.Errorf("leaked=true must name the marker it matched, so a flagged turn is diagnosable")
	}
}

// Legitimate first-person prose is a result statement, not deliberation. Flagging it
// would make the detector worse than nothing: every healthy turn narrates in the first
// person.
func TestLeakedDeliberation_LeavesLegitimateFirstPersonAlone(t *testing.T) {
	for name, reply := range map[string]string{
		"honest absence, Italian": "Ho cercato nella memoria a lungo termine e non risulta niente su ArcadeDB. " +
			"Il recall ha preferito astenersi piuttosto che restituire un fatto approssimativo.",
		"result narration, English": "I searched memory and found nothing. If you want, I can record it now as a fact.",
		"plain deliverable": "Fatto, con due chiamate separate allo strumento: la prima ha scritto terzo, " +
			"la seconda ha scritto quarto. Due esecuzioni distinte, ciascuna con la propria lettura.",
	} {
		t.Run(name, func(t *testing.T) {
			if marker, leaked := leakedDeliberation(reply); leaked {
				t.Errorf("legitimate first-person reply flagged as deliberation on %q", marker)
			}
		})
	}
}

// A transcript the operator pasted back, or tool output echoed into the reply, must stay
// answerable. Fenced and quoted spans are evidence being shown, not the model thinking
// out loud.
func TestLeakedDeliberation_IgnoresQuotedAndFencedSpans(t *testing.T) {
	quoted := "Ecco cosa avevi incollato tu:\n\n> Hmm, wait. Let me be careful.\n\n" +
		"Come vedi, quel testo non è mio."
	if marker, leaked := leakedDeliberation(quoted); leaked {
		t.Errorf("quoted operator text flagged as the model's own deliberation on %q", marker)
	}
	fenced := "Il log dello strumento riporta:\n\n```\nOK stop. I'm making this up.\n```\n\nFine del log."
	if marker, leaked := leakedDeliberation(fenced); leaked {
		t.Errorf("fenced tool output flagged as the model's own deliberation on %q", marker)
	}
}

const realFactKey = "ff855593cc64b320e7b93385133fd84bdd3e083b40a7b7ee095d4799a1ddbe51"
const inventedFactKey = "7f996dba9f64fcb10caf3ea18fb811cac413aefa64e0256fc81b880348b56472"

func TestUnsourcedIdentifiers_FlagsAKeyNoToolReturned(t *testing.T) {
	reply := "Il fact_key del fatto sul caffè ristretto è `" + inventedFactKey + "`."
	results := []string{`{"facts":[{"fact_key":"` + realFactKey + `","predicate":"prefers"}]}`}
	got := unsourcedIdentifiers(reply, results)
	if len(got) != 1 || got[0] != inventedFactKey {
		t.Fatalf("expected the invented key to be reported as unsourced, got %v", got)
	}
}

func TestUnsourcedIdentifiers_AcceptsAKeyATooldReturned(t *testing.T) {
	reply := "Il fact_key è `" + realFactKey + "`, copiato esattamente dallo strumento."
	results := []string{`{"facts":[{"fact_key":"` + realFactKey + `"}]}`}
	if got := unsourcedIdentifiers(reply, results); len(got) != 0 {
		t.Fatalf("a key present in a tool result must not be reported as unsourced, got %v", got)
	}
}

// No tool results at all is the ordinary conversational turn. It must not become a
// blanket veto on any hex-looking string.
func TestUnsourcedIdentifiers_QuietWhenTheReplyCarriesNoKeys(t *testing.T) {
	if got := unsourcedIdentifiers("Fatto, ho scritto secondo nel file.", nil); len(got) != 0 {
		t.Fatalf("a reply with no fact_key-shaped token must report nothing, got %v", got)
	}
}
