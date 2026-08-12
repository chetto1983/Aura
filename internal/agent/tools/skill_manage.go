package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// SkillManageTool is the WRITE half of the skills grammar, split out of SkillTool.
//
// Why the split. `skill` used to front all ten actions behind one `action` enum, so
// tool_search promoting it for the common case — "apply the spreadsheet skill" — also
// paid for the authoring, install and snippet-lifecycle grammar the model was not
// going to use. Measured on the live registry: the one verb cost 1081 tokens as a
// floor (the spec's own comment records 1638 with a populated loader), of which 74% of
// the schema was prose describing actions. Reads are the frequent case; authoring is
// rare and deliberate, so they now cost separately.
//
// Security posture: this half keeps the fail-closed Mutating + Multiplexed pair and
// therefore still requires its per-action classifier entry, which gateway's boot-guard
// (ValidateClassifiable) asserts. The reads that moved to SkillTool were ALREADY pinned
// to scoring.Safe by the classifier's skillFixedTiers, and classify returns Safe for a
// non-Mutating tool, so their tier is unchanged by the split — nothing was de-gated.
//
// It holds no seams of its own: it dispatches against the SAME *SkillTool the read half
// uses (one Loader, one Writer, one Alerter, one cache-invalidation path), so a write
// here is visible to info/use in the same turn exactly as before. A composed pointer
// rather than an embedded one keeps Spec/Execute unambiguous.
type SkillManageTool struct {
	// Skills carries the loader/writer/alerter seams and the action implementations.
	// Nil is a configuration error, reported by Execute rather than panicking.
	Skills *SkillTool

	routerOnce sync.Once
	router     *ActionRouter
}

// Spec returns the deferred manifest entry for the authoring half. The Description is
// deliberately short: the skill MANIFEST lives on the read tool, and duplicating it here
// would pay for the same list twice whenever both halves are promoted.
func (t *SkillManageTool) Spec() Spec {
	return Spec{
		Name:    "skill_manage",
		Summary: "Author, install, and manage the skills in your library.",
		Description: "Write half of the skills library: author (create/update/delete), install from the open ecosystem, " +
			"and manage runnable snippets (save_snippet/archive/restore). " +
			"Use `skill` to list, read or apply an existing skill — this tool is for changing the library. " +
			"Every write takes effect immediately: what you create, update, install or save is usable on this same turn.",
		Parameters: json.RawMessage(skillManageParamsSchema),
		Deferred:   true,
		// D-02/D-02d: still the fail-closed Mutating floor with the Multiplexed hint the
		// gateway boot-guard reads — every action here writes, so there is no read to
		// de-escalate and no landmine to avoid.
		Mutating:       true,
		Multiplexed:    true,
		OperationScope: OperationScopeAgent, OperationNormalizer: OperationNormalizerCanonical,
		ReplayPolicy: ReplayToolResult,
	}
}

// Execute parses the `action` discriminator and dispatches through the ActionRouter.
// It never panics on a bad action — the router returns a structured error.
func (t *SkillManageTool) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	if t.Skills == nil {
		return ToolResult{}, fmt.Errorf("skill_manage: no skills tool wired")
	}
	var head struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return ToolResult{}, fmt.Errorf("skill_manage args: %w", err)
	}
	if head.Action == "" {
		return ToolResult{}, fmt.Errorf("skill_manage: action is required")
	}
	return t.actionRouter().Dispatch(ctx, head.Action, raw)
}

func (t *SkillManageTool) actionRouter() *ActionRouter {
	t.routerOnce.Do(func() {
		s := t.Skills
		t.router = NewActionRouter(map[string]ActionFunc{
			// Authoring (11-05): validate -> write -> materialize -> audit, all in the one
			// call (amendment #97). There is no approve action because there is nothing left
			// to approve.
			"create": s.actionCreate,
			"update": s.actionUpdate,
			"delete": s.actionDelete,
			// install runs the SAME Installer the cockpit uses: fetch, validate, land it
			// active, audit. It used to be deliberately absent (amendment #51 / D-40 left
			// self-extension to the find-skills always-on skill and the CLI) — but a CLI
			// install writes into the directory it is standing in, not into the loader's
			// roots, so the loop the model was taught could not close. A skill you cannot
			// install through the tool is a skill Aura never learns about.
			"install": s.actionInstall,
			// Snippet lifecycle (18-03): save_snippet is the in-loop save (a normal result,
			// NEVER a pause); restore is Archive's inverse; archive is the manual SAFE-tier
			// de-materialize.
			"save_snippet": s.actionSaveSnippet,
			"restore":      s.actionRestore,
			"archive":      s.actionArchive,
		})
	})
	return t.router
}
