# Map tracking

## Live positions

The bridge pushes players and changed vehicles every `live_seconds` (default 2),
with a complete vehicle list every 30 seconds. Browsers receive incremental live
messages. While these are fresh, the bridge is authoritative for positions and
the vehicle list. MetricZ adds details such as ping and kills and takes over if
the bridge is quiet for 15 seconds. The map badge shows `live`, `slow` or `stale`.
Only vehicles that exist in the world appear; spawning depends on the mission.
Markers update in place, and open details follow the latest data.

## Event lifetimes

Markers without `ttl_s` need regular seen messages. After `stale_seconds`
(Settings → Map & tracking, default 120) without a ping, the marker expires.
A later ping revives it. The panel uses receipt time so game-server clock drift
cannot keep a ghost marker alive. Deleted objects end on reconciliation.
Deaths and raids have their own lifetimes. Ended events remain available in
History and through `GET /api/map/event?key=…` and `#/map/event/<key>`.

## Icons and layers

Players use green dots; vehicles and world entities have distinct SVG icons.
Legend buttons filter categories independently. Items and base flags have their
own switches. Nearby scene objects can share a numbered icon whose details list
the observed entities. Counts do not infer the number of economy event groups.
Item labels use classnames. Optional integrations can supply a friendly name,
group tag and territory radius. Labels are rendered as text, never HTML.

## Ground items

Configured items are tracked only while they have no inventory parent. Drop and
pickup hooks start and end markers; each later drop gets a new lifecycle key.
Spawn and first-observation history is separate and hidden until History is enabled.
Saving tracking settings requests a new census through the command poll, without
a restart, even if the class list is unchanged. World censuses run after startup
or settings changes, with a one-minute minimum interval. They process bounded
batches between server frames so telemetry and command polling can continue.
Normal refreshes use the tracked-entity registry. Inheritance matches are cached
by classname and invalidated when settings change.

Deaths remain live for one minute and raids for 30 minutes after the last hit.
Both remain in History. Hidden active events offer a “Show all” action.
