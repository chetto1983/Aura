//go:build docker_integration

package main

import (
	"bufio"
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

func TestSignInRelayCarriesBinaryThroughExecConn(t *testing.T) {
	ctx, router, box := liveBox(t)
	read, write := io.Pipe()
	server, err := router.ExecStream(ctx, box, usersandbox.ExecRequest{Command: `node -e 'require("node:net").createServer(s=>s.pipe(s)).listen(3000,"127.0.0.1",()=>console.log("ready"))'`}, nil, write)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Kill(); _ = read.Close(); _ = write.Close(); _, _ = server.Wait() })
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(read).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "ready\n" {
			t.Fatalf("server readiness=%q", line)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err = router.WriteFile(ctx, box, "/tmp/signin-relay.mjs", signInRelayScript); err != nil {
		t.Fatal(err)
	}
	conn, err := router.DialExec(ctx, box, "node /tmp/signin-relay.mjs")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	payload := bytes.Repeat([]byte{0, 255, 13, 10, 65, 0, 32, 127}, 32768)
	written := make(chan error, 1)
	go func() { _, e := conn.Write(payload); written <- e }()
	received := make([]byte, len(payload))
	if _, err = io.ReadFull(conn, received); err != nil {
		t.Fatal(err)
	}
	if err = <-written; err != nil || !bytes.Equal(payload, received) {
		t.Fatalf("binary transport failed: %v", err)
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
}
