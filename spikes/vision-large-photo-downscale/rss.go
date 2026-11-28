//go:build ignore

// rss runs a command and prints, on stderr, its peak resident set (getrusage of the waited child,
// which is the child's VmHWM), its wall time, and the container's cgroup memory.peak when the
// file exists. Built static so it runs unchanged in any of the images under test.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: rss <command> [args...]")
		os.Exit(2)
	}
	cmd := exec.Command(os.Args[1], os.Args[2:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	start := time.Now()
	err := cmd.Run()
	wall := time.Since(start)
	exit := 0
	if err != nil {
		exit = -1
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		}
	}
	var maxRSS int64
	signal := "none"
	if cmd.ProcessState != nil {
		if ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
			maxRSS = ru.Maxrss
		}
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			signal = ws.Signal().String()
		}
	}
	peak := "n/a"
	if b, err := os.ReadFile("/sys/fs/cgroup/memory.peak"); err == nil {
		peak = strings.TrimSpace(string(b))
	}
	fmt.Fprintf(os.Stderr, "RSS_KIB=%d WALL_MS=%d EXIT=%d SIGNAL=%s CGROUP_PEAK_BYTES=%s\n", maxRSS, wall.Milliseconds(), exit, signal, peak)
	os.Exit(exit)
}
