package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chetto1983/aura/internal/scoring"
)

// Sub-slice 7e executable snippets (D-04/D-16/D-19/D-20). A snippet is a skill with
// type:snippet whose body IS executable code in one of the three supported languages.
// It is SAVED through the same write-and-materialize path as an instruction skill
// (amendment #97 — the save IS the effect) and is EXECUTED BY PATH
// (python3 <dir>/<name>.py — NEVER the exec bit, spike 005).
// There is NO bespoke run code (D-04): action=use returns instructions + the path;
// the model calls shell_exec, which a strict profile routes into the per-identity box
// without the model choosing or seeing it (amendment #96). The CLI
// `aura skills snippet exec` is the deterministic operator path that stamps usage.

// SnippetLanguage is the snippet code language enum (D-20). All three interpreters are
// present in both execution environments (python3 / sh / node) — the aura image and the
// per-identity box — so a snippet in any of them runs by-path under either profile.
type SnippetLanguage string

// The three allowed snippet languages.
const (
	LangPython SnippetLanguage = "python"
	LangShell  SnippetLanguage = "shell"
	LangJS     SnippetLanguage = "js"
)

// snippetMeta maps a language to its file extension and the interpreter the model/CLI
// prefixes to the path for a by-path invocation. interpreter+path are the structured
// argv (NEVER a shell string, NEVER the exec bit, spike 005).
type snippetMeta struct {
	ext         string
	interpreter string
}

var snippetMetaByLang = map[SnippetLanguage]snippetMeta{
	LangPython: {ext: "py", interpreter: "python3"},
	LangShell:  {ext: "sh", interpreter: "sh"},
	LangJS:     {ext: "js", interpreter: "node"},
}

// inSandboxSkillsRoot is the in-container mount point of the ro /skills bind mount
// (D-17, spike 005). action=use hands the model a path under this root.
const inSandboxSkillsRoot = "/skills"

// validSnippetLanguage normalizes + validates the language enum, returning the meta
// and a structured error for anything outside {python,shell,js} (D-20). It tolerates
// a few common aliases (py/python3, bash, javascript/node) folding them to the canon.
func validSnippetLanguage(language string) (SnippetLanguage, snippetMeta, error) {
	canon := SnippetLanguage(strings.ToLower(strings.TrimSpace(language)))
	switch canon {
	case "py", "python3":
		canon = LangPython
	case "bash":
		canon = LangShell
	case "javascript", "node":
		canon = LangJS
	}
	meta, ok := snippetMetaByLang[canon]
	if !ok {
		return "", snippetMeta{}, fmt.Errorf("%w: snippet language %q must be one of python, shell, js",
			ErrInvalidStructure, language)
	}
	return canon, meta, nil
}

// SnippetCodeFile is the deterministic code-file name a snippet materializes to
// inside its skill dir: <name>.<ext>. The sandbox sees it at
// /skills/<name>/<name>.<ext> and execs it by path.
func SnippetCodeFile(name string, lang SnippetLanguage) (string, error) {
	meta, ok := snippetMetaByLang[lang]
	if !ok {
		return "", fmt.Errorf("%w: unknown snippet language %q", ErrInvalidStructure, lang)
	}
	return name + "." + meta.ext, nil
}

// SnippetSandboxPath returns the stable in-box path action=use hands out for a snippet
// under a strict profile: /skills/<name>/<name>.<ext>. It is the by-path target of the
// shell_exec call the SandboxRouter routes into the per-identity box (amendment #96).
func SnippetSandboxPath(name string, lang SnippetLanguage) (string, error) {
	file, err := SnippetCodeFile(name, lang)
	if err != nil {
		return "", err
	}
	return inSandboxSkillsRoot + "/" + name + "/" + file, nil
}

// SnippetHostPath returns the HOST by-path target action=use hands out under D-01
// (host-primary): exportDir/<name>/<name>.<ext>. It mirrors SnippetSandboxPath EXACTLY
// (same snippetMetaByLang ext map, same structured ErrInvalidStructure for an unknown
// language) but roots the path at AURA_SKILL_EXPORT_DIR via filepath.Join so the path
// is OS-correct — it is the file the model runs through shell_exec on the host, not the
// in-box /skills mount (which SnippetSandboxPath serves when a strict profile routes the
// run into the box).
func SnippetHostPath(name string, lang SnippetLanguage, exportDir string) (string, error) {
	file, err := SnippetCodeFile(name, lang)
	if err != nil {
		return "", err
	}
	return filepath.Join(exportDir, name, file), nil
}

