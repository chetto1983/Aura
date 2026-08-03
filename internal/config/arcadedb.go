package config

import "os"

// ArcadeDBConfig addresses the memory server.
//
// cmd/aura uses the ordinary server address for the cockpit's bounded read-only
// memory graph. It also uses the separate admin credential for de-provisioning:
// memory is one ArcadeDB database and one server user per identity
// (internal/arcadedb/tenant.go), so "forget this person" is `drop database` +
// `drop user`, both server commands.
//
// The env names are the sidecar's, deliberately: the two composition roots must
// name the same server, and a second spelling would let them drift onto
// different ones.
//
// The type lives here rather than in internal/arcadedb because config is the
// thin root every subcommand reads: importing that package would drag the
// document and object-store stacks behind it into `aura db migrate`.
type ArcadeDBConfig struct {
	BaseURL  string // ARCADEDB_URL
	Database string // ARCADEDB_DATABASE — the SHARED database; per-identity memory lives in mem_<uuid>
	// User/Password is the ordinary credential on the shared database. It carries
	// the adaptive-learning projection, which is Aura's own plane and is scoped by
	// an owner_id property rather than by a database.
	User     string // ARCADEDB_USER
	Password string // ARCADEDB_PASSWORD
	// AdminUser/AdminPassword carries server rights: create/drop database,
	// create/drop user. Kept apart from the pair above for the same reason
	// AURA_DB_URL is kept apart from AURA_DB_MIGRATE_URL — the credential that
	// reads must not be able to drop.
	AdminUser     string // ARCADEDB_ADMIN_USER
	AdminPassword string // ARCADEDB_ADMIN_PASSWORD
}

// loadArcadeDB reads the memory-server wiring. Every field may be empty: a
// deploy with no ArcadeDB configured builds NO memory purger, which the
// de-provisioning preflight then refuses to work around.
func loadArcadeDB() ArcadeDBConfig {
	return ArcadeDBConfig{
		BaseURL:       envDefault("ARCADEDB_URL", "http://127.0.0.1:2480"),
		Database:      envDefault("ARCADEDB_DATABASE", "aura_memory"),
		User:          os.Getenv("ARCADEDB_USER"),
		Password:      os.Getenv("ARCADEDB_PASSWORD"),
		AdminUser:     os.Getenv("ARCADEDB_ADMIN_USER"),
		AdminPassword: os.Getenv("ARCADEDB_ADMIN_PASSWORD"),
	}
}
