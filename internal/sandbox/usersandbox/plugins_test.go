package usersandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func commitOf(c byte) string { return strings.Repeat(string(c), 40) }

func treeNames(trees []PluginTree) []string {
	names := make([]string, 0, len(trees))
	for _, t := range trees {
		names = append(names, t.Name)
	}
	return names
}

func TestPlanPluginSyncWritesOnlyChangedOrMissingTrees(t *testing.T) {
	t.Parallel()
	inBox := map[string]string{"a": commitOf('1'), "b": commitOf('2'), "gone": commitOf('5'), "stale": commitOf('9')}
	present := map[string]bool{"a": true, "b": true, "stale": true}
	want := []PluginTree{
		{Name: "a", Version: commitOf('1')},
		{Name: "b", Version: commitOf('3')},
		{Name: "gone", Version: commitOf('5')},
		{Name: "new", Version: commitOf('4')},
	}
	remove, write := planPluginSync(inBox, present, want)
	if !slices.Equal(remove, []string{"stale"}) {
		t.Errorf("remove = %v, want [stale]", remove)
	}
	if got := treeNames(write); !slices.Equal(got, []string{"b", "gone", "new"}) {
		t.Errorf("write = %v, want [b gone new]", got)
	}
}

// The listing comes out of the box, which the model writes to, and its names become arguments
// of an `rm -rf` and parts of box paths.
func TestParseMarkersDropsUnsafeEntries(t *testing.T) {
	t.Parallel()
	c := commitOf('a')
	out := strings.Join([]string{
		"good " + c + " 1",
		"absent " + c + " 0",
		"$(rm${IFS}-rf${IFS}/w) " + c + " 1",
		".. " + c + " 1",
		" " + c + " 1",
		"short abc 1",
		"upper " + commitOf('A') + " 1",
		"",
	}, "\n")
	commits, present := parseMarkers(out)
	if want := map[string]string{"good": c, "absent": c}; !maps.Equal(commits, want) {
		t.Errorf("commits = %v, want %v", commits, want)
	}
	if !present["good"] || present["absent"] {
		t.Errorf("present = %v, want good only", present)
	}
}

// boxDocker answers the Docker calls the mirror makes without attaching a stream: execs that
// exit 0, and archive uploads, whose entry names it records.
type boxDocker struct {
	mu      sync.Mutex
	execs   [][]string
	entries []string
}

func newBoxDocker(t *testing.T) (*boxDocker, *DockerBackend) {
	t.Helper()
	rec := &boxDocker{}
	cli := newFakeDockerClient(t, func(req *http.Request) (*http.Response, error) {
		p := req.URL.Path
		switch {
		case strings.HasSuffix(p, "/containers/box/exec"):
			var body struct{ Cmd []string }
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				return nil, err
			}
			rec.mu.Lock()
			rec.execs = append(rec.execs, body.Cmd)
			rec.mu.Unlock()
			return dockerJSONResponse(http.StatusCreated, `{"Id":"e1"}`), nil
		case strings.HasSuffix(p, "/exec/e1/start"):
			return dockerJSONResponse(http.StatusOK, ``), nil
		case strings.HasSuffix(p, "/exec/e1/json"):
			return dockerJSONResponse(http.StatusOK, `{"Running":false,"ExitCode":0}`), nil
		case strings.HasSuffix(p, "/containers/box/archive") && req.Method == http.MethodPut:
			tr := tar.NewReader(req.Body)
			for {
				hdr, err := tr.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					return nil, err
				}
				rec.mu.Lock()
				rec.entries = append(rec.entries, hdr.Name)
				rec.mu.Unlock()
			}
			return dockerJSONResponse(http.StatusOK, ``), nil
		}
		return nil, fmt.Errorf("unexpected request: %s %s", req.Method, p)
	})
	return rec, NewDockerBackend(cli, "box-image", Resources{})
}

func TestClearDestPassesItsTargetAsArgv(t *testing.T) {
	t.Parallel()
	rec, b := newBoxDocker(t)
	if err := clearDest(context.Background(), b.cli, BoxHandle{ContainerID: "box"}, "/skills"); err != nil {
		t.Fatalf("clearDest: %v", err)
	}
	if len(rec.execs) != 1 || !slices.Equal(rec.execs[0], []string{"rm", "-rf", "--", "/skills"}) {
		t.Errorf("execs = %q, want one rm argv with no shell", rec.execs)
	}
}

// Not parallel: it swaps the default logger to read the skip.
func TestPluginSyncSkipsTheBrowserBoxAndAFaultyTree(t *testing.T) {
	called := false
	browser := NewDockerBackend(nil, "box-image", Resources{}, WithPluginTrees(func(context.Context) ([]PluginTree, error) {
		called = true
		return nil, nil
	}))
	if err := browser.syncPlugins(context.Background(), BoxHandle{ContainerID: "box", Kind: Browser}); err != nil || called {
		t.Fatalf("browser box: err %v, resolver called %v", err, called)
	}

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	one := seedSkillTree(t, "run.sh", "one")
	notADir := filepath.Join(seedSkillTree(t, "plugin.json", "{}"), "plugin.json")
	two := seedSkillTree(t, "run.sh", "two")
	rec, b := newBoxDocker(t)
	c := commitOf('c')
	err := b.applyPluginSync(context.Background(), BoxHandle{ContainerID: "box"}, []string{"old"}, []PluginTree{
		{Name: "one", HostDir: one, Version: c},
		{Name: "bad", HostDir: notADir, Version: c},
		{Name: "two", HostDir: two, Version: c},
	})
	if err != nil {
		t.Fatalf("applyPluginSync: %v", err)
	}
	want := []string{
		"plugins/one/run.sh", "plugins/.aura/one",
		"plugins/two/run.sh", "plugins/.aura/two",
	}
	if !slices.Equal(rec.entries, want) {
		t.Errorf("copied %q, want %q", rec.entries, want)
	}
	if !slices.ContainsFunc(rec.execs, func(cmd []string) bool {
		return slices.Equal(cmd, []string{"rm", "-rf", "--", "/plugins/old", "/plugins/.aura/old"})
	}) {
		t.Errorf("execs = %q, want the removed tree and its marker deleted through argv", rec.execs)
	}
	if !strings.Contains(logs.String(), "bad") {
		t.Errorf("the skipped tree was not logged: %q", logs.String())
	}
}
