# Development

Requirements: Go (the toolchain in `go.mod` downloads automatically), Node 22, Python 3 (PBO packer).

```bash
npm ci --prefix web && npm --prefix web test && npm --prefix web run build   # UI → internal/web/dist
go vet ./... && go test ./...                                                # includes a real-binary smoke test
bash mod/build.sh dev                                                        # dist/DZMBridge.zip
```

## Run locally without SSO
`cmd/dzm-devoidc` is a fake SSO for development only. It is not part of any release, and the
production binary has no dev-login path.
```bash
go run ./cmd/dzm-devoidc            # prints DZM_OIDC_* values; keep it running
export DZM_DATA_DIR=$PWD/.data DZM_SECRET_KEY=$(head -c32 /dev/urandom | base64) \
       DZM_PUBLIC_URL=http://127.0.0.1:8080 DZM_INGEST_PUBLIC_URL=http://127.0.0.1:8080 \
       DZM_OIDC_ISSUER=… DZM_OIDC_CLIENT_ID=dzm-test DZM_OIDC_CLIENT_SECRET=s3cret
mkdir -p .data && go run ./cmd/dzm serve
```
Plain `http://` is accepted only for loopback URLs (tests and dev). Production URLs must be https.

## Layout
| Path | What |
|---|---|
| `cmd/dzm` | the binary (serve, version, check-config) |
| `internal/auth` | SSO OIDC login, sessions, CSRF |
| `internal/profiles` | server profiles + connect wizard (#20) |
| `internal/rcon` | RCON + A2S connector (#15) |
| `internal/metricz` | MetricZ receiver, live map state, perf (#5) |
| `internal/restarts` | restart observation (#4) |
| `internal/bridge` | ingest v1 + command queue (#16) |
| `internal/players` | identities, sessions, combat (#11) |
| `internal/actions` | mod-backed player actions (#7) |
| `internal/mapevents` | world events + rare items (#6) |
| `internal/bans` | bans + external lists (#10) |
| `web/` | React UI |
| `mod/DZMBridge` | server-side PBO sources |