// SnippetInvocation resolves a snippet's by-path invocation from a (possibly aliased)
// language string: the in-box path AND the interpreter the caller prefixes to it (D-04).
// It is the CLI entry point (it accepts the raw frontmatter language and validates the
// enum). An unknown language is a structured error.
func SnippetInvocation(name, language string) (sandboxPath, interpreter string, err error) {
	lang, meta, lerr := validSnippetLanguage(language)
	if lerr != nil {
		return "", "", lerr
	}
	path, perr := SnippetSandboxPath(name, lang)
	if perr != nil {
		return "", "", perr
	}
	return path, meta.interpreter, nil
}

// SnippetHostInvocation resolves a snippet's HOST by-path invocation from a (possibly
// aliased) language string (D-01 host-primary): the host export-dir path the model runs
// through shell_exec AND the interpreter. It is the loader-adapter entry point — it
// validates the enum once and roots the path at exportDir (AURA_SKILL_EXPORT_DIR), the
// same dir the Writer materializes into. An unknown language is a structured error.
func SnippetHostInvocation(name, language, exportDir string) (hostPath, interpreter string, err error) {
	lang, meta, lerr := validSnippetLanguage(language)
	if lerr != nil {
		return "", "", lerr
	}
	path, perr := SnippetHostPath(name, lang, exportDir)
	if perr != nil {
		return "", "", perr
	}
	return path, meta.interpreter, nil
}

// SnippetSaveResult is what SaveSnippet returns: the resulting status, the computed
// risk tier, and whether needs_network was surfaced at the RISKY tier (D-20/D-37).
// The tier and the needs_* flags stopped branching with amendment #97 — they are
// reported, not enforced. The caller (CLI / tool adapter) frames them for the
// operator/model.
type SnippetSaveResult struct {
	Status         string
	Tier           scoring.RiskTier
	Language       SnippetLanguage
	NeedsNetwork   bool // surfaced in the SAVE gate (D-20/D-37); the proxy is Phase-8
	NeedsWorkspace bool
}

// SaveSnippet writes a snippet (a type:snippet skill) and makes it usable in the same
// call: it validates the language enum + the write boundary, computes the RISKY tier,
// surfaces needs_network/needs_workspace, and lands BOTH the SKILL.md (frontmatter +
// docs body) and the <name>.<ext> code file active + materialized + audited.
//
// Materializing here is what closes the loop the model actually needs: UseSnippet
// hands back a path under exportDir, so a snippet that was written but not exported is
// one the model is told to run and cannot. The supplied body is the executable code;
// the frontmatter's optional inputs_schema/outputs_desc/deps/tags ride through for
// use's docs rendering (D-20, validator-tolerated).
func (w *Writer) SaveSnippet(ctx context.Context, name, language, code string, fm Frontmatter, actor AuditActor) (SnippetSaveResult, error) {
	lang, meta, err := validSnippetLanguage(language)
	if err != nil {
		return SnippetSaveResult{}, fmt.Errorf("save snippet %q: %w", name, err)
	}
	fm.Name = name
	fm.Type = TypeSnippet
	fm.Language = string(lang)

	// The SKILL.md body for a snippet is a docs frame (rendered by use), NOT the code
	// — the code lives in the <name>.<ext> file. The write-boundary validator runs on
	// the CODE (the executable surface a blocklist must gate, T-11-03-E1), with the
	// snippet's own body cap.
	if verr := ValidateForWrite(fm, code, w.blocklist, w.bodyCapBytes, false); verr != nil {
		return SnippetSaveResult{}, fmt.Errorf("save snippet %q: %w", name, verr)
	}

	tier := scoring.ComputeSkillTier(scoring.SkillCreate, code)
	docsBody := renderSnippetDocs(fm, meta)

	files := map[string][]byte{
		"SKILL.md":            skillFileBytes(fm, docsBody),
		name + "." + meta.ext: []byte(code),
	}
	audit := actorAudit(name, AuditCreate, HashSkillFiles(files), actor)
	if werr := w.writeActive(ctx, name, writeFilesInto(files), audit); werr != nil {
		return SnippetSaveResult{}, fmt.Errorf("save snippet %q: %w", name, werr)
	}

	return SnippetSaveResult{
		Status:         StatusActive,
		Tier:           tier,
		Language:       lang,
		NeedsNetwork:   fm.NeedsNetwork,
		NeedsWorkspace: fm.NeedsWorkspace,
	}, nil
}

