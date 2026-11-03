package usersandbox

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func validSignInSpec() SignInSpec {
	return SignInSpec{Session: "signin-0123456789abcdef01234567", IdentityID: "owner", DesktopImage: "desktop:local", SidecarImage: "sidecar:local",
		AuthURL: "https://provider.example/authorize?state=s", CockpitOrigin: "https://aura.lan:8443", StreamOrigin: "https://aura.lan:8444", Egress: EgressPolicy{Floor: true}}
}

func TestSignInEnvHardensTheDesktop(t *testing.T) {
	s := validSignInSpec()
	s.Session = "signin-abc"
	env := strings.Join(signInEnv(s), "\n") + "\n"
	for _, entry := range []string{"HARDEN_DESKTOP=true", "HARDEN_OPENBOX=true", "SELKIES_FILE_TRANSFERS=none", "SELKIES_ENABLE_SHARING=false",
		"SUBFOLDER=/sign-in/signin-abc/", "CHROME_CLI=" + s.AuthURL, "SELKIES_FRAME_ANCESTORS=" + s.CockpitOrigin, "SELKIES_ALLOWED_ORIGINS=" + s.StreamOrigin} {
		if !strings.Contains(env, entry+"\n") {
			t.Errorf("missing %s", entry)
		}
	}
	for _, key := range []string{"PELORUS=", "PASSWORD=", "CUSTOM_USER="} {
		if strings.Contains(env, key) {
			t.Errorf("desktop credentials/helper enabled: %s", key)
		}
	}
}

func TestSignInHostConfigPublishesNothing(t *testing.T) {
	h := signInHostConfig("sidecar-id")
	if len(h.PortBindings) != 0 || len(h.Binds) != 0 || h.Privileged || len(h.CapAdd) != 0 || len(h.Devices) != 0 {
		t.Fatalf("unsafe host exposure: %+v", h)
	}
	if string(h.NetworkMode) != "container:sidecar-id" || h.ShmSize != signInShmBytes || h.Memory != signInMemoryBytes || h.PidsLimit == nil || *h.PidsLimit != signInPidsLimit || h.Init != nil {
		t.Fatalf("wrong desktop limits/netns/init: %+v", h)
	}
}

func TestSignInRefusesASessionThatIsNotOurs(t *testing.T) {
	b := NewDockerBackend(nil, "sidecar:local", Resources{}, WithEgress("floor:local"))
	for _, session := range []string{"", "../escape", "agent-0123456789abcdef01234567", "signin-abc", "signin-0123456789ABCDEF01234567"} {
		s := validSignInSpec()
		s.Session = session
		if _, err := b.StartSignIn(t.Context(), s, nil); err == nil {
			t.Errorf("accepted session %q", session)
		}
	}
}

func TestSignInRefusesToRunWithoutTheFloor(t *testing.T) {
	b := NewDockerBackend(nil, "sidecar:local", Resources{})
	if _, err := b.StartSignIn(t.Context(), validSignInSpec(), nil); !errors.Is(err, ErrSignInNeedsTheFloor) {
		t.Fatalf("StartSignIn = %v", err)
	}
}

func TestSignInRefusesNonBareOriginsBeforeCreatingAnything(t *testing.T) {
	b := NewDockerBackend(nil, "sidecar:local", Resources{}, WithEgress("floor:local"))
	for _, origin := range []string{"", "null", "https://aura.lan/path", "https://aura.lan?config=x", "https://user:pass@aura.lan", "https://*", "https://aura.lan;anything", "https://aura.lan\nother", "https://aura.lan#fragment"} {
		for _, desktop := range []bool{false, true} {
			s := validSignInSpec()
			if desktop {
				s.StreamOrigin = origin
			} else {
				s.CockpitOrigin = origin
			}
			if _, err := b.StartSignIn(t.Context(), s, nil); err == nil {
				t.Errorf("accepted origin %q", origin)
			}
		}
	}
	s := validSignInSpec()
	s.StreamOrigin = s.CockpitOrigin
	if _, err := b.StartSignIn(t.Context(), s, nil); err == nil {
		t.Error("accepted desktop on the cockpit origin")
	}
}

func signInAdmissionBackend(t *testing.T, running bool) *DockerBackend {
	t.Helper()
	cli := newFakeDockerClient(t, func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/containers/json") {
			t.Fatalf("unexpected daemon request: %s", r.URL)
		}
		if running && strings.Contains(r.URL.Query().Get("filters"), signInLabel) {
			body, err := json.Marshal([]map[string]any{{"Id": "desktop", "Names": []string{"/aura-signin-0123456789abcdef01234567-desktop"}, "Labels": map[string]string{signInLabel: "signin-0123456789abcdef01234567", signInRoleLabel: "desktop"}}})
			if err != nil {
				t.Fatal(err)
			}
			return dockerJSONResponse(http.StatusOK, string(body)), nil
		}
		return dockerJSONResponse(http.StatusOK, `[]`), nil
	})
	t.Cleanup(func() { _ = cli.Close() })
	return NewDockerBackend(cli, "sidecar:local", Resources{}, WithBrowserLimits(Resources{}, 1))
}

func TestRunningSignInKeepsItsBrowserSlot(t *testing.T) {
	b := signInAdmissionBackend(t, true)
	if err := b.AdmitBrowser(t.Context(), "another-desktop"); err == nil || !strings.Contains(err.Error(), "browser capacity") {
		t.Fatalf("running sign-in did not hold its slot: %v", err)
	}
}

func TestStoppingAnUnlaunchedSignInReleasesItsSlot(t *testing.T) {
	b := signInAdmissionBackend(t, false)
	session := validSignInSpec().Session
	if err := b.AdmitBrowser(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	if err := b.StopSignIn(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	if err := b.AdmitBrowser(t.Context(), "replacement-desktop"); err != nil {
		t.Fatalf("cancelled preflight retained its slot: %v", err)
	}
}
