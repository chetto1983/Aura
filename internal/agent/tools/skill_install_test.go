package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// skill action=install exists because the taught alternative could not work: `npx skills
// add` writes into its working directory, which is not a loader root, so the model
// installed a skill, was told it succeeded, and then could not find it. These tests pin
// the two halves of the replacement — the source reaches the installer, and the result
// tells the model it can use the skill now.

// installCtx supplies the tool-call context NewResult requires (session, call id, run
// dir, preview cap) — a bare context makes every result-returning tool error out.
func installCtx(t *testing.T) context.Context {
	t.Helper()
	return WithToolCallContext(t.Context(), "session", "toolcall", t.TempDir(), 4096)
}

func installArgs(t *testing.T, source string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"action": "install", "source": source})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func TestSkillInstallForwardsTheSourceAndNamesTheResult(t *testing.T) {
	w := &fakeSkillWriter{installName: "xlsx"}
	tool := manageSkills(&SkillTool{Loader: &fakeSkillLoader{}, Writer: w})

	res, err := tool.Execute(installCtx(t), installArgs(t, " anthropics/skills@xlsx "))
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if w.installCalls != 1 {
		t.Fatalf("installer calls = %d, want 1", w.installCalls)
	}
	// Trimmed, but otherwise untouched: the tool does not parse or rewrite the source.
	if w.installSource != "anthropics/skills@xlsx" {
		t.Fatalf("source = %q, want the trimmed source verbatim", w.installSource)
	}
	if !strings.Contains(res.Preview, "xlsx") {
		t.Errorf("result does not name the installed skill: %q", res.Preview)
	}
	// The model must learn it can act NOW; the old flow's whole failure was a success
	// message followed by an unusable skill.
	if !strings.Contains(res.Preview, "this turn") {
		t.Errorf("result does not say the skill is usable now: %q", res.Preview)
	}
}

func TestSkillInstallRequiresASource(t *testing.T) {
	w := &fakeSkillWriter{}
	tool := manageSkills(&SkillTool{Loader: &fakeSkillLoader{}, Writer: w})

	for _, source := range []string{"", "   "} {
		_, err := tool.Execute(installCtx(t), installArgs(t, source))
		if err == nil {
			t.Fatalf("source %q: want an error", source)
		}
		if !strings.Contains(err.Error(), "source is required") {
			t.Errorf("source %q: error %q does not say what is missing", source, err)
		}
	}
	if w.installCalls != 0 {
		t.Errorf("a sourceless install still called the installer %d time(s)", w.installCalls)
	}
}

func TestSkillInstallSurfacesTheInstallerError(t *testing.T) {
	boom := errors.New("blocklisted injection sequence")
	tool := manageSkills(&SkillTool{Loader: &fakeSkillLoader{}, Writer: &fakeSkillWriter{installErr: boom}})

	_, err := tool.Execute(installCtx(t), installArgs(t, "owner/repo"))
	if err == nil {
		t.Fatal("a rejected install must be an error, not a cheerful result")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error %v does not wrap the installer's cause", err)
	}
	if !strings.Contains(err.Error(), "owner/repo") {
		t.Errorf("error %q does not name the source that failed", err)
	}
}

func TestSkillInstallWithoutAWriterIsAClearError(t *testing.T) {
	tool := manageSkills(&SkillTool{Loader: &fakeSkillLoader{}})

	_, err := tool.Execute(installCtx(t), installArgs(t, "owner/repo"))
	if err == nil {
		t.Fatal("install with no writer wired must error, never panic")
	}
	if !strings.Contains(err.Error(), "no writer") {
		t.Errorf("error %q does not explain why install is unavailable", err)
	}
}

// TestSkillSpecAdvertisesInstallAndWarnsOffTheCLI: the schema is what the model reads
// every turn. If install is absent from the enum the model falls back to the shell, which
// is the behaviour this action exists to end.
func TestSkillSpecAdvertisesInstallAndWarnsOffTheCLI(t *testing.T) {
	spec := (manageSkills(&SkillTool{Loader: &fakeSkillLoader{}})).Spec()
	var schema struct {
		Properties map[string]struct {
			Enum        []string `json:"enum"`
			Description string   `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(spec.Parameters, &schema); err != nil {
		t.Fatalf("Parameters not valid JSON: %v", err)
	}

	action, ok := schema.Properties["action"]
	if !ok {
		t.Fatal("schema has no action property")
	}
	var found bool
	for _, v := range action.Enum {
		if v == "install" {
			found = true
		}
	}
	if !found {
		t.Fatalf("install missing from the action enum: %v", action.Enum)
	}
	if !strings.Contains(action.Description, "Never install by running the skills CLI") {
		t.Error("the action description does not warn the model off the CLI install")
	}
	if _, ok := schema.Properties["source"]; !ok {
		t.Error("schema has no source property for action=install")
	}
}
