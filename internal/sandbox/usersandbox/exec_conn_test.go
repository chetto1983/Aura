package usersandbox

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

type connTestProcess struct {
	done chan struct{}
	once sync.Once
}

func (p *connTestProcess) Kill()              { p.once.Do(func() { close(p.done) }) }
func (p *connTestProcess) Wait() (int, error) { <-p.done; return 0, nil }

func TestExecConnCarriesBytesBothWaysAndEndsWithTheProcess(t *testing.T) {
	p := &connTestProcess{done: make(chan struct{})}
	c, err := dialExecConn(context.Background(), func(in io.ReadCloser, out io.Writer) (execConnProcess, error) {
		go func() { defer in.Close(); _, _ = io.Copy(out, in) }()
		return p, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	go func() { _, _ = c.Write([]byte("input")) }()
	data := make([]byte, 5)
	if _, err = io.ReadFull(c, data); err != nil || string(data) != "input" {
		t.Fatalf("%q %v", data, err)
	}
	p.Kill()
	if _, err = c.Read(data); err != io.EOF {
		t.Fatalf("ended process should produce EOF: %v", err)
	}

	p2 := &connTestProcess{done: make(chan struct{})}
	c2, err := dialExecConn(context.Background(), func(io.ReadCloser, io.Writer) (execConnProcess, error) { return p2, nil })
	if err != nil {
		t.Fatal(err)
	}
	_ = c2.Close()
	select {
	case <-p2.done:
	case <-time.After(time.Second):
		t.Fatal("Close did not kill exec")
	}
}

func TestExecConnCancellationAndDeadlines(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &connTestProcess{done: make(chan struct{})}
	c, err := dialExecConn(ctx, func(io.ReadCloser, io.Writer) (execConnProcess, error) { return p, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(time.Millisecond))
	_, err = c.Read(make([]byte, 1))
	if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("deadline: %v", err)
	}
	cancel()
	select {
	case <-p.done:
	case <-time.After(time.Second):
		t.Fatal("context did not close exec")
	}
}
