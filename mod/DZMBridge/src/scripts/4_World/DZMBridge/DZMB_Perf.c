// Server performance for DZ Manager: frame rate, uptime and how many players,
// infected, animals, items and vehicles exist. Sent once a minute as
// server.perf. Counts come from spawn/delete hooks (an increment each), never
// from scanning the world.

class DZMB_Perf
{
	static int s_Infected;
	static int s_Animals;
	static int s_Items;

	// Frame counting inside the current one-second sample.
	protected static int s_Frames;
	protected static float s_FrameTime;
	// Samples since the last report.
	protected static float s_Last;
	protected static float s_Min = -1;
	protected static float s_Sum;
	protected static int s_Samples;
	protected static float s_ReportAcc;

	static const float REPORT_SECONDS = 60;

	// Called every server frame with the frame's duration.
	static void Frame(float timeslice)
	{
		s_Frames++;
		s_FrameTime += timeslice;
		if (s_FrameTime < 1.0)
			return;
		float fps = s_Frames;
		fps = fps / s_FrameTime;
		s_Frames = 0;
		s_FrameTime = 0;
		s_Last = fps;
		s_Sum += fps;
		s_Samples++;
		if (s_Min < 0 || fps < s_Min)
			s_Min = fps;
	}

	// Called once a second by the bridge; emits a report every REPORT_SECONDS.
	static void Tick(DZMB_Bridge bridge, float dt)
	{
		s_ReportAcc += dt;
		if (s_ReportAcc < REPORT_SECONDS || s_Samples == 0)
			return;
		s_ReportAcc = 0;
		array<Man> players = new array<Man>;
		GetGame().GetPlayers(players);
		DZMB_Json d = new DZMB_Json();
		d.Num("fps", s_Last).Num("fps_avg", s_Sum / s_Samples).Num("fps_min", s_Min);
		d.IntV("uptime_seconds", GetGame().GetTime() / 1000);
		d.IntV("players", players.Count()).IntV("infected", s_Infected).IntV("animals", s_Animals);
		d.IntV("items", s_Items).IntV("vehicles", DZMB_Vehicles.s_All.Count());
		s_Min = -1;
		s_Sum = 0;
		s_Samples = 0;
		bridge.Emit("server.perf", "", "", d.End(), false, "0 0 0");
	}
}

modded class ZombieBase
{
	override void EEInit()
	{
		super.EEInit();
		if (GetGame().IsDedicatedServer())
			DZMB_Perf.s_Infected++;
	}

	override void EEDelete(EntityAI parent)
	{
		if (GetGame().IsDedicatedServer() && DZMB_Perf.s_Infected > 0)
			DZMB_Perf.s_Infected--;
		super.EEDelete(parent);
	}
}

modded class AnimalBase
{
	override void EEInit()
	{
		super.EEInit();
		if (GetGame().IsDedicatedServer())
			DZMB_Perf.s_Animals++;
	}

	override void EEDelete(EntityAI parent)
	{
		if (GetGame().IsDedicatedServer() && DZMB_Perf.s_Animals > 0)
			DZMB_Perf.s_Animals--;
		super.EEDelete(parent);
	}
}
