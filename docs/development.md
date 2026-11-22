# Development

Requirements: Go (the toolchain in `go.mod` downloads automatically), Node 22, Python 3 (PBO packer).
On Windows, use WSL or Git Bash for the shell commands below.

```bash
npm ci --prefix web && npm --prefix web test && npm --prefix web run build   # UI → internal/web/dist
go vet ./... && go test ./...                                                # includes a real-binary smoke test
bash mod/build.sh dev                                                        # dist/DZMBridge.zip
```

## Run locally
```bash
export DZM_DATA_DIR=$PWD/.data DZM_SECRET_KEY=$(head -c32 /dev/urandom | base64) \
       DZM_PUBLIC_URL=http://localhost:8080 DZM_INGEST_PUBLIC_URL=http://localhost:8080
mkdir -p .data && go run ./cmd/dzm serve
go run ./cmd/dzm admin invite-owner   # in another shell: open the link, set a password and optionally enable 2FA
```
Plain `http://` is accepted only for loopback URLs (tests and dev). Production URLs must be https.
To try single sign-on, `go run ./cmd/dzm-devoidc` is a fake OpenID Connect provider (development
only, never in a release); add the `DZM_OIDC_*` values it prints, then link it under Account.

## Layout
| Path | What |
|---|---|
| `cmd/dzm` | the binary (serve, version, check-config, health, map, admin invite-owner) |
| `internal/auth` | accounts: password/Steam/OIDC sign-in, TOTP 2FA, invites, sessions, CSRF |
| `internal/perm` | roles and the permission of every API route (deny by default) |
| `internal/profiles` | server profiles + connect wizard |
| `internal/rcon` | RCON + A2S connector |
| `internal/metricz` | live map state, performance history (from @DZMBridge, or the optional MetricZ receiver) |
| `internal/restarts` | restart schedule, countdowns and restart detection |
| `internal/publicapi`, `internal/mcp` | public API and MCP server with scoped keys |
| `internal/bridge` | ingest v1 + command queue |
| `internal/players` | identities, sessions, combat |
| `internal/actions` | mod-backed player actions |
| `internal/mapevents` | world events + rare items |
| `internal/bans` | bans + external lists |
| `web/` | React UI |
| `mod/DZMBridge` | server-side PBO sources |
| `deploy/` | Linux installer, updater, backup and systemd units, Docker Compose |
