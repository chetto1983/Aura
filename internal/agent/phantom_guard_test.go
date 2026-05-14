package agent

import "testing"

// toolsFn returns a ToolNamesFn that yields a fixed slice — handy for tests.
func toolsFn(names ...string) func() []string {
	return func() []string { return names }
}

func TestPhantomToolGuard_LooksPhantom(t *testing.T) {
	guard := &PhantomToolGuard{
		ToolNamesFn: toolsFn("task", "wiki_page", "search_memory", "web"),
	}
	emptyCalls := map[string]bool{}

	cases := []struct {
		name           string
		content        string
		hasCalls       bool
		calledThisTurn map[string]bool
		wantPhantom    bool
	}{
		// --- positives: tool name mentioned, not called this turn ---
		{
			name:        "italian eseguito run_now sul task",
			content:     "Eseguito run_now sul task — l'agent_job è partito",
			wantPhantom: true,
		},
		{
			name:        "italian wiki_page mentioned",
			content:     "Ho creato la pagina con wiki_page e ora la rileggo per controllo.",
			wantPhantom: true,
		},
		{
			name:        "english search_memory mentioned",
			content:     "I just ran search_memory and pulled the relevant pages.",
			wantPhantom: true,
		},

		// --- negatives: legitimate patterns that must NOT trigger ---
		{
			name:        "no tool names in plain reply",
			content:     "Ciao Davide, dimmi pure di cosa hai bisogno.",
			wantPhantom: false,
		},
		{
			name:        "very short reply with tool name (filtered by min-chars)",
			content:     "task ok",
			wantPhantom: false,
		},
		{
			name:        "substring inside a longer word (multitasking)",
			content:     "Multitasking è la specialità del mio orchestrator interno.",
			wantPhantom: false,
		},
		{
			name:        "plural tasks should not match singular task",
			content:     "Hai diversi tasks in coda e tutti aspettano la finestra di manutenzione.",
			wantPhantom: false,
		},
		{
			name:        "empty content",
			content:     "",
			wantPhantom: false,
		},

		// --- guard is a no-op when the response itself has tool_use ---
		{
			name:        "tool name mentioned but tool_use present",
			content:     "Ho usato task per schedulare il reminder come richiesto.",
			hasCalls:    true,
			wantPhantom: false,
		},

		// --- guard ignores tool names that were ACTUALLY called this turn ---
		{
			name:           "task mentioned and task called this turn",
			content:        "Ho schedulato il reminder con task, è in coda per le 17.",
			calledThisTurn: map[string]bool{"task": true},
			wantPhantom:    false,
		},
		{
			name:           "task called, but wiki_page mentioned without call",
			content:        "Ho schedulato il task; la pagina wiki_page sarà aggiornata dopo.",
			calledThisTurn: map[string]bool{"task": true},
			wantPhantom:    true,
		},

		// --- Wave 2.10.b regression guards (live-debug 2026-05-13):
		{
			name:        "didactic — tool name inside backticks (no claim)",
			content:     "Ecco come funziono: di solito chiamo `wiki_page` quando devo salvare una pagina, e `task` per gli scheduling. È tutto qui, niente di magico.",
			wantPhantom: false,
		},
		{
			name: "didactic — tool name inside fenced code block (no claim)",
			content: "Il flow è questo:\n\n```\n1. Chiamo wiki_page(action=\"append\", ...)\n2. Aggiorno l'indice\n```\n\nFammi sapere se vuoi più dettagli.",
			wantPhantom: false,
		},
		{
			name:        "descriptive — present-tense how-it-works (no past claim)",
			content:     "In genere chiamo wiki_page solo quando l'utente conferma. Non lo invoco mai per esempi ipotetici. Funziona così da sempre.",
			wantPhantom: false,
		},
		{
			name:        "descriptive — future-tense plan (no past claim yet)",
			content:     "Adesso ti chiamo wiki_page per aggiornare la pagina, poi tornerò qui con il risultato.",
			wantPhantom: false,
		},

		// --- Wave 2.10.b: real-claim path still fires when both signals align
		{
			name:        "real claim — italian past-tense performative + bare tool name",
			content:     "Ho schedulato il reminder con task, è in coda per le 17 di oggi.",
			wantPhantom: true,
		},
		{
			name:        "real claim — english past-tense + bare tool name",
			content:     "I just called search_memory to pull the related pages — found three matches.",
			wantPhantom: true,
		},

		// --- Wave 2.10.b: proximity check (live-debug 2026-05-13)
		{
			name: "mixed — past claim about A, prospective about B (out of proximity window)",
			content: "Ho cercato online le ultime informazioni e le ho già raccolte. " +
				"Se vuoi posso anche salvarle nella tua knowledge base, dimmi te. " +
				"Se confermi, userò wiki_page per scriverle nella pagina dedicata. " +
				"In ogni caso, ecco il riassunto.",
			calledThisTurn: map[string]bool{},
			wantPhantom:    false,
		},
		{
			name:        "mixed — past claim ABOUT a tool that WAS called (no phantom)",
			content:     "Ho cercato online con search_memory e trovato tre pagine pertinenti. Eccole.",
			calledThisTurn: map[string]bool{"search_memory": true},
			wantPhantom: false,
		},
		{
			name: "in-proximity claim about an UNcalled tool (real phantom, far-paragraph other claim does not save us)",
			content: "Buongiorno Davide.\n\nPer il primo punto, devo dire che ti ho già aggiornato la dashboard. " +
				"Per il secondo, è una cosa che richiede un attimo di lavoro. " +
				"Ho schedulato il task reminder-X per ricordartelo domani alle 9.",
			calledThisTurn: map[string]bool{}, // task was NOT called
			wantPhantom:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := tc.calledThisTurn
			if called == nil {
				called = emptyCalls
			}
			got := guard.LooksPhantom(tc.content, tc.hasCalls, called)
			if got != tc.wantPhantom {
				t.Fatalf("LooksPhantom(%q, hasCalls=%v, called=%v) = %v, want %v",
					tc.content, tc.hasCalls, called, got, tc.wantPhantom)
			}
		})
	}
}

