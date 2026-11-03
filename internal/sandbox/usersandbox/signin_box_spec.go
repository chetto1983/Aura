package usersandbox

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/container"
)

const (
	signInLabel     = "aura.signin"
	signInRoleLabel = "aura.signin.role"
	// S6's public-page peak was 664 MiB; keep headroom for consent pages.
	signInMemoryBytes = 1536 << 20
	signInPidsLimit   = 2048
	signInShmBytes    = 1 << 30
)

var signInSessionPattern = regexp.MustCompile(`^signin-[a-f0-9]{24}$`)

// ErrSignInNeedsTheFloor prevents navigation before the tenancy boundary is installed.
var ErrSignInNeedsTheFloor = errors.New("sandbox: a sign-in needs the egress floor")

// SignInSpec omits host exposure and persistent mounts so a login cannot request them.
type SignInSpec struct {
	Session, IdentityID, DesktopImage, SidecarImage, AuthURL string
	CockpitOrigin, StreamOrigin                              string
	Egress                                                   EgressPolicy
}

// SignInBox exposes only the callback/relay sidecar to trusted adapters.
type SignInBox struct {
	Session string
	Sidecar BoxHandle
}

// SignInPrepare installs the listener before Chromium can complete an immediate redirect.
type SignInPrepare func(context.Context, SignInBox) error

func signInEnv(s SignInSpec) []string {
	return []string{
		"HARDEN_DESKTOP=true", "HARDEN_OPENBOX=true", "SELKIES_FILE_TRANSFERS=none", "SELKIES_ENABLE_SHARING=false",
		"SUBFOLDER=/sign-in/" + s.Session + "/", "CHROME_CLI=" + s.AuthURL,
		"SELKIES_FRAME_ANCESTORS=" + s.CockpitOrigin, "SELKIES_ALLOWED_ORIGINS=" + s.StreamOrigin,
	}
}

func signInHostConfig(owner string) *container.HostConfig {
	pids := int64(signInPidsLimit)
	return &container.HostConfig{NetworkMode: container.NetworkMode("container:" + owner), ShmSize: signInShmBytes,
		Memory: signInMemoryBytes, PidsLimit: &pids}
}

func validateSignIn(s SignInSpec) error {
	if !signInSessionPattern.MatchString(s.Session) {
		return errors.New("sandbox: invalid sign-in session")
	}
	if strings.TrimSpace(s.IdentityID) == "" || s.DesktopImage == "" || s.SidecarImage == "" {
		return errors.New("sandbox: incomplete sign-in configuration")
	}
	a, err := bareSignInOrigin(s.CockpitOrigin)
	if err != nil {
		return err
	}
	b, err := bareSignInOrigin(s.StreamOrigin)
	if err != nil {
		return err
	}
	if a.Scheme != b.Scheme || !strings.EqualFold(a.Hostname(), b.Hostname()) || originPort(a) == originPort(b) {
		return errors.New("sandbox: sign-in needs the same hostname and scheme on a separate port")
	}
	u, err := url.Parse(s.AuthURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("sandbox: invalid sign-in authorization address")
	}
	return nil
}

func bareSignInOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		raw != u.Scheme+"://"+u.Host || strings.ContainsAny(raw, "*;,| \t\r\n\"'\\") {
		return nil, errors.New("sandbox: sign-in origins must be bare HTTP(S) origins")
	}
	if p := originPort(u); p < 1 || p > 65535 {
		return nil, fmt.Errorf("sandbox: invalid sign-in origin port")
	}
	return u, nil
}

func originPort(u *url.URL) int {
	if u.Port() == "" {
		if u.Scheme == "https" {
			return 443
		}
		return 80
	}
	p, _ := strconv.Atoi(u.Port())
	return p
}

func signInLabels(session, role string) map[string]string {
	return map[string]string{signInLabel: session, signInRoleLabel: role}
}
