package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// ShellExec is Aura's keystone tool: a full terminal inside the caller's own per-identity
// workspace container. Pipes, redirects, chains and any installed interpreter work exactly as
// they would on a machine — but it IS that container, not the host: the host filesystem and the
// host shell are unreachable from here, and a box that cannot be resolved DENIES the call rather
// than falling back (D-09/GATE-01). For a long job, "background": true returns a shell_id read
// via shell_poll and stopped via shell_kill.
type ShellExec struct {
	// DefaultTimeout caps a call that omits timeout_ms. Zero → defaultShellTimeout.
	DefaultTimeout time.Duration

	// Background, when set, is the shared registry that holds jobs started with
	// "background": true so shell_poll/shell_kill (wired to the same registry) can
	// read and stop them across turns. Nil → background mode is unavailable.
	Background *BackgroundShells

	// Approvals is the one-shot ledger for commands matching
	// AURA_SHELL_DESTRUCTIVE_PATTERNS. Nil fails closed for configured matches.
	Approvals *ShellApprovals

	// Router is the per-identity box routing seam (SBX-01, plan 37-07) and the ONLY execution
	// path: Route returns a live box handle and Execute runs the command INSIDE the box via
	// Router.Exec. A nil Router, or one whose backend is unavailable, DENIES every call — there
	// is no host arm behind it (D-09/GATE-01).
	Router *usersandbox.SandboxRouter

	// InstallHook answers a skills-install command from the HOST install pipeline instead of
	// running it in the box, and is the ONLY thing shell_exec knows about skills — the source
	// string goes in, rendered text comes out. The composition root wires it to the same
	// Installer skill_manage uses; nil refuses the command rather than running it (see
	// shell_exec_install.go for why an install must never execute in the box).
	InstallHook func(ctx context.Context, source string) (string, error)

	// mu guards cwd: the per-session PERSISTENT working directory (Claude-Code
	// Bash-tool parity — a `cd` in one call carries into the next). Keyed by the
	// tool-call session id from WithToolCallContext ("" for bare-ctx callers). The
	// tracked dir is the box shell's final $PWD, captured via the cwd marker.
	mu  sync.Mutex
	cwd map[string]string
}

type shellExecArgs struct {
	Command    string            `json:"command"`
	Cwd        string            `json:"cwd"`
	TimeoutMs  int64             `json:"timeout_ms"`
	Env        map[string]string `json:"env"`
	Background bool              `json:"background"`
}

type shellExecFooter struct {
	ExitCode   *int   `json:"exit_code,omitempty"`
	Cwd        string `json:"cwd"`
	DurationMS int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out"`
	Cancelled  bool   `json:"cancelled,omitempty"`
}

// defaultShellTimeout is how long a call without an explicit timeout_ms BLOCKS before its
// job is handed back as a background shell. It is deliberately short.
//
// It was 120s, and at 120s it was a wall: reaching it KILLED the command and lost the work,
// so the cap had to be generous enough that almost nothing hit it — which meant an operator
// could sit two minutes in front of a spinner. Since promotion (shell_bg_promote.go) the cap
// kills nothing; it only decides when to stop blocking. Three seconds keeps the ordinary
// command inline — ls, git, grep are all well under a second — while a build stops holding
// the turn hostage.
//
// A caller that genuinely wants to wait still can: an explicit timeout_ms overrides this,
// up to AURA_SHELL_MAX_TIMEOUT_MS. Deployments tune it with AURA_SHELL_DEFAULT_TIMEOUT_MS.
const defaultShellTimeout = 3 * time.Second

// slowRunShare is the fraction of its cap a command may burn before the result starts
// telling the model about "background": true. A quarter is early enough that the advice
// lands while the job still succeeds, rather than after the cap has already killed it.
const slowRunShare = 0.25

// backgroundAdvice is the one sentence both the slow-run notice and the timeout marker
// end with. It names the parameter, what it returns, and who tells the model when the
// job is done — the three things missing from a bare "[command timed out]".
const backgroundAdvice = `set "background": true — it returns a shell_id immediately instead of blocking, Aura notifies this conversation when the job exits, and shell_poll then reads the retained output`

