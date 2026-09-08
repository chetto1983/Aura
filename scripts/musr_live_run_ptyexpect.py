#!/usr/bin/env python3
"""Drives an interactive CLI over a real pseudo-terminal, feeding fixed input lines when
specific prompt substrings appear. No third-party dependency (stdlib pty/select/os only) —
this is what makes `aura identity create`'s TTY-only secret prompts (readHiddenFromStdin ->
golang.org/x/term.ReadPassword, cmd/aura/recover_operator.go:130-138) automatable without
relaxing T-01-03 secret discipline: the password/security-answer never touch argv, env, or a
log — they are typed into a real PTY, the same interface a human would use, only orchestrated
here instead of by hand.

Used by scripts/musr_live_run.sh (Phase 01 Plan 06) to provision identity B headlessly.

Env vars (paths only — never the secret bytes themselves, see the discipline note above):
  PTYRUN_OUTPUT     path to mirror the child's combined stdout+stderr into (for FAIL diagnostics)
  PTYRUN_EXCHANGES_FILE
                    path to a 0600 file, deleted by the caller immediately after this process
                    exits, holding "prompt1\\x1esend1\\x1eprompt2\\x1esend2..." (\\x1e = ASCII
                    RS, chosen because it cannot appear in a CLI prompt string or a generated
                    secret). A file, not an env var: an env var is readable by the same UID via
                    /proc/<pid>/environ for the process's whole lifetime; a file this process
                    opens once and never re-reads has a strictly smaller exposure window.

Usage: musr_live_run_ptyexpect.py <argv0> [args...]
Exit code mirrors the child process's exit code (127 if exec itself fails).
"""
import os
import pty
import select
import sys
import time

PROMPT_TIMEOUT_SEC = 60


def main() -> None:
    argv = sys.argv[1:]
    if not argv:
        sys.stderr.write("musr_live_run_ptyexpect: usage: <argv0> [args...]\n")
        sys.exit(2)

    out_path = os.environ["PTYRUN_OUTPUT"]
    with open(os.environ["PTYRUN_EXCHANGES_FILE"], "r", encoding="utf-8") as ef:
        raw = ef.read().split("\x1e")
    exchanges = [
        (raw[i].encode(), (raw[i + 1] + "\n").encode()) for i in range(0, len(raw), 2)
    ]

    pid, fd = pty.fork()
    if pid == 0:
        os.execvp(argv[0], argv)
        os._exit(127)

    buf = b""
    idx = 0
    deadline = time.time() + PROMPT_TIMEOUT_SEC
    status = 0
    with open(out_path, "wb") as outf:
        while True:
            if time.time() > deadline:
                sys.stderr.write("musr_live_run_ptyexpect: timeout waiting for prompts/exit\n")
                break
            ready, _, _ = select.select([fd], [], [], 1.0)
            if fd in ready:
                try:
                    data = os.read(fd, 4096)
                except OSError:
                    data = b""
                if not data:
                    break
                buf += data
                outf.write(data)
                outf.flush()
                while idx < len(exchanges) and exchanges[idx][0] in buf:
                    os.write(fd, exchanges[idx][1])
                    buf = b""
                    idx += 1
            else:
                done_pid, status = os.waitpid(pid, os.WNOHANG)
                if done_pid == pid:
                    break
        try:
            _, status = os.waitpid(pid, 0)
        except ChildProcessError:
            pass

    code = os.WEXITSTATUS(status) if os.WIFEXITED(status) else 1
    sys.exit(code)


if __name__ == "__main__":
    main()
