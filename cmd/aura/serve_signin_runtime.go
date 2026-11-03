package main

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/browserlogin"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

type signInRuntime struct {
	login       *browserlogin.Login
	browser     *sandboxSignInBrowser
	origin      string
	port        int
	root        context.Context
	stop        context.CancelFunc
	pullMu      sync.Mutex
	pullState   agui.SignInImageState
	pullWorkers sync.WaitGroup
	closed      bool
}

func wireSignInRuntime(ctx context.Context, chat *chatEnv, server *agui.Server, service *mcpAuthService) {
	root, stop := context.WithCancel(ctx)
	browser := &sandboxSignInBrowser{router: chat.sandboxRouter, image: chat.cfg.Sandbox.SignInBrowserImage, sidecarImage: chat.cfg.Sandbox.Image,
		policy: usersandbox.EgressPolicy{Floor: true, FQDNAllowlist: chat.cfg.Sandbox.EgressAllowlist}}
	login := browserlogin.New(root, browser, browserlogin.Options{})
	browser.owns, browser.cancel = login.Owns, login.Cancel
	service.signIn = &signInRuntime{login: login, browser: browser, origin: chat.cfg.MCPSandboxOrigin, port: chat.cfg.MCPSandboxPort, root: root, stop: stop}
	server.SetSignInBrowser(browser)
	server.SetSignInImages(service.signIn)
}

func signInStreamOrigin(request agui.SignInRequest, configured string, port int) (string, error) {
	refuse := errors.New("this address cannot reach the sign-in desktop; use your own browser")
	if request.Remote {
		return "", refuse
	}
	parent, err := parseSignInOrigin(request.Origin)
	if err != nil || (parent.Scheme == "http" && parent.Hostname() != "localhost" && !net.ParseIP(parent.Hostname()).IsLoopback()) {
		return "", refuse
	}
	stream := configured
	if stream == "" {
		if port < 1 || port > 65535 {
			return "", refuse
		}
		stream = "https://" + net.JoinHostPort(parent.Hostname(), strconv.Itoa(port))
	}
	child, err := parseSignInOrigin(stream)
	if err != nil || child.Scheme != parent.Scheme || !strings.EqualFold(child.Hostname(), parent.Hostname()) || effectiveSignInPort(child) == effectiveSignInPort(parent) {
		return "", refuse
	}
	return stream, nil
}

func parseSignInOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || raw != u.Scheme+"://"+u.Host || strings.ContainsAny(raw, "*;,| \t\r\n\"'\\") {
		return nil, errors.New("invalid sign-in origin")
	}
	if p := effectiveSignInPort(u); p < 1 || p > 65535 {
		return nil, errors.New("invalid sign-in origin port")
	}
	return u, nil
}

func effectiveSignInPort(u *url.URL) int {
	if u.Port() == "" {
		if u.Scheme == "https" {
			return 443
		}
		return 80
	}
	p, _ := strconv.Atoi(u.Port())
	return p
}
