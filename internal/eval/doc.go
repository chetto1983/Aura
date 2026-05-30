// Package eval hosts Aura's LIVE, operator-authorized evaluation harnesses. The
// only harness today is the chain-of-thought / tool-use eval (files tagged
// `//go:build cot_eval`), a MANUAL paid gate that drives the real LlmAgent over the
// real openai_compat client against the configured model and scores every AI-SPEC
// eval dimension plus a CoT-reasoning + guardrail extension. It is NEVER part of CI
// or the Makefile quality targets — like scripts/llm_smoke.sh it is gated on
// OPENROUTER_API_KEY and run by a human.
//
// This doc.go carries NO build tag so the package is valid under the default build
// (so `go test ./...` does not report "build constraints exclude all Go files"); it
// holds no runnable code. Run the harness with:
//
//	set -a; . ./.env; set +a
//	export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
//	go test -tags cot_eval -run TestCoTEval -timeout 600s -v ./internal/eval/
package eval
