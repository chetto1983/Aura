package usersandbox

import (
	"net/http"
	"strings"
	"testing"
)

func TestSignInImagePullCountsDownloadsRatherThanExtraction(t *testing.T) {
	cli := newFakeDockerClient(t, func(*http.Request) (*http.Response, error) {
		return dockerJSONResponse(http.StatusOK,
			`{"id":"a","status":"Downloading","progressDetail":{"current":2,"total":10}}`+"\n"+
				`{"id":"a","status":"Extracting","progressDetail":{"current":90,"total":100}}`+"\n"+
				`{"id":"b","status":"Downloading","progressDetail":{"current":3,"total":20}}`+"\n"+
				`{"id":"a","status":"Download complete"}`+"\n"+`{"id":"b","status":"Download complete"}`+"\n"), nil
	})
	t.Cleanup(func() { _ = cli.Close() })
	b := NewDockerBackend(cli, "sidecar:local", Resources{})
	var done, total int64
	if err := b.PullSignInImage(t.Context(), "desktop:test", func(d, n int64) {
		done, total = d, n
		if n > 30 {
			t.Fatal("extraction counted as downloaded bytes")
		}
	}); err != nil {
		t.Fatal(err)
	}
	if done != 30 || total != 30 {
		t.Fatalf("download progress %d/%d", done, total)
	}
}

func TestSignInImagePullSurfacesDaemonStreamErrors(t *testing.T) {
	cli := newFakeDockerClient(t, func(*http.Request) (*http.Response, error) {
		return dockerJSONResponse(http.StatusOK, `{"errorDetail":{"message":"registry denied"}}`+"\n"), nil
	})
	t.Cleanup(func() { _ = cli.Close() })
	b := NewDockerBackend(cli, "sidecar:local", Resources{})
	if err := b.PullSignInImage(t.Context(), "desktop:test", nil); err == nil || !strings.Contains(err.Error(), "registry denied") {
		t.Fatalf("pull failure = %v", err)
	}
}
