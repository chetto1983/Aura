# 17-07 Distribution Surface Summary

## Result

Completed the install and release distribution surface for the Aura appliance.

- Added `scripts/install.sh` as an executable Linux/macOS installer with hardware preflight checks, Docker install guidance, idempotent `.env` secret generation, optional `--appliance`, optional `--gvisor`, and a token-prefilled setup wizard URL.
- Added `deploy/aura.service` for native Linux appliance installs under `/opt/aura`, including documented gVisor provisioning steps and a compose-based systemd lifecycle.
- Extended `.goreleaser.yaml` with GoReleaser v2 `dockers_v2` publishing for `ghcr.io/chetto1983/aura:{{ .Tag }}` on `linux/amd64` and `linux/arm64`, deliberately omitting `latest`.
- Added artifact regression coverage for the installer, service unit, and release-image contract.
- Installed GoReleaser locally with Winget and verified `GitVersion: 2.16.0`.

## Decisions

- Existing `.env` files are preserved. The installer validates required keys and fixes permissions, but does not regenerate or rewrite existing secrets.
- gVisor is opt-in and native-Linux-only. The installer rejects Docker Desktop for `--gvisor` and writes a systemd drop-in only when the gVisor path is requested.
- GoReleaser uses `dockers_v2` with `extra_files` for `go.mod`, `go.sum`, `cmd`, and `internal` because the existing `docker/aura/Dockerfile` builds from source inside the Docker context.
- The GoReleaser `go mod tidy` before hook added missing `go.sum` entries during the verified snapshot build; those checksum updates were included so release builds start clean.

## Verification

- `bash -n scripts/install.sh`
- `goreleaser check`
- `goreleaser build --snapshot --clean`
- `go test ./cmd/aura -run "Test(ProductionContainerArtifactsMatchFatImageContract|DistributionSurfaceArtifactsMatchReleaseContract)" -v`
- `go test ./cmd/aura -v`
- `go vet ./cmd/aura`
- `go build ./...`
- `bash scripts/check-file-size.sh`
- Static greps for installer secrets/Docker/setup URL, GoReleaser image platforms, and systemd/gVisor unit content.

## Follow-Up

- Real multi-arch image publishing still needs a release tag and CI environment with Docker buildx/QEMU configured.
