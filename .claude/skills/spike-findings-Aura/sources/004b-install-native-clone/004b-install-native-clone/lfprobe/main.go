// lfprobe — nails the 004b lockfile-hash root cause: clone with autocrlf
// DISABLED (raw LF blobs) and recompute the skills-CLI folder hash. If it
// equals the 004a lockfile value, the CLI hashed pre-rewrite LF bytes and
// Windows autocrlf rewrote the tree afterwards.
//
// Run: go run ./.planning/spikes/004b-install-native-clone/lfprobe
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	repo     = "https://github.com/anthropics/skills"
	cliHash  = "cb7fba2177d0d35d14b3244272b25d1b05246e99b3468a087fe06c84193dbd20"
	scratch  = `D:\tmp\spike-004b-lf`
	skillRel = "skills/xlsx"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_ = os.RemoveAll(scratch)
	cmd := exec.CommandContext(ctx, "git", "-c", "core.autocrlf=false", "clone", "--depth", "1", "--single-branch", repo, scratch)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("clone failed: %v: %s\n", err, out)
		os.Exit(1)
	}
	dir := filepath.Join(scratch, filepath.FromSlash(skillRel))
	type entry struct {
		rel  string
		data []byte
	}
	var entries []entry
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		entries = append(entries, entry{strings.ReplaceAll(rel, "\\", "/"), data})
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	h := sha256.New()
	for _, e := range entries {
		h.Write([]byte(e.rel))
		h.Write(e.data)
	}
	got := hex.EncodeToString(h.Sum(nil))
	fmt.Printf("lf-clone hash = %s\nlockfile hash = %s\nmatch = %v\n", got, cliHash, got == cliHash)
	if got != cliHash {
		os.Exit(1)
	}
}