// slowRunNotice returns the advice line for a command that finished but consumed a large
// share of its cap, and "" for one that did not. A zero or negative cap yields nothing:
// with no ceiling there is no share to exceed and no wall to warn about.
//
// It exists because the advice was reaching the model in the wrong place. Measured
// 2026-09-09 over every recorded turn: 28 tool-call turns, zero uses of background,
// while an artifact bundle spent 50.3s of a 120s cap — already 42% of the way to a
// failure that would have discarded the whole build.
func slowRunNotice(took, cap time.Duration) string {
	if cap <= 0 || took < time.Duration(float64(cap)*slowRunShare) {
		return ""
	}
	return fmt.Sprintf("[took %.1fs of the %.0fs cap — for a job this long %s]",
		took.Seconds(), cap.Seconds(), backgroundAdvice)
}

func (s *ShellExec) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "The shell command line to run, e.g. \"ls -la\", \"python3 script.py\", \"git status\". Runs through a POSIX shell inside your workspace container, so pipes, redirects, and && chains all work. For long scripts, create the file with fs_write first, then run it here."},
    "cwd": {"type": "string", "description": "Optional working directory override, as an absolute path inside your workspace container (e.g. \"/workspace/project\"). Your working directory PERSISTS between calls (a cd carries over) and starts at /workspace."},
    "timeout_ms": {"type": "integer", "minimum": 0, "description": "Optional: how long to BLOCK waiting for this command, in milliseconds. Omit for the default (a few seconds). This is not a kill deadline — a command still running when it elapses is handed back as a background job, never killed. Raise it only when you want to sit and wait for the result inline."},
    "env": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Optional extra environment variables for this command only."},
    "background": {"type": "boolean", "description": "Return a shell_id IMMEDIATELY instead of blocking at all. You rarely need it: a command that outlives the blocking window is handed back as a background job on its own, so set this only when you already know the job is long (a build, a download, a dev server) and want to keep working without waiting even those first seconds. Either way Aura notifies this conversation when the job exits, shell_poll reads its output, and shell_kill stops it."}
  },
  "required": ["command"]
}`)
	return Spec{
		Name:    "shell_exec",
		Summary: "Run a shell command in your workspace container — a full terminal.",
		Description: "Run a command line through a POSIX shell inside YOUR workspace container — use it for builds, scripts, and glue work that dedicated tools do not cover. " +
			"Every path you name is a path in that container, which is where fs_read/fs_write and document_open also work; the machine hosting Aura is not reachable from here. " +
			"Do NOT reach for it when a dedicated tool fits: to read, search, or write files use fs_read / fs_grep / fs_glob and fs_write / fs_edit (they return structured results and page large files instead of flooding context); to get current web facts like a price, the weather, or today's news use the dedicated web search/fetch tools (load them with tool_search if they are not in your list). Reaching for the shell because the specific tool is not visible is the most common mistake. " +
			"Pipes, redirects, && chains, any installed interpreter (python, node), git, and filesystem work all just work. " +
			"Your working directory persists between calls (a cd carries over) and starts at /workspace. " +
			"Returns combined stdout and stderr plus a final [aura_shell {...}] JSON footer with exit_code, cwd, duration_ms, and timed_out; rely on that footer instead of spending separate pwd or exit-code calls. " +
			"You do NOT have to predict how long a command will take. One still running after a few seconds is handed back AUTOMATICALLY as a background job carrying a shell_id — it is never killed — and Aura notifies this conversation when it exits; shell_poll then reads its output and shell_kill stops it. Set \"background\": true up front when you already know the job is long and want the id straight away, or raise \"timeout_ms\" when you would rather block until it finishes.\n\n" +
			// These four rules used to live in the system prompt, where they were read
			// thousands of tokens before the decision they govern. They belong with the
			// schema: the model reads them exactly when it is about to run a command.
			"Working rules:\n" +
			"- One call is one shell transaction. When steps are sequential, put discovery, execution and verification in the SAME command and print one compact final status, JSON preferred.\n" +
			// Measured 2026-08-03: asked for one row out of a spreadsheet, she printed
			// the whole sheet (prompt 8.171 -> 9.596 tokens) and then spent a SECOND
			// call searching the dump she had just made (13.626). The script is where
			// the filtering belongs; stdout is for the answer.
			"- QUERY data, never dump it. When the answer is a row, a count or an aggregate, do the filtering INSIDE the script — pandas, awk, jq, sqlite — and print only the result. Printing a whole sheet, table or log into the conversation so you can search it in the next call costs two calls and a context window to do one call's work.\n" +
			"- Never author file content here. Heredocs and quoted echo/printf blobs break on quoting — write the file with fs_write, then run it.\n" +
			"- Pick ONE interpreter per task and install into it: `python3 -m pip install ...`, never bare `pip`, so the install lands on the interpreter you run. An import that fails right after installing means pip used a different one; fix it with `python3 -m pip`, do not alternate between python and python3.\n" +
			"- Treat code you generated as untrusted: read it before running it, and prefer a scratch directory you can clean up.",
		Parameters: params,
		// NOT deferred: this is the most-used tool in the system, and hiding it bought
		// nothing. Measured 2026-08-03 across live turns, the manifest held four tools
		// — ask_user, read_tool_output, text_response, tool_search — and not one of
		// them does any work, so every substantive turn opened with a search for how
		// to do things. The schema costs tokens once per turn against a search round
		// trip that costs a whole model call, every conversation.
		//
		// Anthropic's own guidance for the pattern says to keep the three to five most
		// frequently used tools loaded so the model can call them without searching
		// first. Having zero is what made her wander.
		Deferred: false,
		// Conservatively Mutating (D-43): a command line can write files or mutate
		// state and the agent cannot tell `ls` from `python build.py` statically.
		Mutating:       true,
		OperationScope: OperationScopeAgent, OperationNormalizer: OperationNormalizerCanonical,
		ReplayPolicy: ReplayToolResult,
	}
}

func (s *ShellExec) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a shellExecArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		// Truncated args are almost always the output-token budget cutting a giant
		// command mid-JSON (observed live: a one-shot python script carrying all its
		// data). The hint steers the model to the incremental pattern instead of
		// retrying the same oversized call (D-15 self-correction).
		return ToolResult{}, fmt.Errorf("shell_exec args: %w — your arguments were likely truncated by the output budget; "+
			"put large or multi-line content in files with fs_write/fs_edit, then run the file here", err)
	}
	if strings.TrimSpace(a.Command) == "" {
		return ToolResult{}, fmt.Errorf("shell_exec: command is required")
	}

	// Models occasionally emit CRLF line endings inside command; under the POSIX
	// shell a stray \r corrupts heredoc terminators and the cwd-tracking wrap
	// (live run 9, amendment #53 / D-42). Normalize once and use the same command
	// for destructive matching, approval digesting, and execution.
	commandForGate := strings.ReplaceAll(a.Command, "\r\n", "\n")

	// A skills install is answered by the HOST pipeline and never reaches the box: run in the
	// box it succeeds into a directory no loader reads (shell_exec_install.go). This precedes
	// Route because the box is irrelevant to it — an install must not fail merely because the
	// sandbox is down, and must not run merely because it is up.
	if res, handled, err := s.maybeInstallResult(ctx, commandForGate); handled {
		return res, err
	}

	// Every call runs in the caller's per-identity box. A box that cannot be resolved DENIES;
	// there is no host arm left to fall back to (D-09/GATE-01).
	boxHandle, routeErr := s.Router.Route(ctx)
	if routeErr != nil {
		return sandboxUnavailableResult("shell_exec", routeErr), nil
	}

	// The approval digest is bound to the directory the command ACTUALLY runs in — the box cwd.
	// While a host arm existed this read a host resolver, so an operator approving `rm -rf …`
	// approved it for a directory the command never entered.
	workdir := s.boxWorkdir(ctx, a.Cwd)
	approvalRequired, err := s.requireShellApproval(ctx, commandForGate, workdir)
	if err != nil {
		return ToolResult{}, err
	}
	if approvalRequired != nil {
		return *approvalRequired, nil
	}

	if a.Background {
		if s.Background == nil {
			return ToolResult{}, fmt.Errorf("shell_exec: background mode is not available in this context")
		}
		// The background job runs INSIDE the box via a streamed box exec (37-09), mirroring
		// executeInBox's dir/env. A box start failure denies fail-CLOSED (D-09/GATE-01); no host
		// process is reachable from here.
		id, err := s.Background.startBox(ctx, boxHandle, commandForGate, workdir, boxEnv(a.Env))
		if err != nil {
			if dirErr := s.rejectMissingBoxDir(ctx, boxHandle, workdir, a.Cwd); dirErr != nil {
				return ToolResult{}, dirErr
			}
			return sandboxUnavailableResult("shell_exec", err), nil
		}
		rendered := fmt.Sprintf("Started in the background as %s. Aura will notify this conversation when it exits; then read its final output with shell_poll (shell_id=%q). Stop it with shell_kill.\n[aura_shell_bg {\"shell_id\":%q,\"status\":\"running\"}]", id, id, id)
		res, err := NewResult(ctx, rendered)
		if err != nil {
			return ToolResult{}, err
		}
		res.Meta = &ToolResultMeta{"shell_id": id, "background": true}
		return res, nil
	}

	return s.executeInBox(ctx, boxHandle, commandForGate, a.Cwd, a.Env, a.TimeoutMs)
}

// capShellOutput bounds ONE box stream to AURA_SHELL_OUTPUT_CAP, reporting how much it dropped.
// It keeps the TAIL, which is both what a reader wants and what the cwd marker rides on. The box
// backend already accumulated the whole stream in memory before we see it, so this bounds what
// reaches the MODEL, not what the daemon buffered.
func capShellOutput(p []byte) string {
	b := boundedOutputBuffer{capBytes: shellOutputBufCap()}
	b.Write(p)
	return b.String()
}

type boundedOutputBuffer struct {
	buf      []byte
	capBytes int
	dropped  int64
}

func (b *boundedOutputBuffer) Write(p []byte) {
	if len(p) == 0 {
		return
	}
	if b.capBytes <= 0 {
		b.capBytes = defaultShellOutputCap
	}
	if len(p) >= b.capBytes {
		b.dropped += int64(len(b.buf) + len(p) - b.capBytes)
		b.buf = append(b.buf[:0], p[len(p)-b.capBytes:]...)
		return
	}
	overflow := len(b.buf) + len(p) - b.capBytes
	if overflow > 0 {
		b.dropped += int64(overflow)
		copy(b.buf, b.buf[overflow:])
		b.buf = b.buf[:len(b.buf)-overflow]
	}
	b.buf = append(b.buf, p...)
}

func (b *boundedOutputBuffer) String() string {
	if b.dropped <= 0 {
		return string(b.buf)
	}
	return fmt.Sprintf("[output truncated: dropped %d byte(s); showing last %d byte(s)]\n%s",
		b.dropped, len(b.buf), string(b.buf))
}

// renderShellBody renders the command output, substituting the explicit "[no output]" notice only
// when there is neither output NOR a status line to explain the silence.
func renderShellBody(output, status string) string {
	if strings.TrimSpace(output) == "" && status == "" {
		return "[no output]"
	}
	return output
}

const shellStderrTailCap = 800

// renderShellFooter builds the trailing block the model reads instead of spending a separate pwd
// or exit-code call: the status line, the [aura_shell {...}] JSON, and — only when body+footer
// would overflow the per-call cap and page into the sidecar — a reserved tail of stderr, so a
// failure's cause is never the part that gets paged away.
func renderShellFooter(ctx context.Context, body, stderr, status string, footer shellExecFooter) string {
	reserved := appendShellFooter(status, footer)
	if shouldReserveStderrTail(ctx, body, reserved, stderr) {
		reserved = appendShellFooter(joinFooterSections(status, stderrTailBlock(stderr)), footer)
	}
	return reserved
}

func shouldReserveStderrTail(ctx context.Context, body, reserved, stderr string) bool {
	if strings.TrimSpace(stderr) == "" {
		return false
	}
	tc, ok := toolCallCtx(ctx)
	if !ok {
		return false
	}
	return len(body)+len(reserved) > tc.cap
}

func stderrTailBlock(stderr string) string {
	tail := strings.TrimRight(truncateTailBytes(stderr, shellStderrTailCap), "\n")
	if tail == "" {
		return ""
	}
	return "[stderr tail]\n" + tail
}

func joinFooterSections(parts ...string) string {
	var b strings.Builder
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(part)
	}
	return b.String()
}

func truncateTailBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

func appendShellFooter(output string, footer shellExecFooter) string {
	raw, err := json.Marshal(footer)
	if err != nil {
		return output
	}
	var b strings.Builder
	b.WriteString(output)
	ensureTrailingNewline(&b)
	b.WriteString("[aura_shell ")
	b.Write(raw)
	b.WriteByte(']')
	return b.String()
}

func ensureTrailingNewline(b *strings.Builder) {
	if s := b.String(); s != "" && !strings.HasSuffix(s, "\n") {
		b.WriteByte('\n')
	}
}
