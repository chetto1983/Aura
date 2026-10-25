package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/chetto1983/aura/internal/packs"
	"github.com/chetto1983/aura/internal/plugins"
)

// fakePlugins answers each call with the error a test sets, recording what reached it.
type fakePlugins struct {
	err        error
	installed  plugins.InstallRequest
	removed    string
	deleteData bool
}

func (f *fakePlugins) List() ([]plugins.Record, error) {
	return []plugins.Record{{Name: "broker-analysis", Phase: plugins.PhaseInstalled}}, f.err
}

func (f *fakePlugins) Review(_ context.Context, ref string) (plugins.Review, error) {
	if f.err != nil {
		return plugins.Review{}, f.err
	}
	return plugins.Review{
		Ref:               ref,
		Connectors:        []plugins.Connector{{Name: "broker-analysis", Placement: plugins.PlacementBox}},
		SkippedConnectors: []packs.Skipped{{Name: "memory", Reason: "reserved by recipe:memory"}},
	}, nil
}

func (f *fakePlugins) Install(_ context.Context, _ string, req plugins.InstallRequest) (plugins.Report, error) {
	f.installed = req
	return plugins.Report{Skills: []plugins.Step{{Name: "s", Outcome: plugins.OutcomeOK}}}, f.err
}

func (f *fakePlugins) Remove(_ context.Context, _, name string, deleteData bool) (plugins.RemoveReport, error) {
	f.removed, f.deleteData = name, deleteData
	return plugins.RemoveReport{Name: name, DataDeleted: deleteData}, f.err
}

func pluginServer(p PluginsProvider) *Server {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetGovernanceWriteProviders(GovernanceWriteProviders{Plugins: p})
	return s
}

const commit40 = "0123456789abcdef0123456789abcdef01234567"

func TestPluginRoutes(t *testing.T) {
	t.Run("unwired", func(t *testing.T) {
		s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
		for _, rt := range []writeRoute{
			{http.MethodGet, "/api/governance/plugins", ""},
			{http.MethodPost, "/api/governance/plugins/review", `{"ref":"o/r/p"}`},
			{http.MethodPost, "/api/governance/plugins", `{"ref":"o/r/p","commit":"` + commit40 + `","digest":"d"}`},
			{http.MethodDelete, "/api/governance/plugins/p", ""},
		} {
			if rec := doGovWrite(t, s, rt.method, rt.target, rt.body, true); rec.Code != http.StatusServiceUnavailable {
				t.Errorf("%s %s = %d, want 503", rt.method, rt.target, rec.Code)
			}
		}
	})
	t.Run("a write with no principal", func(t *testing.T) {
		s := pluginServer(&fakePlugins{})
		if rec := doGovWrite(t, s, http.MethodDelete, "/api/governance/plugins/p", "", false); rec.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated remove = %d, want 401", rec.Code)
		}
	})
	t.Run("status codes", func(t *testing.T) {
		for err, want := range map[error]int{
			fmt.Errorf("%w: bad ref", plugins.ErrInvalidRequest): http.StatusBadRequest,
			plugins.ErrDigestChanged:                             http.StatusConflict,
			fmt.Errorf("%w: p", plugins.ErrAlreadyInstalled):     http.StatusConflict,
			fmt.Errorf("%w: p", plugins.ErrNotInstalled):         http.StatusNotFound,
			errors.New("fetch o/r: exit status 128"):             http.StatusBadGateway,
		} {
			s := pluginServer(&fakePlugins{err: err})
			body := `{"ref":"o/r/p","commit":"` + commit40 + `","digest":"d"}`
			if rec := doGovWrite(t, s, http.MethodPost, "/api/governance/plugins", body, true); rec.Code != want {
				t.Errorf("install with %v = %d, want %d", err, rec.Code, want)
			}
		}
	})
	t.Run("a malformed body", func(t *testing.T) {
		s := pluginServer(&fakePlugins{})
		if rec := doGovWrite(t, s, http.MethodPost, "/api/governance/plugins/review", `{"ref":`, true); rec.Code != http.StatusBadRequest {
			t.Errorf("malformed review = %d, want 400", rec.Code)
		}
	})
	t.Run("the install reaches the backend with the approved request", func(t *testing.T) {
		f := &fakePlugins{}
		body := `{"ref":"o/r/p","commit":"` + commit40 + `","digest":"d"}`
		if rec := doGovWrite(t, pluginServer(f), http.MethodPost, "/api/governance/plugins", body, true); rec.Code != http.StatusOK {
			t.Fatalf("install = %d %s", rec.Code, rec.Body)
		}
		if f.installed != (plugins.InstallRequest{Ref: "o/r/p", Commit: commit40, Digest: "d"}) {
			t.Errorf("installed %+v", f.installed)
		}
	})
	t.Run("data=delete reaches the removal", func(t *testing.T) {
		for target, want := range map[string]bool{
			"/api/governance/plugins/p?data=delete": true,
			"/api/governance/plugins/p":             false,
		} {
			f := &fakePlugins{}
			if rec := doGovWrite(t, pluginServer(f), http.MethodDelete, target, "", true); rec.Code != http.StatusOK {
				t.Fatalf("remove = %d", rec.Code)
			}
			if f.removed != "p" || f.deleteData != want {
				t.Errorf("%s: removed %q deleteData %v", target, f.removed, f.deleteData)
			}
		}
	})
	t.Run("the review fields", func(t *testing.T) {
		rec := doGovWrite(t, pluginServer(&fakePlugins{}), http.MethodPost, "/api/governance/plugins/review", `{"ref":"o/r/p"}`, true)
		var got struct {
			Connectors        []struct{ Placement string }
			SkippedConnectors []struct{ Reason string } `json:"skipped_connectors"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode %s: %v", rec.Body, err)
		}
		if len(got.Connectors) != 1 || got.Connectors[0].Placement != "box" ||
			len(got.SkippedConnectors) != 1 || got.SkippedConnectors[0].Reason != "reserved by recipe:memory" {
			t.Errorf("review = %s", rec.Body)
		}
	})
	t.Run("the list", func(t *testing.T) {
		rec := doGovWrite(t, pluginServer(&fakePlugins{}), http.MethodGet, "/api/governance/plugins", "", true)
		var got struct{ Plugins []plugins.Record }
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Plugins) != 1 || got.Plugins[0].Name != "broker-analysis" {
			t.Errorf("list = %d %s (%v)", rec.Code, rec.Body, err)
		}
	})
}
