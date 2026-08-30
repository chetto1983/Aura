package telegram

import (
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

// Amendment #188: the terminal STATE_DELTA's limit_hit / steps_consumed (the keys
// internal/agent's terminalBudgetEvent + finalizeEvent emit) render as a visible
// "turn stopped" line under the cost footer, so a turn cut at the step cap no
// longer reads as a finished one on Telegram. A delta without them adds nothing.
func TestStatusPaneRendersBudgetTrip(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePane(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewStateDeltaEvent([]events.JSONPatchOperation{
			{Op: "replace", Path: "/cost_usd", Value: "0.0012"},
			{Op: "replace", Path: "/limit_hit", Value: "max_steps"},
			{Op: "replace", Path: "/steps_consumed", Value: 25},
			{Op: "replace", Path: "/termination_reason", Value: "budget_exhausted"},
		}),
		events.NewRunFinishedEvent("t", "r"),
	})
	got := lastText(bot)
	for _, want := range []string{"Costo: $0.0012", "Turno interrotto", "limite di passi raggiunto", "25 passi", "continua"} {
		if !strings.Contains(got, want) {
			t.Fatalf("pane text lacks %q:\n%s", want, got)
		}
	}

	quiet := newFakeBot()
	drivePane(quiet, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewStateDeltaEvent([]events.JSONPatchOperation{
			{Op: "replace", Path: "/cost_usd", Value: "0.0012"},
		}),
		events.NewRunFinishedEvent("t", "r"),
	})
	if strings.Contains(lastText(quiet), "Turno interrotto") {
		t.Fatalf("a turn that ended on its own must not show a budget trip:\n%s", lastText(quiet))
	}

	wallclock := newFakeBot()
	drivePane(wallclock, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewStateDeltaEvent([]events.JSONPatchOperation{
			{Op: "replace", Path: "/limit_hit", Value: "wallclock"},
		}),
		events.NewRunFinishedEvent("t", "r"),
	})
	if got := lastText(wallclock); !strings.Contains(got, "limite di tempo raggiunto") {
		t.Fatalf("wallclock trip not named:\n%s", got)
	}
}
