package mcp

import (
	"crypto/subtle"
	"errors"
	"net/url"
)

// ErrPasteMismatch deliberately omits the pasted URL, which carries a code and state.
var ErrPasteMismatch = errors.New("this address does not match the pending sign-in; copy the full return address and try again")

// Paste accepts any return host: state, not a caller-supplied hostname, proves the attempt.
func (f *Flows) Paste(owner, id, address string) (Flow, error) {
	f.mu.Lock()
	f.sweepLocked()
	flow, ok := f.byID[id]
	if !ok || flow.owner != owner {
		f.mu.Unlock()
		return Flow{}, ErrNoSuchFlow
	}
	if flow.Status != FlowAuthorizationRequired || f.byState[flow.state] != flow {
		f.mu.Unlock()
		return Flow{}, ErrFlowNotWaiting
	}
	state := flow.state
	f.mu.Unlock()
	if len(address) > 16*1024 {
		return Flow{}, ErrPasteMismatch
	}
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Flow{}, ErrPasteMismatch
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(state)) != 1 {
		return Flow{}, ErrPasteMismatch
	}
	if refused := query.Get("error"); refused != "" {
		return f.Fail(state, errors.New(refused+": "+query.Get("error_description")))
	}
	if query.Get("code") == "" {
		return Flow{}, ErrPasteMismatch
	}
	return f.Complete(state, query.Get("code"), query.Get("iss"))
}

// Cancel removes the state mapping before releasing the fetcher, preventing stale consent.
func (f *Flows) Cancel(owner, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sweepLocked()
	flow, ok := f.byID[id]
	if !ok || flow.owner != owner {
		return ErrNoSuchFlow
	}
	f.forgetLocked(flow)
	return nil
}
