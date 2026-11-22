# HTTPS with Cloudflare Tunnel

A Cloudflare Tunnel publishes DZ Manager at your own domain with a valid certificate, without
opening any port on your router or firewall. The tunnel program on your machine connects out to
Cloudflare; visitors and the game server connect to Cloudflare, which passes the traffic through.
It is available on Cloudflare's free plan.

## What you need

- A free Cloudflare account
- A domain added to Cloudflare (its DNS managed by Cloudflare)
- The machine running DZ Manager

## 1. Create the tunnel

1. In the Cloudflare dashboard open **Zero Trust → Networks → Tunnels** (the exact menu names may
   change; look for "Tunnels") and choose **Create a tunnel**, type **Cloudflared**.
2. Give it a name, for example `dz-manager`.
3. Cloudflare shows an install command containing a long **token**. Copy only the token.

## 2. Run the tunnel program

**Linux install** (the installer already installed `cloudflared` and a service for it). As root:

```bash
install -m 0600 /dev/null /etc/dzm/cloudflared-token
nano /etc/dzm/cloudflared-token           # paste the token, save
systemctl enable --now cloudflared.service
```

**Docker:** add the `tunnel` service shown in [Install with Docker](../../docs/docker.md#running-the-cloudflare-tunnel-in-docker-too).

**Anything else:** install `cloudflared` from
[Cloudflare's downloads](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)
and run the command the dashboard showed.

The tunnel shows as **Healthy** in the dashboard within a minute.

## 3. Add the public hostnames

In the tunnel's **Public Hostname** tab:

| Hostname | Service |
|---|---|
| `dzm.example.com` (the panel) | `http://127.0.0.1:8080` (Docker with the tunnel service: `http://dzm:8080`) |
| `dzm-ingest.example.com` (optional, game server only) | the same service, with path `ingest/*` |

For a separate ingest hostname, add a second rule for the same hostname without a path that
returns **HTTP 404**, so that hostname serves nothing but `/ingest/…`. If you use one hostname for
everything, skip the second row.

Set the same addresses in the DZ Manager settings (`DZM_PUBLIC_URL`, `DZM_INGEST_PUBLIC_URL`) and
restart it.

## 4. Let the game server through

The game server is a program, not a browser: it cannot answer "checking your browser" pages,
CAPTCHAs or login prompts. If you enable any of these for your domain (Bot Fight Mode, WAF
challenge rules, Cloudflare Access), add an exception that skips them for the ingest paths
(`/ingest/*`). Those paths are protected by DZ Manager's own per-server credentials.

You can additionally allow only your game server's public IP address on the ingest hostname, if
your game host tells you which address its server uses for outgoing traffic.

## 5. Check

- In a browser: `https://dzm.example.com/ready` shows `"status":"ready"`.
- From any machine: `curl -i -X POST https://dzm-ingest.example.com/ingest/ping` answers
  `401` with a short JSON error from DZ Manager (it wants the game server's credentials). An HTML
  page or a `403` from Cloudflare means a security rule is still in the way.

Keep the tunnel token private. Anyone with it can run your tunnel.
