package release

// Version, Commit, and BuildDate are injected at link time via -ldflags:
//
//	-X github.com/aura/aura/internal/release.Commit=$(git rev-parse --short HEAD)
//	-X github.com/aura/aura/internal/release.BuildDate=$(date -u +%FT%TZ)
//
// Without injection the defaults below are used (dev builds, unit tests).
var (
	Version   = "dev"
	Commit    = "dev"
	BuildDate = "unknown"
)
