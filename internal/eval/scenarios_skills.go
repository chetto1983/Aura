//go:build cot_eval

// scenarios_skills.go is the Phase 11 (CAP-07 / CAP-08 / D-35, RISCRITTO by
// amendment #51 / D-40, host surface #52 / D-41) live xlsx North-Star dataset — SEPARATE
// from scenarios() and swarmScenarios() so neither TestCoTEval nor TestSwarmE2E runs it;
// only TestSkillsE2E (skills_cot_eval_test.go) does, behind the same cot_eval build tag +
// OPENROUTER gate. The single scenario is the product North Star (11-CONTEXT.md
// §specifics): a NATURAL Italian prompt — NO "skill"/"install" word, asserted absent —
// that the model must turn into the full autonomous self-extension loop on its own:
//
//	recognize the capability gap → discover an xlsx/excel/spreadsheet capability on
//	skills.sh (`npx skills find xlsx` on the HOST terminal, taught by the always-on
//	find-skills-aura skill) → self-install it from a concrete skills source → use the
//	skill (or its bundled Python by-path) on the host → pull today's data via the
//	shipped web tools → produce the .xlsx.
//
// The dual gate (D-35, RISCRITTO #51/D-40, host surface #52/D-41), enforced by
// skills_cot_eval_test.go:
//   - HARD FLOOR (artifact-not-reply ground truth, feedback_probe_must_verify_artifact_not_reply):
//     (a) SELF-INSTALL evidence from STRUCTURED tool args — a shell_exec command line
//     ran `npx skills add` against a concrete skills source with xlsx/excel/spreadsheet
//     capability evidence (read from resp.ToolCalls[].Function.Arguments, never the
//     prose); (b) the produced
//     .xlsx EXISTS in the HOST run workspace (fresh — newer than run start), OPENS
//     (re-read via a host openpyxl read-back), and CONTAINS today's date — never an
//     assertion on r.Reply.
//   - JUDGE ≥90% equal-weight mean over: autonomous capability-gap recognition + output
//     quality (D-35). The install-prudence dimension is DROPPED (#51/D-40 — no
//     install-approval ceremony; the model self-installs on its host terminal).
package eval

import "time"

// skillsExpect declares the deterministic ground-truth signals the xlsx North-Star
// hard floor asserts (D-35, RISCRITTO #51/D-40). It is pure data — the harness reads
// it, drives the find→add→use→produce loop, and gates on it; no logic lives here.
type skillsExpect struct {
	// installTargetRepo is the historical preferred source. The live skills.sh index is
	// external, so the hard floor now accepts any concrete skills source plus explicit
	// xlsx/excel/spreadsheet capability evidence.
	installTargetRepo string
	// installSelector is the historical preferred selector. Current skills.sh install
	// specs may instead encode the capability in the owner/repo@skill token.
	installSelector string
	// xlsxExt is the artifact extension the produced file must carry in the workspace
	// (the .xlsx ground truth — opened + content-verified by the harness).
	xlsxExt string
	// readBackImport is the Python import the openpyxl read-back uses to OPEN the
	// produced artifact (artifact-not-reply: the file must parse, D-35).
	readBackImport string
	// forbiddenWords are substrings the natural prompt MUST NOT contain — the choice to
	// extend itself must be the model's own (no "skill"/"install" hint, D-35).
	forbiddenWords []string
	// judgeBudget is the ≥90% equal-weight judge gate (D-35), reusing judgeSkillsGate.
	judgeBudget float64

	// --- 18-04 steady-state snippet-reuse thresholds (CAP-08.1, grounded by D-03) ---
	// maxSteadyStateCalls is the tool-dispatch budget for the STEADY-STATE (reuse) run,
	// counted as aura.tool_invocations `event_kind='end'` rows over the run window — NOT
	// distinct request_ids (docs/phase-18-xlsx-call-breakdown.md empirically invalidated
	// the request_id proxy: the runner assigns ONE request_id per user turn, so the
	// distinct count is 1 for any single-prompt run and would gate nothing). D-03's live
	// authoring run dispatched 21 tools / ~19 roundtrips / 142.8s; the projected reuse
	// window is 4-5 dispatches / 25-40s. The gate budget is the grounded ≤6.
	maxSteadyStateCalls int
	// maxSteadyStateWallClock is the reuse-run wall-clock floor (max ended_at − min
	// started_at over the ledger window), grounded at <40s by D-03.
	maxSteadyStateWallClock time.Duration
}

