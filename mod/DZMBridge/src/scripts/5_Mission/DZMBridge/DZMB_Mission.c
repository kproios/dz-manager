// MissionServer integration: start, tick (throttled to 1 s), connect/disconnect.
// We extend MissionServer instead of replacing it so other mods keep working.

modded class MissionServer
{
	protected float m_DZMB_Acc;

	override void OnInit()
	{
		super.OnInit();
		DZMB_Bridge.Start();
	}

	override void OnUpdate(float timeslice)
	{
		super.OnUpdate(timeslice);
		DZMB_Bridge b = DZMB_Bridge.Get();
		// The world census runs in small slices every frame; everything else once a second.
		if (b)
		{
			b.ContinueTrackingScan();
			DZMB_Perf.Frame(timeslice);
		}
		m_DZMB_Acc += timeslice;
		if (m_DZMB_Acc < 1.0)
			return;
		if (b)
			b.OnUpdate(m_DZMB_Acc);
		m_DZMB_Acc = 0;
	}

	override void InvokeOnConnect(PlayerBase player, PlayerIdentity identity)
	{
		super.InvokeOnConnect(player, identity);
		DZMB_Bridge b = DZMB_Bridge.Get();
		if (b)
			b.OnConnect(player, identity);
	}

	// PlayerDisconnected is the reliable hook: InvokeOnDisconnect only runs while
	// the character still exists (Takaro integration finding, MIT).
	override void PlayerDisconnected(PlayerBase player, PlayerIdentity identity, string uid)
	{
		DZMB_Bridge b = DZMB_Bridge.Get();
		if (b)
			b.OnDisconnect(player, identity, uid);
		super.PlayerDisconnected(player, identity, uid);
	}
}
