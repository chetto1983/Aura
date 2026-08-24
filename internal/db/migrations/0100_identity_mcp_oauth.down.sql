-- Drop the per-identity MCP OAuth grants.
--
-- Rolling back costs exactly the stored grants: every remote MCP server whose token lived
-- here has to be authorized again through the browser flow. It is fail-closed -- a missing
-- row means "no token", which triggers the authorization flow rather than mounting the
-- server unauthenticated. The operator's own pre-registered client credentials are NOT in
-- this table (they ride ManagedServer.Env), so nothing an operator configured is lost.
--
-- DROP TABLE removes the two RLS policies with it; naming them separately would fail on a
-- database where the table is already gone.

DROP TABLE IF EXISTS aura.identity_mcp_oauth;
