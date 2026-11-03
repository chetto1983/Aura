package agui

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
)

// SignInBrowser exposes transport and owner checks, never page contents or browser control.
type SignInBrowser interface {
	Owns(ctx context.Context, owner, session string) bool
	Origins(session string) (cockpit, stream string, ok bool)
	Dial(ctx context.Context, session string) (net.Conn, error)
	Cancel(ctx context.Context, owner, session string) error
}

var signInProxySessionPattern = regexp.MustCompile(`^signin-[a-f0-9]{24}$`)

// SetSignInBrowser binds only the human login runtime, outside agent-browser routes.
func (s *Server) SetSignInBrowser(b SignInBrowser) { s.signInBrowser = b }

func (s *Server) registerSignInRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/sign-in/{session}/", s.handleSignInProxy)
	mux.HandleFunc("GET /api/sign-in/{session}", s.handleSignInInfo)
	mux.HandleFunc("DELETE /api/sign-in/{session}", s.handleSignInCancel)
}

func (s *Server) ownedSignIn(r *http.Request) (session, cockpit, stream string, ok bool) {
	session = r.PathValue("session")
	id, exists := owner(r)
	if !exists || !signInProxySessionPattern.MatchString(session) || s.signInBrowser == nil || !s.signInBrowser.Owns(r.Context(), id, session) {
		return "", "", "", false
	}
	cockpit, stream, ok = s.signInBrowser.Origins(session)
	if !ok || !validSignInOrigins(cockpit, stream) {
		return "", "", "", false
	}
	return session, cockpit, stream, true
}

func validSignInOrigins(cockpit, stream string) bool {
	var parsed []*url.URL
	for _, raw := range []string{cockpit, stream} {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || raw != u.Scheme+"://"+u.Host || strings.ContainsAny(raw, "*;,| \t\r\n\"'\\") {
			return false
		}
		parsed = append(parsed, u)
	}
	return parsed[0].Scheme == parsed[1].Scheme && strings.EqualFold(parsed[0].Hostname(), parsed[1].Hostname()) && !sameOrigin(cockpit, stream)
}

func (s *Server) handleSignInInfo(w http.ResponseWriter, r *http.Request) {
	_, cockpit, stream, ok := s.ownedSignIn(r)
	w.Header().Set("Cache-Control", "no-store")
	if !ok || !sameOrigin(requestOrigin(r), cockpit) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]string{"cockpitOrigin": cockpit, "streamOrigin": stream})
}

func (s *Server) handleSignInCancel(w http.ResponseWriter, r *http.Request) {
	session, cockpit, _, ok := s.ownedSignIn(r)
	if !ok || !sameOrigin(requestOrigin(r), cockpit) {
		http.NotFound(w, r)
		return
	}
	id, _ := owner(r)
	if err := s.signInBrowser.Cancel(r.Context(), id, session); err != nil {
		http.Error(w, "could not close sign-in", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSignInProxy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", "sandbox; frame-ancestors 'none'")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	session, cockpit, stream, ok := s.ownedSignIn(r)
	if !ok || !sameOrigin(requestOrigin(r), stream) {
		http.NotFound(w, r)
		return
	}
	csp := "sandbox allow-scripts allow-same-origin; frame-ancestors " + cockpit + "; connect-src 'self'; form-action 'none'; base-uri 'self'"
	w.Header().Set("Content-Security-Policy", csp)
	if r.URL.Path == "/sign-in/"+session+"/.aura-ready" {
		if r.Method != http.MethodGet || r.Header.Get("Origin") != cockpit {
			http.Error(w, "sign-in origin refused", http.StatusForbidden)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", cockpit)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && r.Header.Get("Origin") != stream {
		http.Error(w, "sign-in origin refused", http.StatusForbidden)
		return
	}
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		if !s.signInBrowser.Owns(r.Context(), scopedIdentityID(r.Context()), session) {
			return nil, net.ErrClosed
		}
		return s.signInBrowser.Dial(ctx, session)
	}}
	defer transport.CloseIdleConnections()
	proxy := &httputil.ReverseProxy{FlushInterval: -1, Transport: transport, Rewrite: func(p *httputil.ProxyRequest) {
		p.Out.URL.Scheme = "http"
		p.Out.URL.Host = "127.0.0.1:3000"
		p.Out.Host = streamHost(stream)
		for h := range p.Out.Header {
			if privateSignInHeader(h) {
				p.Out.Header.Del(h)
			}
		}
	}, ModifyResponse: func(res *http.Response) error {
		for _, h := range []string{"Set-Cookie", "Set-Cookie2", "Clear-Site-Data", "Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Content-Security-Policy-Report-Only", "Refresh", "Reporting-Endpoints", "Report-To", "NEL"} {
			res.Header.Del(h)
		}
		res.Header.Set("Content-Security-Policy", csp)
		res.Header.Set("Cache-Control", "no-store")
		res.Header.Set("Referrer-Policy", "no-referrer")
		return nil
	}, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "sign-in stream unavailable", http.StatusBadGateway)
	}}
	proxy.ServeHTTP(w, r)
}

func privateSignInHeader(h string) bool {
	h = strings.ToLower(h)
	return h == "cookie" || h == "cookie2" || h == "authorization" || h == "proxy-authorization" || h == "forwarded" || strings.HasPrefix(h, "x-forwarded-") || strings.HasPrefix(h, "x-aura-") || h == "referer"
}

func streamHost(origin string) string { u, _ := url.Parse(origin); return u.Host }
