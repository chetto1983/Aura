// The detection RULES, over literal snapshots.
//
// They are the part of the detector that is portable Hermes logic, so they are tested
// without a box, a shell or a filesystem: a snapshot is exactly the set of answers the
// rules ask for, and writing one by hand is the whole point of the split. Fetching a
// snapshot from the real box is coding_context_box_test.go's job.
package agent

import (
	"maps"
	"path"
	"slices"
	"strings"
	"testing"
)

// snapshotOf builds a snapshot from absolute paths to content. A path with empty content
// is an existence-only probe, exactly as the box probe reports one.
func snapshotOf(files map[string]string, gitDirs ...string) projectSnapshot {
	snap := projectSnapshot{
		files:   make(map[string]string, len(files)),
		gitDirs: make(map[string]bool, len(gitDirs)),
		tempDir: "/tmp",
		homeDir: "/root",
	}
	maps.Copy(snap.files, files)
	for _, dir := range gitDirs {
		snap.gitDirs[dir] = true
	}
	return snap
}

func TestAncestorsWalkUpToTheRootAndStop(t *testing.T) {
	got := ancestors("/workspace/api/internal")
	want := []string{"/workspace/api/internal", "/workspace/api", "/workspace", "/"}
	if !slices.Equal(got, want) {
		t.Fatalf("ancestors = %v, want %v", got, want)
	}
	// A pathological cwd must not make the probe unbounded: the chain is what the one
	// exec has to enumerate, so its length is a cost, not just a loop counter.
	deep := ancestors("/" + strings.Repeat("a/", 100))
	if len(deep) != maxAncestorProbeDepth {
		t.Fatalf("deep chain = %d entries, want the %d cap", len(deep), maxAncestorProbeDepth)
	}
}

func TestProjectFactsPrefersTheGitRootOverAMarker(t *testing.T) {
	// A repository with a vendored manifest deeper than its own root: .git wins, and it
	// wins from ANY depth, where the marker walk stops at markerRootMaxDepth.
	snap := snapshotOf(map[string]string{
		"/workspace/api/sub/package.json": `{"scripts":{"test":"vitest"}}`,
		"/workspace/api/go.mod":           "",
	}, "/workspace/api")

	facts := projectFactsFrom(snap, "/workspace/api/sub")
	if !facts.Found || facts.Root != "/workspace/api" {
		t.Fatalf("facts = %+v, want the git root /workspace/api", facts)
	}
}

func TestProjectFactsFallsBackToTheNearestMarker(t *testing.T) {
	snap := snapshotOf(map[string]string{"/workspace/api/go.mod": ""})

	facts := projectFactsFrom(snap, "/workspace/api/internal/store")
	if !facts.Found || facts.Root != "/workspace/api" {
		t.Fatalf("facts = %+v, want the nearest marker root", facts)
	}
}

func TestProjectFactsIgnoresManifestsInSharedRoots(t *testing.T) {
	// A stray Makefile left in /tmp or $HOME by any process must not turn every workspace
	// under it into a project -- the original's reason for skipping both.
	for _, shared := range []string{"/tmp", "/root"} {
		t.Run(shared, func(t *testing.T) {
			snap := snapshotOf(map[string]string{path.Join(shared, "Makefile"): "test:\n\tgo test\n"})
			if facts := projectFactsFrom(snap, shared+"/scratch"); facts.Found {
				t.Fatalf("facts = %+v, want no project: %s is shared, not a workspace", facts, shared)
			}
		})
	}
}

func TestProjectFactsAreEmptyOutsideAProject(t *testing.T) {
	empty := snapshotOf(nil)
	for name, cwd := range map[string]string{
		"no marker anywhere": "/workspace/scratch",
		"empty cwd":          "",
		"blank cwd":          "   ",
	} {
		t.Run(name, func(t *testing.T) {
			if facts := projectFactsFrom(empty, cwd); facts.Found {
				t.Fatalf("facts = %+v, want Found=false", facts)
			}
		})
	}
}

func TestDetectVerifyCommandsOrderAndPackageManager(t *testing.T) {
	const root = "/workspace/api"
	cases := map[string]struct {
		files map[string]string
		want  []string
	}{
		"run_tests.sh comes first": {
			files: map[string]string{
				root + "/scripts/run_tests.sh": "",
				root + "/Makefile":             "test:\n\tgo test ./...\n",
			},
			want: []string{"scripts/run_tests.sh", "make test"},
		},
		"lockfile picks the package manager": {
			files: map[string]string{
				root + "/package.json":    `{"scripts":{"test":"vitest","lint":"eslint ."}}`,
				root + "/pnpm-lock.yaml":  "",
				root + "/yarn.lock":       "",
				root + "/pyproject.toml":  "[tool.pytest.ini_options]\n",
				root + "/does-not-matter": "",
			},
			// pnpm outranks yarn, and pytest follows the package scripts.
			want: []string{"pnpm run test", "pnpm run lint", "pytest"},
		},
		"npm is the default with no lockfile": {
			files: map[string]string{root + "/package.json": `{"scripts":{"build":"tsc"}}`},
			want:  []string{"npm run build"},
		},
		"a malformed manifest yields no scripts": {
			files: map[string]string{root + "/package.json": "{not json"},
			want:  []string{},
		},
		"pytest.ini alone is enough": {
			files: map[string]string{root + "/pytest.ini": ""},
			want:  []string{"pytest"},
		},
		"a pyproject without the pytest table is not": {
			files: map[string]string{root + "/pyproject.toml": "[project]\nname='x'\n"},
			want:  []string{},
		},
		"makefile targets are matched at line start": {
			files: map[string]string{
				root + "/Makefile": "test:\n\tgo test ./...\n# not-a-target: check:\nlint:\n\tgolangci-lint run\n",
			},
			want: []string{"make test", "make lint"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := detectVerifyCommands(snapshotOf(tc.files), root)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("commands = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDetectVerifyCommandsIsCapped(t *testing.T) {
	// Every verifyTarget present twice over (scripts and Makefile) is more than the nudge
	// should ever recite.
	const root = "/workspace/api"
	makefile := strings.Builder{}
	scripts := make([]string, 0, len(verifyTargets))
	for _, target := range verifyTargets {
		makefile.WriteString(target + ":\n\t/bin/true\n")
		scripts = append(scripts, `"`+target+`":"x"`)
	}
	got := detectVerifyCommands(snapshotOf(map[string]string{
		root + "/Makefile":     makefile.String(),
		root + "/package.json": `{"scripts":{` + strings.Join(scripts, ",") + `}}`,
	}), root)

	if len(got) != maxVerifyCommands {
		t.Fatalf("commands = %d, want the %d cap: %v", len(got), maxVerifyCommands, got)
	}
}