func TestPhantomToolGuard_NilGuardIsInert(t *testing.T) {
	var guard *PhantomToolGuard
	if guard.LooksPhantom("Eseguito run_now sul task wave-X-foobar", false, nil) {
		t.Fatal("nil guard must never report phantom")
	}
	if guard.RetriesAllowed() != 0 {
		t.Fatalf("nil guard retriesAllowed = %d, want 0", guard.RetriesAllowed())
	}
}

func TestPhantomToolGuard_NilToolNamesFnIsInert(t *testing.T) {
	guard := &PhantomToolGuard{}
	if guard.LooksPhantom("Ho schedulato il task wave-X-foobar", false, nil) {
		t.Fatal("guard with nil ToolNamesFn must never report phantom")
	}
}

func TestPhantomToolGuard_EmptyRegistryIsInert(t *testing.T) {
	guard := &PhantomToolGuard{ToolNamesFn: toolsFn()}
	if guard.LooksPhantom("Ho schedulato il task wave-X-foobar", false, nil) {
		t.Fatal("guard with empty registry must never report phantom")
	}
}

func TestPhantomToolGuard_RetriesAllowed(t *testing.T) {
	cases := []struct {
		name string
		g    *PhantomToolGuard
		want int
	}{
		{"default zero MaxRetries", &PhantomToolGuard{}, 1},
		{"explicit 1", &PhantomToolGuard{MaxRetries: 1}, 1},
		{"explicit 3", &PhantomToolGuard{MaxRetries: 3}, 3},
		{"negative falls to default", &PhantomToolGuard{MaxRetries: -5}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.g.RetriesAllowed(); got != tc.want {
				t.Fatalf("retriesAllowed = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPhantomToolGuard_CorrectionTextDefault(t *testing.T) {
	guard := &PhantomToolGuard{}
	text := guard.CorrectionText()
	if text == "" {
		t.Fatal("default correction text is empty")
	}
	for _, marker := range []string{"tool", "sistema"} {
		if !containsStr(text, marker) {
			t.Errorf("correction text missing %q: %s", marker, text)
		}
	}
}

func TestPhantomToolGuard_CorrectionTextOverride(t *testing.T) {
	guard := &PhantomToolGuard{CorrectionMessage: "custom override"}
	if got := guard.CorrectionText(); got != "custom override" {
		t.Fatalf("override not honored: got %q", got)
	}
}

func TestContainsAsWord(t *testing.T) {
	cases := []struct {
		haystack string
		needle   string
		want     bool
	}{
		{"ho schedulato il task wave-x", "task", true},
		{"tasks pending in the queue", "task", false}, // plural
		{"multitasking is hard", "task", false},      // substring
		{"task!", "task", true},                       // trailing punct
		{"...task", "task", true},                     // leading punct
		{"task", "task", true},                        // exact
		{"the tasking system", "task", false},        // suffix-extended
		{"", "task", false},                           // empty haystack
		{"task", "", false},                           // empty needle
	}
	for _, tc := range cases {
		got := containsAsWord(tc.haystack, tc.needle)
		if got != tc.want {
			t.Errorf("containsAsWord(%q, %q) = %v, want %v", tc.haystack, tc.needle, got, tc.want)
		}
	}
}

// containsStr is a tiny strings.Contains shim so the test file doesn't
// need a strings import next to its single use site.
func containsStr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
