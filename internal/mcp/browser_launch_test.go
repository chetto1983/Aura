package mcp

import (
	"context"
	"errors"
	"io"
	"testing"
)

type browserOnlyBox struct{ fakeBox }

func (*browserOnlyBox) StartStdio(context.Context, BoxStart, io.ReadCloser, io.Writer) (BoxProcess, error) {
	return nil, errors.New("browser was sent to the shell launcher")
}

func (b *browserOnlyBox) StartBrowserStdio(ctx context.Context, start BoxStart, in io.ReadCloser, out io.Writer) (BoxProcess, error) {
	return b.fakeBox.StartStdio(ctx, start, in, out)
}

func TestDirectManagedBrowserSessionUsesDedicatedLauncher(t *testing.T) {
	box := &browserOnlyBox{}
	server := ManagedServer{Command: "agent-browser", Args: []string{"mcp"}, Source: "recipe:browser", Trust: ManagedTrust{Class: TrustTrustedRecipe}, Runtime: ManagedRuntime{Kind: RuntimeKindBox}}
	session, err := OpenSDKSession(t.Context(), "browser", server, EgressPolicy{}, SessionOptions{Box: box})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ListTools(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	for _, p := range box.procs {
		<-p.done
	}
}
