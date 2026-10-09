package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/approvalpolicies"
)

type cliPolicyStore struct {
	rows  []approvalpolicies.Row
	setBy string
}

func (f *cliPolicyStore) Set(_ context.Context, _, tool, action string, p approvalpolicies.Policy, setBy string) error {
	f.setBy = setBy
	f.rows = append(f.rows, approvalpolicies.Row{Tool: tool, Action: action, Policy: p, SetAt: time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC), SetBy: setBy})
	return nil
}

func (f *cliPolicyStore) List(context.Context, string) ([]approvalpolicies.Row, error) {
	return f.rows, nil
}

func (f *cliPolicyStore) Clear(_ context.Context, _, tool, action string) (bool, error) {
	for i, r := range f.rows {
		if r.Tool == tool && r.Action == action {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func resolveAlice(name string) (string, error) {
	if name == "alice" {
		return "11111111-1111-1111-1111-111111111111", nil
	}
	return "", errors.New("identity not found: " + name)
}

func runPolicy(t *testing.T, store *cliPolicyStore, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := gatewayPolicyCommand(context.Background(), resolveAlice, store, args, &out)
	return out.String(), err
}

func TestGatewayPolicySetListClear(t *testing.T) {
	store := &cliPolicyStore{}
	if out, err := runPolicy(t, store, "set", "alice", "calendar", "send_email", "deny"); err != nil || !strings.Contains(out, `"calendar send_email" is now deny`) {
		t.Fatalf("set multiplexed = %q, %v", out, err)
	}
	if out, err := runPolicy(t, store, "set", "alice", "shell_exec", "ask"); err != nil || !strings.Contains(out, `"shell_exec" is now ask`) {
		t.Fatalf("set plain = %q, %v", out, err)
	}
	if store.setBy != policyCLIActor {
		t.Errorf("set_by = %q, want %q", store.setBy, policyCLIActor)
	}
	out, err := runPolicy(t, store, "list", "alice")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	want := [][]string{
		{"TOOL", "ACTION", "POLICY", "SET", "AT", "BY"},
		{"calendar", "send_email", "deny", "2026-10-09", "18:00", "cli"},
		{"shell_exec", "-", "ask", "2026-10-09", "18:00", "cli"},
	}
	if len(lines) != len(want) {
		t.Fatalf("list = %q", out)
	}
	for i, line := range lines {
		if got := strings.Fields(line); strings.Join(got, " ") != strings.Join(want[i], " ") {
			t.Errorf("list line %d = %q, want %q", i, got, want[i])
		}
	}
	if out, _ := runPolicy(t, store, "clear", "alice", "shell_exec"); !strings.Contains(out, "ok: cleared") {
		t.Fatalf("clear = %q", out)
	}
	if out, _ := runPolicy(t, store, "clear", "alice", "shell_exec"); !strings.Contains(out, "nothing cleared") {
		t.Fatalf("second clear must not print success: %q", out)
	}
	store.rows = nil
	if out, _ := runPolicy(t, store, "list", "alice"); !strings.Contains(out, "no tool policies") {
		t.Fatalf("empty list = %q", out)
	}
}

func TestGatewayPolicyRefusesBadInput(t *testing.T) {
	store := &cliPolicyStore{}
	for _, args := range [][]string{
		{},
		{"set"},
		{"set", "alice", "shell_exec"},
		{"set", "alice", "a", "b", "c", "ask"},
		{"clear", "alice"},
		{"list", "alice", "extra"},
		{"revoke", "alice", "x"},
	} {
		if _, err := runPolicy(t, store, args...); !errors.Is(err, errGatewayPolicyUsage) {
			t.Errorf("%v = %v, want usage", args, err)
		}
	}
	if _, err := runPolicy(t, store, "set", "alice", "shell_exec", "maybe"); !errors.Is(err, approvalpolicies.ErrInvalidPolicy) {
		t.Errorf("unknown word = %v, want ErrInvalidPolicy", err)
	}
	if _, err := runPolicy(t, store, "list", "bob"); err == nil || !strings.Contains(err.Error(), "bob") {
		t.Errorf("unknown identity = %v", err)
	}
	if len(store.rows) != 0 {
		t.Fatalf("a refused command wrote %+v", store.rows)
	}
}
