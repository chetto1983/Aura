package main

import (
	"context"
	"testing"

	authulaservices "github.com/Authula/authula/services"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agui"
)

func TestWireBootstrapServiceRequiresPoolAndAuthulaProvider(t *testing.T) {
	server := &fakeBootstrapServer{}
	pool := &pgxpool.Pool{}
	provider := fakeBootstrapProvider{core: &authulaservices.CoreServices{}}

	if !wireBootstrapService(server, pool, provider) {
		t.Fatal("wireBootstrapService returned false with pool and Authula provider")
	}
	if server.service == nil {
		t.Fatal("SetBootstrapService was not called")
	}

	for _, tc := range []struct {
		name     string
		pool     *pgxpool.Pool
		provider bootstrapProvider
	}{
		{name: "missing pool", provider: provider},
		{name: "missing provider", pool: pool},
		{name: "missing core", pool: pool, provider: fakeBootstrapProvider{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := &fakeBootstrapServer{}
			if wireBootstrapService(server, tc.pool, tc.provider) {
				t.Fatal("wireBootstrapService returned true for incomplete dependencies")
			}
			if server.service != nil {
				t.Fatal("SetBootstrapService was called for incomplete dependencies")
			}
		})
	}
}

func TestWireBootstrapServiceTreatsTypedNilProviderAsMissing(t *testing.T) {
	server := &fakeBootstrapServer{}
	pool := &pgxpool.Pool{}
	var provider *panicBootstrapProvider

	if wireBootstrapService(server, pool, provider) {
		t.Fatal("wireBootstrapService returned true for typed nil provider")
	}
	if server.service != nil {
		t.Fatal("SetBootstrapService was called for typed nil provider")
	}
}

type fakeBootstrapServer struct {
	service agui.BootstrapService
}

func (s *fakeBootstrapServer) SetBootstrapService(service agui.BootstrapService) {
	s.service = service
}

type fakeBootstrapProvider struct {
	core *authulaservices.CoreServices
}

func (p fakeBootstrapProvider) CoreServices() *authulaservices.CoreServices {
	return p.core
}

func (p fakeBootstrapProvider) OperatorUserID(context.Context) (string, error) {
	return "", nil
}

type panicBootstrapProvider struct{}

func (*panicBootstrapProvider) CoreServices() *authulaservices.CoreServices {
	panic("typed nil provider should be treated as missing")
}

func (*panicBootstrapProvider) OperatorUserID(context.Context) (string, error) {
	panic("typed nil provider should be treated as missing")
}
