package packs

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func installFixture() Pack {
	return Pack{
		Source: "anthropics/knowledge-work-plugins", Directory: "sales",
		Name: "sales", Version: "1.3.0",
		Skills: []string{"call-prep", "forecast"},
		Servers: []Server{
			{Name: "gmail", Type: "http"},
			{Name: "hubspot", Type: "http", URL: "https://mcp.hubspot.com/anthropic"},
			{Name: "notes", Type: "stdio", Command: "notes-mcp"},
		},
	}
}

func TestInstallRunsEveryStepAndAccountsForIt(t *testing.T) {
	t.Parallel()
	var skills, servers []string
	rep, err := Installer{
		InstallSkill: func(_ context.Context, ref string) error { skills = append(skills, ref); return nil },
		AddServer: func(_ context.Context, name string, _ Server) error {
			servers = append(servers, name)
			return nil
		},
	}.Install(t.Context(), installFixture())
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if want := []string{
		"anthropics/knowledge-work-plugins@call-prep",
		"anthropics/knowledge-work-plugins@forecast",
	}; !equal(skills, want) {
		t.Errorf("skills = %v, want the installer syntax %v", skills, want)
	}
	if !equal(servers, []string{"gmail", "hubspot", "notes"}) {
		t.Errorf("servers = %v", servers)
	}
	if rep.Failed() != 0 || len(rep.Skills) != 2 || len(rep.Servers) != 3 {
		t.Errorf("report = %+v", rep)
	}
}

// A pack is many things at once — small-business ships thirty-one skills — and
// one withdrawn skill must not abort the other thirty and leave the operator
// with a half-installed pack and no account of it.
func TestInstallCarriesOnPastAFailureAndReportsIt(t *testing.T) {
	t.Parallel()
	boom := errors.New("skill not found in catalogue")
	rep, err := Installer{
		InstallSkill: func(_ context.Context, ref string) error {
			if strings.HasSuffix(ref, "@call-prep") {
				return boom
			}
			return nil
		},
		AddServer: func(context.Context, string, Server) error { return nil },
	}.Install(t.Context(), installFixture())
	if err != nil {
		t.Fatalf("Install returned a fatal error for one bad step: %v", err)
	}
	if len(rep.Skills) != 2 {
		t.Fatalf("the second skill was skipped: %+v", rep.Skills)
	}
	if !errors.Is(rep.Skills[0].Err, boom) || rep.Skills[1].Err != nil {
		t.Errorf("failures are not recorded per step: %+v", rep.Skills)
	}
	if rep.Failed() != 1 {
		t.Errorf("Failed() = %d, want 1", rep.Failed())
	}
}

func TestInstallStopsWhenTheContextIsCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := Installer{
		InstallSkill: func(context.Context, string) error {
			t.Error("a cancelled install still ran a step")
			return nil
		},
	}.Install(ctx, installFixture())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// A nil seam is how a caller installs only one half.
func TestInstallSkipsAHalfWithNoSeam(t *testing.T) {
	t.Parallel()
	rep, err := Installer{AddServer: func(context.Context, string, Server) error { return nil }}.
		Install(t.Context(), installFixture())
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(rep.Skills) != 0 || len(rep.Servers) != 3 {
		t.Errorf("report = %+v", rep)
	}
}

// Installing a placeholder would put a server in the managed config that can
// never connect, that the operator then has to approve, and that the model sees
// as a capability it does not have.
func TestInstallableServersLeavesPlaceholdersOut(t *testing.T) {
	t.Parallel()
	install, skipped := InstallableServers(installFixture())

	if len(install) != 2 || install[0].Name != "hubspot" || install[1].Name != "notes" {
		t.Errorf("installable = %+v", install)
	}
	if len(skipped) != 1 || skipped[0].Name != "gmail" {
		t.Errorf("skipped = %+v", skipped)
	}
}

func TestSourceMarkerRoundTrips(t *testing.T) {
	t.Parallel()
	ref := Ref{Source: "anthropics/knowledge-work-plugins", Directory: "sales"}
	marker := SourceMarker(ref)
	if marker != "pack:anthropics/knowledge-work-plugins/sales" {
		t.Fatalf("marker = %q", marker)
	}
	got, ok := RefFromSource(marker)
	if !ok || got != ref {
		t.Fatalf("RefFromSource(%q) = %+v, %v", marker, got, ok)
	}
}

// Aura's own sources are bare words. None of them may read as a pack, or a
// single trust-approve would sweep in servers no pack installed.
func TestRefFromSourceIgnoresEverySourceNoPackWrote(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"manual", "custom", "env:AURA_MCP_SERVERS_JSON", "recipe:whatsapp",
		"", "pack:", "pack:owner", "pack:owner/repo/..", "packed:owner/repo",
	} {
		if ref, ok := RefFromSource(s); ok {
			t.Errorf("RefFromSource(%q) = %+v, want it refused", s, ref)
		}
	}
}

func TestWriteReportNamesEveryOutcome(t *testing.T) {
	t.Parallel()
	rep := Report{
		Pack:    installFixture(),
		Skills:  []StepResult{{Name: "a"}, {Name: "b", Err: errors.New("gone")}},
		Servers: []StepResult{{Name: "hubspot"}},
	}

	var out bytes.Buffer
	WriteReport(&out, rep)
	got := out.String()
	for _, want := range []string{"sales v1.3.0", "ok      a", "FAILED  b — gone", "ok      hubspot", "1 step(s) failed"} {
		if !strings.Contains(got, want) {
			t.Errorf("report is missing %q:\n%s", want, got)
		}
	}
}

func TestWriteReportSaysNothingAboutAnEmptyHalf(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	WriteReport(&out, Report{Pack: Pack{Name: "bare"}})
	if strings.Contains(out.String(), "connectors") {
		t.Errorf("an empty half was rendered:\n%s", out.String())
	}
}
