package usersandbox

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

func completedStreamForTest(cli *client.Client) *ExecStreamHandle {
	done := make(chan struct{})
	close(done)
	return &ExecStreamHandle{cli: cli, container: "box", pidFile: "/tmp/job.pid", execID: "finished", done: done, killReq: make(chan struct{}), stopDone: make(chan struct{})}
}

func TestExecStreamKillAfterConfirmedCompletionDoesNothing(t *testing.T) {
	var creates atomic.Int32
	cli := newFakeDockerClient(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/containers/box/exec") {
			creates.Add(1)
			return nil, context.DeadlineExceeded
		}
		return dockerJSONResponse(http.StatusOK, `{"Running":false,"ExitCode":17}`), nil
	})
	defer cli.Close()
	h := completedStreamForTest(cli)
	t.Cleanup(func() {
		select {
		case <-h.killReq:
			<-h.stopDone
		default:
		}
	})
	if code, err := h.Wait(); code != 17 || err != nil {
		t.Fatalf("Wait=%d, %v", code, err)
	}
	// The background-shell reaper defers Kill after Wait returns.
	h.Kill()
	h.Kill()
	select {
	case <-h.killReq:
		t.Fatal("Kill started termination after confirmed completion")
	default:
	}
	if creates.Load() != 0 {
		t.Fatal("completed job created a Docker cleanup execution")
	}
	if code, err := h.Wait(); code != 17 || err != nil {
		t.Fatalf("repeated Wait lost completion: %d, %v", code, err)
	}
}

func TestExecStreamWaitJoinsKillDuringFinalInspection(t *testing.T) {
	inspecting, releaseInspect := make(chan struct{}), make(chan struct{})
	stopping, releaseStop := make(chan struct{}), make(chan struct{})
	cli := newFakeDockerClient(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/containers/box/exec") {
			close(stopping)
			<-releaseStop
			return nil, context.DeadlineExceeded
		}
		close(inspecting)
		<-releaseInspect
		return dockerJSONResponse(http.StatusOK, `{"Running":false,"ExitCode":17}`), nil
	})
	defer cli.Close()
	h := completedStreamForTest(cli)
	type result struct {
		code int
		err  error
	}
	returned := make(chan result, 1)
	go func() { code, err := h.Wait(); returned <- result{code, err} }()
	<-inspecting
	h.Kill()
	<-stopping
	close(releaseInspect)
	var early *result
	select {
	case got := <-returned:
		early = &got
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseStop)
	<-h.stopDone
	if early != nil {
		t.Fatalf("Wait bypassed active termination: code=%d err=%v", early.code, early.err)
	}
	select {
	case got := <-returned:
		if !errors.Is(got.err, ErrExecUnconfirmed) || !errors.Is(got.err, context.DeadlineExceeded) {
			t.Fatalf("Wait lost termination failure: code=%d err=%v", got.code, got.err)
		}
		if code, err := h.Wait(); code != got.code || err != got.err {
			t.Fatalf("repeated Wait lost termination result: %d, %v", code, err)
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not return after termination settled")
	}
}