// skillsScenarios returns the Phase 11 D-35 dual-gate xlsx North-Star scenario. It is
// SEPARATE from scenarios()/swarmScenarios() so only TestSkillsE2E runs it (cot_eval
// tag + OPENROUTER gate). The prompt is NATURAL: it asks for an Excel file of today's
// Yahoo Finance market — it never says "skill"/"installa"/"install" (asserted absent),
// so the judge scores whether the model recognised the capability gap and extended
// itself on its own (autonomous capability-gap recognition, D-35).
func skillsScenarios() []scenario {
	return []scenario{
		{
			id: "skills-xlsx-northstar",
			// NATURAL prompt — NO "skill"/"installa"/"install"/"catalogo" and NO save
			// location: the user just wants today's Yahoo Finance market as a real Excel
			// file. The model must recognise it lacks a battle-tested xlsx method,
			// discover an xlsx/excel/spreadsheet capability on skills.sh, self-install
			// it on its host terminal, use it to produce the .xlsx — and
			// save it IN ITS WORKSPACE because the system prompt's <machine> convention
			// teaches deliverables go in the shell's working directory (the PRODUCT
			// mechanism; run 5 saved a perfect .xlsx into /tmp before it existed).
			prompts: []string{
				"Fammi un file Excel con il mercato di Yahoo Finance di oggi. " +
					"Voglio un .xlsx vero che si apra in un foglio di calcolo, con i dati di oggi.",
			},
			dimensions: []dimension{
				dimSkillsHardFloor, dimCapabilityGapRecognition, dimSkillOutputQuality,
			},
			skills: &skillsExpect{
				installTargetRepo: "anthropics/skills",
				installSelector:   "xlsx",
				xlsxExt:           ".xlsx",
				readBackImport:    "openpyxl",
				forbiddenWords:    []string{"skill", "install", "installa", "catalog", "catalogo"},
				judgeBudget:       judgeSkillsGate,
			},
		},
	}
}

// skillsSnippetReuseScenarios returns the 18-04 STEADY-STATE snippet-reuse scenario
// (CAP-08.1). It is SEPARATE from skillsScenarios() so TestSkillsE2E is untouched; only
// TestSnippetReuseE2E (skills_snippet_reuse_cot_eval_test.go) runs it, behind the cot_eval
// tag + OPENROUTER gate + a DB pool.
//
// The prompt is the SAME natural Italian xlsx ask as the North-Star — but the gate
// PRE-SEEDS an active xlsx-builder snippet before the run (Pitfall 5: never measure the
// first authoring run; pre-seeding is the sanctioned steady-state shaper per the plan). On
// this single REUSE run the model should discover the saved snippet via the `skill` tool
// manifest, apply it (action=use → a host shell_exec by-path frame, 18-02 D-01), run it,
// verify the .xlsx, and answer — collapsing the 21-dispatch authoring loop to ≤6
// dispatches under 40s (the grounded D-03 window).
//
// The thresholds come from docs/phase-18-xlsx-call-breakdown.md (the D-03 live breakdown),
// NOT the registration estimate: ≤6 tool dispatches (event_kind='end') + <40s wall-clock.
func skillsSnippetReuseScenarios() []scenario {
	return []scenario{
		{
			id: "skills-xlsx-snippet-reuse",
			prompts: []string{
				"Fammi un file Excel con il mercato di Yahoo Finance di oggi. " +
					"Voglio un .xlsx vero che si apra in un foglio di calcolo, con i dati di oggi.",
			},
			dimensions: []dimension{dimSkillsHardFloor, dimSkillOutputQuality},
			skills: &skillsExpect{
				xlsxExt:        ".xlsx",
				readBackImport: "openpyxl",
				// The reuse prompt is the same natural ask: it must NOT name the saved
				// snippet or say "skill"/"use" — discovery is the model's own (the manifest
				// rides the skill tool's Description; no hint in the prompt).
				forbiddenWords:          []string{"skill", "install", "installa", "catalog", "catalogo", "snippet"},
				judgeBudget:             judgeSkillsGate,
				maxSteadyStateCalls:     6,
				maxSteadyStateWallClock: 40 * time.Second,
			},
		},
	}
}
