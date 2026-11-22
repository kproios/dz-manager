# Game server setup

Everything on the game server is **server-side only**. Players never download anything, and none
of this goes into the normal `-mod=` list.

## 1. Install @DZMBridge

@DZMBridge is a small server-side mod with no dependencies. It sends live positions, events and
performance to DZ Manager and carries out the actions you click in the panel.

1. **Get the mod.** Subscribe to "DZ Manager Bridge" on the Steam Workshop, or download
   `DZMBridge.zip` from the [latest release](../../../releases/latest) and check it against
   `SHA256SUMS` from the same release.
2. **Upload it.** Put the `@DZMBridge` folder next to the server executable, with your host's
   file manager or by FTP/SFTP.
3. **Load it as a server-side mod.** Add it to the server mods launch parameter:
   `-servermod=@DZMBridge` (game hosts usually have a "server-side mods" field for this). It is
   deliberately unsigned: server-side mods need no `.bisign`, and no key goes into `keys/`.
4. **Start the server once.** The mod creates `profiles/DZMBridge/config.json` and the server log
   shows `[DZMBridge] disabled: paste the config …`.
5. **Paste the configuration.** In DZ Manager open **Settings → Server**, click **Connect…** on
   your server and copy the bridge configuration over `profiles/DZMBridge/config.json`. It
   contains the ingest address and this server's own credentials. Treat it like a password.
6. **Restart the game server.**

**Check:** **Settings → Server** shows the installed bridge version and a recent heartbeat, the
dashboard shows the server's FPS and uptime, and **Settings → Map & tracking** says the map was
reported by the game server. If not, see [Troubleshooting](troubleshooting.md#the-bridge-never-connects).

### Working with other mods

The bridge only adds to the game's own classes and works next to other mods. When LBmaster mods
are loaded it also uses their notifications, global ban list, territory flags and code lock
raids. To switch that off, set `"disable_lbmaster": true` in its configuration.

### Updating and removing

DZ Manager updates itself; the game-side mod does not. **Settings → Server** shows the installed
version. When a release says the bridge changed, replace the `@DZMBridge` folder while the server
is stopped (a running server locks its mod files) and start it again.

To remove it, take `@DZMBridge` out of the server mods and restart. The mod changes nothing else
on the server; it only writes its own `profiles/DZMBridge/config.json`.

## 2. MetricZ (optional)

The bridge already reports server performance. [MetricZ](https://github.com/WoozyMasta/metricz) is
only useful if you also export its metrics to other tools, or run an old bridge version. DZ Manager
accepts its data, and prefers the bridge's when both arrive. To use it: install it as a
server-side mod from its own release or the Steam Workshop, start once, merge the MetricZ block
from **Connect…** into `profiles/metricz/config.json`, and restart.

## 3. Map image (in DZ Manager, once per map)

**Settings → Map & tracking → Map image:** pick the map and a size, then **Download and install**.
DZ Manager downloads a terrain image from [tools.dzhosts.com/maps](https://tools.dzhosts.com/maps/)
(rendered from the game's own terrain by [dayz-terrain-map](https://github.com/jmhobbs/dayz-terrain-map)),
cuts it into map tiles and deletes the download. 7,000–8,000 pixels is plenty and takes a few
seconds.

The map's name, size and place names come from the game server through the bridge, so official
and Workshop maps both work. Until the bridge has connected you can choose the map by hand in the
same card. Markers work without an image; it is only the background.

From the command line, also for maps that are not listed (with an image you rendered yourself):

```bash
dzm map list
dzm map install --world chernarusplus
dzm map install --world enoch --slug livonia --px 7616
dzm map install --world namalsk --file namalsk.png
```

Run these with the same settings as the panel: on Docker prefix them with
`docker compose exec dzm`, on a Linux install run them through
`systemd-run --pipe --wait -p User=dzm -p EnvironmentFile=/etc/dzm/dzm.env /opt/dzm/current/dzm …`.
`DZM_MAP_MAX_PX` limits the largest image (see [Configuration](configuration.md)).

## Game host requirements

Most game hosts support everything the bridge needs: server-side mods, editing files under
`profiles/`, and outgoing HTTPS from the game server. See [Game hosts](providers.md) for the
full list and what to check when moving between hosts.
