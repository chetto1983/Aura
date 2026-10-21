package usersandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/moby/moby/client"
)

// ErrExecUnconfirmed means the caller must not assume the remote work has stopped.
var ErrExecUnconfirmed = errors.New("sandbox execution exit is unconfirmed")

var errExecStopRequested = errors.New("sandbox execution termination requested")

func waitExecExit(ctx context.Context, cli *client.Client, execID string, stop <-chan struct{}) (int, error) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return 0, errExecStopRequested
		default:
		}
		ins, err := cli.ExecInspect(ctx, execID, client.ExecInspectOptions{})
		if err != nil {
			return 0, fmt.Errorf("%w: inspect: %w", ErrExecUnconfirmed, err)
		}
		if !ins.Running {
			return ins.ExitCode, nil
		}
		select {
		case <-stop:
			return 0, errExecStopRequested
		case <-ctx.Done():
			return 0, fmt.Errorf("%w: %w", ErrExecUnconfirmed, ctx.Err())
		case <-ticker.C:
		}
	}
}

// The snapshot precedes TERM so reparenting cannot hide resistant descendants.
// Start times prevent escalation from signaling a reused PID. The wrapper is killed last,
// giving it a chance to reap its direct child; zombies are exited, not executing work.
func killJobTreeCommand(pidFile string) string {
	return strings.ReplaceAll(`
P=""
for i in 1 2 3 4 5; do
  P=$(cat '@PIDFILE@' 2>/dev/null)
  [ -n "$P" ] && break
  sleep 0.05
done
[ -n "$P" ] || exit 0
stamp() {
  L=$(cat "/proc/$1/stat" 2>/dev/null) || return 1
  L=${L##*) }
  set -- $L
  [ "$#" -ge 20 ] || return 1
  case "$1" in Z|X) return 1;; esac
  printf '%s' "${20}"
}
same() {
  pid=${1%%:*}
  expected=${1#*:}
  actual=$(stamp "$pid") || return 1
  [ "$expected" = "$actual" ]
}
T=""; F="$P"
while [ -n "$F" ]; do
  N=""
  for f in $F; do
    mark=$(stamp "$f") && T="$T $f:$mark"
  done
  for d in /proc/[0-9]*; do
    c=${d#/proc/}
    pp=$(awk '/^PPid:/{print $2; exit}' "$d/status" 2>/dev/null)
    for f in $F; do [ "$pp" = "$f" ] && N="$N $c"; done
  done
  F="$N"
done
for token in $T; do
  pid=${token%%:*}
  same "$token" && kill -TERM "$pid" 2>/dev/null
done
root=""
for token in $T; do [ "${token%%:*}" = "$P" ] && root="$token"; done
if [ -n "$root" ] && same "$root"; then kill -TERM -"$P" 2>/dev/null; fi
sleep 0.2
for token in $T; do
  pid=${token%%:*}
  [ "$pid" = "$P" ] && continue
  same "$token" && kill -KILL "$pid" 2>/dev/null
done
for i in 1 2 3 4 5 6 7 8 9 10; do
  [ -n "$root" ] && same "$root" || break
  sleep 0.05
done
if [ -n "$root" ] && same "$root"; then
  kill -KILL -"$P" 2>/dev/null
  kill -KILL "$P" 2>/dev/null
fi
for i in 1 2 3 4 5 6 7 8 9 10; do
  alive=""
  for token in $T; do same "$token" && alive=1; done
  [ -z "$alive" ] && { rm -f '@PIDFILE@'; exit 0; }
  sleep 0.05
done
exit 1
`, "@PIDFILE@", pidFile)
}

func killBoxProcessGroup(cli *client.Client, containerID, pidFile, execID string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ec, err := cli.ExecCreate(ctx, containerID, client.ExecCreateOptions{Cmd: []string{"/bin/sh", "-c", killJobTreeCommand(pidFile)}})
	if err != nil {
		return 0, fmt.Errorf("%w: create termination: %w", ErrExecUnconfirmed, err)
	}
	att, err := cli.ExecAttach(ctx, ec.ID, client.ExecAttachOptions{})
	if err != nil {
		return 0, fmt.Errorf("%w: attach termination: %w", ErrExecUnconfirmed, err)
	}
	defer att.Close()
	stop := context.AfterFunc(ctx, att.Close)
	defer stop()
	if _, err = io.Copy(io.Discard, att.Reader); err != nil {
		return 0, fmt.Errorf("%w: terminate stream: %w", ErrExecUnconfirmed, err)
	}
	code, err := waitExecExit(ctx, cli, ec.ID, nil)
	if err != nil {
		return 0, err
	}
	if code != 0 {
		return 0, fmt.Errorf("%w: resistant job tree survived termination", ErrExecUnconfirmed)
	}
	return waitExecExit(ctx, cli, execID, nil)
}
