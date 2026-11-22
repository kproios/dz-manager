# Hosting options

DZ Manager needs two things from the place it runs:

1. **It must stay on.** It records events, watches restarts and serves the panel all the time.
   Services that put apps to sleep when nobody visits do not work.
2. **It must be reachable over HTTPS** at a fixed address, because the game server sends its data
   there and browsers need HTTPS to sign in.

It also keeps its data in a local SQLite database, so it needs a real disk that survives
restarts. 1 CPU, 1 GB of memory and a few GB of disk are plenty for one game server.

## Where to run it

| Option | Cost | Good for | Notes |
|---|---|---|---|
| A computer at home (old PC, mini PC, NAS with Docker, Raspberry Pi 4/5 64-bit) | Free if you already have one | Most people | Pair it with a tunnel (below) so nothing needs to be opened on your router. |
| Your own desktop PC with Docker Desktop | Free | Trying it out | Only works while the PC is on. Fine for testing, not for a live server. |
| An "always free" cloud VM | Free within the provider's limits | Running without hardware at home | Several large providers offer a small VM at no cost, for example Oracle Cloud Free Tier or the Google Cloud free tier. Check their current terms; they change, and some need a payment card for verification. |
| A low-cost VPS | A few euros or dollars per month | A simple, dedicated setup | Any provider that gives you a small Linux VM works. |
| The same machine as the game server | Free if you run the game server yourself | Self-hosted game servers | Keep the panel on its own user or container. Rented game servers usually do not allow this. |

DZ Manager is published for Linux on **x86-64 (amd64)** and **ARM64 (aarch64)**, so ARM machines
such as the Raspberry Pi and ARM cloud VMs work too. On Windows and macOS, run it in Docker.

Not suitable: free "app platforms" that sleep after inactivity or wipe the disk on every deploy.

## Getting HTTPS

Pick one. All of them are free.

### Cloudflare Tunnel (recommended)

The tunnel program makes an outgoing connection to Cloudflare, and Cloudflare serves your panel
at your own domain with a valid certificate. Nothing is opened on your router or firewall, and it
works behind home internet connections that do not allow incoming traffic.

- You need a domain whose DNS is managed by Cloudflare (adding a domain to Cloudflare's free plan
  is free; the domain itself is bought from any registrar, often for a few euros per year).
- Step-by-step setup: [HTTPS with Cloudflare Tunnel](../deploy/cloudflared/README.md).
- The Linux installer already installs the tunnel program. It leaves your SSH settings unchanged,
  so it is safe on a cloud VM too.

Do not use the temporary "quick tunnel" addresses for a live setup: they change every time the
tunnel restarts, and the game server's configuration would break.

### Tailscale Funnel

[Tailscale Funnel](https://tailscale.com/kb/1223/funnel) publishes a local service at a fixed
`https://<machine>.<tailnet>.ts.net` address, with no domain to buy. Install Tailscale on the
DZ Manager machine, enable Funnel for port 8080, and use that address for both
`DZM_PUBLIC_URL` and `DZM_INGEST_PUBLIC_URL`. Free for personal use; check Tailscale's current
plan limits.

### A reverse proxy with automatic certificates

If the machine has a public IP address and you can open ports 80 and 443, a reverse proxy such as
[Caddy](https://caddyserver.com/) gets free certificates from Let's Encrypt automatically:

```
dzm.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

No domain? A free dynamic-DNS name (for example from [DuckDNS](https://www.duckdns.org/)) also
works with Caddy. nginx or Traefik with Let's Encrypt work the same way.

## One address or two?

DZ Manager can use one address for everything, or a separate **ingest address** that only accepts
game-server traffic (`/ingest/…`). Two addresses let you protect the panel more strictly (for
example with extra access rules) without blocking the game server. One address is simpler and
perfectly fine to start with.

## Things that can block the game server

The game server talks to DZ Manager as a program, not a browser. It cannot solve a "checking your
browser" page, a CAPTCHA or a login wall. If you put security rules, bot protection or access
policies in front of DZ Manager, exempt the ingest paths (`/ingest/…`). See
[Troubleshooting](troubleshooting.md#the-bridge-never-connects).
