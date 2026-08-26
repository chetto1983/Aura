package mcp

// Aura-owned HTTP MCPs use the same OAuth contract as arbitrary remote MCPs.
// The issuer is loopback because the generic client runs in the Aura process;
// resource servers fetch the same issuer's keys over the private Compose name.
const (
	AuraAuthorizationServerIssuer  = "http://127.0.0.1:9080"
	AuraAuthorizationServerJWKSURL = "http://aura:9080/oauth/jwks"
	AuraOAuthToolsScope            = "mcp:tools"
	AuraOAuthManageScope           = "mcp:manage"
)
