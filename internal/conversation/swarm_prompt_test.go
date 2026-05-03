package conversation

import (
	"strings"
	"testing"
)

func TestSwarmRoutingPromptMentionsReadOnlyAndTool(t *testing.T) {
	got := SwarmRoutingPrompt()
	for _, want := range []string{"run_aurabot_swarm", "read-only", "direct tools"} {
		if !strings.Contains(got, want) {
			t.Fatalf("SwarmRoutingPrompt() missing %q:\n%s", want, got)
		}
	}
}

func TestSwarmTurnHintForBroadSecondBrainRead(t *testing.T) {
	got := SwarmTurnHint("Analizza tutta la wiki e le fonti: cosa manca nel second brain?")
	if !strings.Contains(got, "run_aurabot_swarm") {
		t.Fatalf("SwarmTurnHint() = %q, want swarm hint", got)
	}
}

func TestSwarmTurnHintSkipsSimpleLookup(t *testing.T) {
	if got := SwarmTurnHint("Leggi la pagina wiki project-aura"); got != "" {
		t.Fatalf("SwarmTurnHint(simple lookup) = %q, want empty", got)
	}
}

func TestSwarmTurnHintSkipsMutationRequests(t *testing.T) {
	prompt := "Analizza tutta la wiki e poi scrivi le modifiche mancanti."
	if got := SwarmTurnHint(prompt); got != "" {
		t.Fatalf("SwarmTurnHint(mutation) = %q, want empty", got)
	}
}

func TestLooksLikeSwarmReadGoalNeedsBroadScaleSignal(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "multi domain broad",
			text: "Review wiki, sources, and skills for second brain health",
			want: true,
		},
		{
			name: "domain broad scale",
			text: "Audit completa della wiki",
			want: true,
		},
		{
			name: "domain only",
			text: "wiki",
			want: false,
		},
		{
			name: "broad without domain",
			text: "fammi un piano completo",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LooksLikeSwarmReadGoal(tc.text); got != tc.want {
				t.Fatalf("LooksLikeSwarmReadGoal(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}
