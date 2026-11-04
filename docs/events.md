# World events and tracked items

Bridge applies Settings → Event & rare-item tracking on its next outbound command poll. No game-host API, SSH daemon integration or new inbound port is required. The latest heartbeat reports unknown configured classnames in Settings.

| Source | Behavior |
| --- | --- |
| Vanilla helicopter wrecks | `Wreck_UH1Y` and `Wreck_Mi8_Crashed`, including configured descendants; static `StaticObj_` scenery is excluded from inherited matches. |
| Custom events / loot containers | Configured entity class → category; ItemBase, Weapon_Base and House init/delete hooks plus an initial reconciliation of actual existing entities. No possible spawn-point coordinates are used. |
| CE-created items | Exact allow-listed item classes emit a delayed `item.spawn` after placement; ItemBase and Weapon_Base are covered. |
| Items already on the ground | An allow-list change or startup reconciliation records `item.observed`. The UI says “First observed”, never invents a CE spawn time. These remain historical observations, not inventory tracking. |
| Unconfigured / unknown classes | No markers are invented; the heartbeat identifies unknown configured classes. |

A single world census after CE startup, or following an operator's tracking change, seeds the already-running world. This deliberate exception to hooks-only initial population makes existing events visible when installing the bridge. It never runs per frame or periodically across the world. Between censuses, init/delete hooks and a small registry refresh maintain configured event entities; a 60-second seen message refreshes their TTL and position, and ends carried custom-item events. Routine seen messages and heartbeats are hidden from activity history by default. End tombstones remain authoritative for out-of-order updates.

Map coordinates are DayZ east/north metres. The MetricZ wizard sets `geo.disable_transform_coordinates=true` and enables height export. The receiver also accepts horizontal X/Z without altitude, matching upstream's default disabled height metrics; repeated X samples cannot stand in for missing Z.

The server-side PBO remains separate from upstream MetricZ, which is unmodified. No AI/infected markers or continuous player-location history are stored.
