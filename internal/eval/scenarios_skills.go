//go:build cot_eval

// scenarios_skills.go is the Phase 11 (CAP-07 / CAP-08 / D-35, RISCRITTO by
// amendment #51 / D-40, host surface #52 / D-41) live xlsx North-Star dataset — SEPARATE
// from scenarios() and swarmScenarios() so neither TestCoTEval nor TestSwarmE2E runs it;
// only TestSkillsE2E (skills_cot_eval_test.go) does, behind the same cot_eval build tag +
// OPENROUTER gate. The single scenario is the product North Star (11-CONTEXT.md
// §specifics): a NATURAL Italian prompt — NO "skill"/"install" word, asserted absent —
// that the model must turn into the full autonomous self-extension loop on its own:
//
//	recognize the capability gap → discover anthropics/skills/xlsx on skills.sh
//	(`npx skills find xlsx` on the HOST terminal, taught by the always-on
//	find-skills-aura skill) → self-install (`npx skills add anthropics/skills --skill
//	xlsx --copy -y` from the skills home) → use the skill (bundled Python by-path on the
//	host) → pull today's data via the shipped web tools → produce the .xlsx.
//
// The dual gate (D-35, RISCRITTO #51/D-40, host surface #52/D-41), enforced by
// skills_cot_eval_test.go:
//   - HARD FLOOR (artifact-not-reply ground truth, feedback_probe_must_verify_artifact_not_reply):
//     (a) SELF-INSTALL evidence from STRUCTURED tool args — a shell_exec command line
//     ran `npx skills add` targeting `anthropics/skills` with the `xlsx` selector
//     (read from resp.ToolCalls[].Function.Arguments, never the prose); (b) the produced
//     .xlsx EXISTS in the HOST run workspace (fresh — newer than run start), OPENS
//     (re-read via a host openpyxl read-back), and CONTAINS today's date — never an
//     assertion on r.Reply.
//   - JUDGE ≥90% equal-weight mean over: autonomous capability-gap recognition + output
//     quality (D-35). The install-prudence dimension is DROPPED (#51/D-40 — no
//     install-approval ceremony; the model self-installs on its host terminal).
package eval

// skillsExpect declares the deterministic ground-truth signals the xlsx North-Star
// hard floor asserts (D-35, RISCRITTO #51/D-40). It is pure data — the harness reads
// it, drives the find→add→use→produce loop, and gates on it; no logic lives here.
type skillsExpect struct {
	// installTargetRepo is the source repo the self-install command line must target
	// (the xlsx North-Star provenance, asserted from the structured `npx skills add`
	// arguments — not the prose). Reputable source per find-skills-aura's "choose"
	// guidance (anthropics/vercel-labs/microsoft).
	installTargetRepo string
	// installSelector is the `--skill` selector the self-install must carry (the xlsx
	// North-Star target). The action-aware capture flags it from the command line.
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
			// NATURAL prompt — NO "skill"/"installa"/"install"/"catalogo". The user just
			// wants today's Yahoo Finance market as a real Excel file; the model must
			// recognise it lacks a battle-tested xlsx method, discover anthropics/skills/
			// xlsx on skills.sh (`npx skills find xlsx`), self-install it on its host
			// terminal, then use it to produce the .xlsx.
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