// renderSnippetDocs builds the SKILL.md body a snippet carries: a human/model-facing
// doc frame describing the language, the by-path invocation, and the optional
// inputs_schema/outputs_desc/deps/tags (D-20, docs-only). It is NOT the code — the
// code lives in the sibling <name>.<ext> file. use renders this verbatim. The frame
// stays GENERIC (no baked execution-tier path, D-01/RESEARCH option b): the concrete
// host vs sandbox invocation is computed at use-time by the tool layer, so an
// already-materialized snippet needs no re-render when the posture changes.
func renderSnippetDocs(fm Frontmatter, meta snippetMeta) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Executable snippet (%s). Run the %s interpreter against the snippet file by path (the exact invocation is provided when you use it).\n\n", fm.Language, meta.interpreter)
	if fm.OutputsDesc != "" {
		fmt.Fprintf(&b, "Outputs: %s\n", fm.OutputsDesc)
	}
	if len(fm.Deps) > 0 {
		fmt.Fprintf(&b, "Deps: %s\n", strings.Join(fm.Deps, ", "))
	}
	if len(fm.Tags) > 0 {
		fmt.Fprintf(&b, "Tags: %s\n", strings.Join(fm.Tags, ", "))
	}
	if fm.NeedsNetwork {
		b.WriteString("needs_network: true (egress is gated at the RISKY tier; the enforced proxy is Phase-8)\n")
	}
	if fm.NeedsWorkspace {
		b.WriteString("needs_workspace: true (writes under the sandbox workspace)\n")
	}
	return b.String()
}

// SnippetUse is what UseSnippet returns: the docs-rendered instructions the model
// reads + the by-path targets + interpreter so the model assembles the exact run call.
// HostPath is the D-01 host-primary target; SandboxPath is the in-box /skills mount.
// Both ride because the tool layer picks between them by profile — the model runs the
// one it is handed, through shell_exec, either way (amendment #96).
type SnippetUse struct {
	Instructions string
	HostPath     string
	SandboxPath  string
	Interpreter  string
	Language     SnippetLanguage
}

// UseSnippet resolves an ACTIVE snippet for the model (D-01/D-04): it returns the docs
// body + the HOST export-dir by-path target (the model runs it via shell_exec) + the
// in-box path (handed out instead under a strict profile) + the interpreter. The
// host path is derived fresh at use-time from w.exportDir (RESEARCH option b) so an
// already-materialized snippet needs no re-materialization. SanitizeName runs FIRST
// (T-18-05-T) so a crafted name cannot traverse out of the export dir. It reads the
// active SKILL.md frontmatter to learn the language; a non-snippet or unknown skill is
// a structured error. It does NOT execute anything (no bespoke run, D-04).
func (w *Writer) UseSnippet(name string) (SnippetUse, error) {
	if err := SanitizeName(name, name); err != nil {
		return SnippetUse{}, fmt.Errorf("use snippet %q: %w", name, err)
	}
	activeDir := filepath.Join(w.activeDir, name)
	raw, err := os.ReadFile(filepath.Join(activeDir, "SKILL.md")) // #nosec G304 -- activeDir = activeRoot/<SanitizeName-validated name>
	if err != nil {
		return SnippetUse{}, fmt.Errorf("use snippet %q: read active SKILL.md: %w", name, err)
	}
	fm, body, perr := parseFrontmatter(raw)
	if perr != nil {
		return SnippetUse{}, fmt.Errorf("use snippet %q: parse: %w", name, perr)
	}
	if fm.Type != TypeSnippet {
		return SnippetUse{}, fmt.Errorf("use snippet %q: skill is type %q, not a snippet", name, fm.Type)
	}
	lang, meta, lerr := validSnippetLanguage(fm.Language)
	if lerr != nil {
		return SnippetUse{}, fmt.Errorf("use snippet %q: %w", name, lerr)
	}
	sandboxPath, _ := SnippetSandboxPath(name, lang)
	hostPath, _ := SnippetHostPath(name, lang, w.exportDir)
	return SnippetUse{
		Instructions: body,
		HostPath:     hostPath,
		SandboxPath:  sandboxPath,
		Interpreter:  meta.interpreter,
		Language:     lang,
	}, nil
}
