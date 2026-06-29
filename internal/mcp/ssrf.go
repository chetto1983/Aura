package mcp

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
)

// RED stub — real implementation lands in the GREEN commit. Signatures only so the
// table tests in ssrf_test.go compile and fail at runtime (TDD red).

func classify(ip netip.Addr) (reason string, blocked bool) {
	_ = ip
	return "", false
}

type resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

func guardEndpoint(_ context.Context, _ string, _ bool, _ map[string]struct{}, _ resolver) (*url.URL, error) {
	return nil, errors.New("ssrf: guard not implemented")
}
