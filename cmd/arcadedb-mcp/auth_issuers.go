package main

// The set of authorization servers this resource server trusts, and the identity
// each one's subjects map onto.
//
// Split from auth.go because it answers a different question. auth.go asks "is this
// token valid" — signature, audience, expiry. This file asks "whose memory is it",
// and the answer stopped being obvious the moment more than one issuer was allowed.
//
// The MCP specification (revision 2026-07-28, basic/authorization) is explicit that an
// MCP server is an OAuth *resource server* and that the authorization server "may be
// hosted with the resource server or a separate entity". Trusting exactly one issuer
// was therefore never a protocol requirement — it was this server assuming that
// everyone who talks to it is already a registered Aura user.

import (
	"strings"

	"github.com/google/uuid"
)

// trustedIssuer is one authorization server whose tokens are accepted, paired with
// the JWKS that verifies them. The two are separate values because they are not
// always the same host: Compose already runs a split horizon where the issuer is
// advertised as 127.0.0.1:9080 (the name a client on the host can reach) while the
// keys are fetched from aura:9080 (the name this container can reach).
type trustedIssuer struct {
	Issuer  string
	JWKSURL string
}

// defaultJWKSPath is where an issuer's keys live when the operator did not say.
// Aura's own authorization server serves them here; a foreign issuer that does not
// must be declared with an explicit `issuer=jwks_url` pair.
const defaultJWKSPath = "/oauth/jwks"

func newTrustedIssuer(issuer, jwksURL string) trustedIssuer {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	jwksURL = strings.TrimSpace(jwksURL)
	if jwksURL == "" {
		jwksURL = issuer + defaultJWKSPath
	}
	return trustedIssuer{Issuer: issuer, JWKSURL: jwksURL}
}

// parseTrustedIssuers reads MCP_OAUTH_TRUSTED_ISSUERS: a comma-separated list of
// `issuer` or `issuer=jwks_url` entries, naming authorization servers OTHER than the
// home one. Blank and malformed-empty entries are dropped rather than becoming an
// issuer named "" that no token can ever match.
//
// It is deliberately a second variable rather than a richer syntax on
// MCP_OAUTH_ISSUER: the home issuer is the one advertised first in the
// protected-resource metadata and the one whose subjects keep their database name
// unchanged, so the two lists mean genuinely different things.
func parseTrustedIssuers(raw string) []trustedIssuer {
	issuers := make([]trustedIssuer, 0, 2)
	for entry := range strings.SplitSeq(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		issuer, jwksURL, _ := strings.Cut(entry, "=")
		if strings.TrimSpace(issuer) == "" {
			continue
		}
		issuers = append(issuers, newTrustedIssuer(issuer, jwksURL))
	}
	return issuers
}

// homeIssuer is the first entry: the authorization server this deployment owns.
// Callers that need a single issuer name — the protected-resource metadata's
// canonical entry, the identity rule below — mean this one.
func (c oauthResourceConfig) homeIssuer() trustedIssuer {
	if len(c.Issuers) == 0 {
		return trustedIssuer{}
	}
	return c.Issuers[0]
}

// issuerNamed returns the trusted issuer a token claims to come from. Matching is
// exact: an issuer is a token's audience-of-audiences, and prefix or suffix
// tolerance here would let a lookalike host mint identities.
func (c oauthResourceConfig) issuerNamed(name string) (trustedIssuer, bool) {
	name = strings.TrimRight(strings.TrimSpace(name), "/")
	for _, candidate := range c.Issuers {
		if candidate.Issuer == name {
			return candidate, true
		}
	}
	return trustedIssuer{}, false
}

func (c oauthResourceConfig) issuerNames() []string {
	names := make([]string, 0, len(c.Issuers))
	for _, issuer := range c.Issuers {
		names = append(names, issuer.Issuer)
	}
	return names
}

// foreignIdentityNamespace anchors the UUIDv5 derivation below. It is computed from a
// fixed URL rather than written as a literal so the value is reproducible by reading
// this line, and so nobody has to trust a magic constant nobody can re-derive.
var foreignIdentityNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://aura.local/mcp/foreign-identity"))

// tenantIdentity maps an authenticated (issuer, subject) pair onto the identity whose
// memory the caller gets.
//
// RFC 7519 §4.1.2 guarantees `sub` is unique only WITHIN one issuer's namespace. The
// moment a second issuer is trusted, keying on `sub` alone is not merely untidy: a
// numeric Google subject and an Aura identity UUID are drawn from different spaces
// and nothing stops one from colliding with — or worse, silently landing on —
// another issuer's tenant. So the issuer is part of the key.
//
// The home issuer is the exception and it is a deliberate one: its subjects ARE Aura
// identity UUIDs, arcadedb.DatabaseFor already parses them as such, and every existing
// mem_<uuid> database is named after one. Passing them through verbatim keeps that
// mapping byte-identical, so widening the trusted set migrates nothing.
//
// Foreign subjects are folded into a UUIDv5 because the tenant machinery — DatabaseFor,
// TenantUserFor, the derived per-tenant credential — is specified over UUIDs. A name-based
// UUID is deterministic (the same person returns to the same memory), collision-resistant,
// and needs no registry to translate it back.
func (c oauthResourceConfig) tenantIdentity(issuer, subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return ""
	}
	if home := c.homeIssuer(); home.Issuer != "" && strings.TrimRight(strings.TrimSpace(issuer), "/") == home.Issuer {
		return subject
	}
	// The separator is a newline because it cannot appear in either half, so no pair
	// of (issuer, subject) values can be re-split into a different pair.
	return uuid.NewSHA1(foreignIdentityNamespace, []byte(issuer+"\n"+subject)).String()
}
