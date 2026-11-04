# HTTPS tunnel setup

Create a tunnel in your own Cloudflare account and point your panel and optional
ingest DNS names at it. Store the tunnel token in `/etc/dzm/cloudflared-token`
with root ownership and mode 0600; the supplied systemd unit reads it there.

Route the panel hostname to `http://127.0.0.1:8080`. For a separate ingest hostname,
route only `/ingest/*` to that service and return 404 for other paths. Set
`DZM_PUBLIC_URL` and `DZM_INGEST_PUBLIC_URL` to your HTTPS names.

Game-server requests cannot answer browser challenges. Configure applicable
security rules to allow the authenticated ingest paths without interactive
verification. An optional source IP allowlist must use the game server's public
egress address. Test `/ingest/ping` from the game host before enabling telemetry.
Never commit tunnel tokens, account IDs or deployment-specific routing exports.
