// DZMBridge core: config, rate-limited log, UTC time, JSON writer, HTTP.
// HTTP wrapper adapted from DayZ-Takaro-Integration (MIT). DayZ RestContext
// cannot send custom headers reliably, so the server token travels in the URL path.

const string DZMB_VERSION = "0.2.0";
const string DZMB_DIR = "$profile:DZMBridge";
const string DZMB_CONFIG = "$profile:DZMBridge/config.json";

class DZMB_ConfigData
{
	int version = 1;
	bool enabled = false;
	string ingest_url = "";
	string server_id = "";
	string server_token = "";
	int event_flush_seconds = 5;
	int command_poll_seconds = 3;
	int heartbeat_seconds = 30;
	int live_seconds = 2;
	int max_queue = 500;
	string log_level = "info";
	// LBmaster's notifications, banlist, territory flags and code lock raids
	// are used when those mods are loaded. Set true to ignore LBmaster. (A
	// "disable" flag, because a field missing from an older config.json loads
	// as false.)
	bool disable_lbmaster = false;
	ref map<string, string> event_classes = new map<string, string>;
	ref array<string> rare_items = new array<string>;
}

class DZMB_Config
{
	static ref DZMB_ConfigData s_Data;

	static DZMB_ConfigData Get()
	{
		if (!s_Data)
			Load();
		return s_Data;
	}

	static void Load()
	{
		s_Data = new DZMB_ConfigData();
		if (!FileExist(DZMB_DIR))
			MakeDirectory(DZMB_DIR);
		if (FileExist(DZMB_CONFIG))
		{
			JsonFileLoader<DZMB_ConfigData>.JsonLoadFile(DZMB_CONFIG, s_Data);
		}
		else
		{
			// Disabled defaults: the owner pastes the generated config from DZ Manager.
			JsonFileLoader<DZMB_ConfigData>.JsonSaveFile(DZMB_CONFIG, s_Data);
		}
		if (!s_Data.event_classes)
			s_Data.event_classes = new map<string, string>;
		if (!s_Data.rare_items)
			s_Data.rare_items = new array<string>;
		if (s_Data.event_flush_seconds < 1) s_Data.event_flush_seconds = 5;
		if (s_Data.command_poll_seconds < 1) s_Data.command_poll_seconds = 3;
		if (s_Data.heartbeat_seconds < 5) s_Data.heartbeat_seconds = 30;
		if (s_Data.live_seconds < 1) s_Data.live_seconds = 2;
		if (s_Data.max_queue < 10) s_Data.max_queue = 500;
	}

	static bool Ready()
	{
		DZMB_ConfigData d = Get();
		return d.enabled && d.ingest_url != "" && d.server_token != "";
	}
}

class DZMB_Log
{
	static int s_LastWarn;

	static void Info(string msg)
	{
		Print("[DZMBridge] " + msg);
	}

	static void Debug(string msg)
	{
		if (DZMB_Config.Get().log_level == "debug")
			Print("[DZMBridge][debug] " + msg);
	}

	// At most one warning per minute so an offline backend never floods the RPT.
	static void Warn(string msg)
	{
		int now = GetGame().GetTime();
		if (s_LastWarn != 0 && now - s_LastWarn < 60000)
			return;
		s_LastWarn = now;
		Print("[DZMBridge][warn] " + msg);
	}
}

class DZMB_Time
{
	static string Pad2(int v)
	{
		if (v < 10)
			return "0" + v.ToString();
		return v.ToString();
	}

	// RFC3339 UTC timestamp, e.g. 2026-11-04T12:00:00Z
	static string NowUTC()
	{
		int y, mo, d, h, mi, s;
		GetYearMonthDayUTC(y, mo, d);
		GetHourMinuteSecondUTC(h, mi, s);
		return y.ToString() + "-" + Pad2(mo) + "-" + Pad2(d) + "T" + Pad2(h) + ":" + Pad2(mi) + ":" + Pad2(s) + "Z";
	}

	// Compact boot identifier used to keep event ids unique across restarts.
	static string BootId()
	{
		int y, mo, d, h, mi, s;
		GetYearMonthDayUTC(y, mo, d);
		GetHourMinuteSecondUTC(h, mi, s);
		return y.ToString() + Pad2(mo) + Pad2(d) + Pad2(h) + Pad2(mi) + Pad2(s) + "-" + Math.RandomInt(1000, 9999).ToString();
	}
}

// Minimal JSON object writer (no reflection, predictable output).
class DZMB_Json
{
	protected string m_Out;
	protected bool m_First;

	void DZMB_Json()
	{
		m_Out = "{";
		m_First = true;
	}

	static string Esc(string s)
	{
		string r = s;
		r.Replace("\\", "\\\\");
		r.Replace("\"", "\\\"");
		r.Replace("\n", " ");
		r.Replace("\r", " ");
		r.Replace("\t", " ");
		return "\"" + r + "\"";
	}

	protected void Key(string k)
	{
		if (!m_First)
			m_Out += ",";
		m_First = false;
		m_Out += Esc(k) + ":";
	}

	DZMB_Json Str(string k, string v)
	{
		Key(k);
		m_Out += Esc(v);
		return this;
	}

	DZMB_Json Num(string k, float v)
	{
		Key(k);
		m_Out += v.ToString();
		return this;
	}

	DZMB_Json IntV(string k, int v)
	{
		Key(k);
		m_Out += v.ToString();
		return this;
	}

	DZMB_Json BoolV(string k, bool v)
	{
		Key(k);
		if (v)
			m_Out += "true";
		else
			m_Out += "false";
		return this;
	}

	DZMB_Json Raw(string k, string json)
	{
		Key(k);
		m_Out += json;
		return this;
	}

	DZMB_Json Pos(string k, vector p)
	{
		Key(k);
		m_Out += "{\"x\":" + p[0].ToString() + ",\"y\":" + p[1].ToString() + ",\"z\":" + p[2].ToString() + "}";
		return this;
	}

	DZMB_Json StrArray(string k, array<string> items)
	{
		Key(k);
		m_Out += "[";
		for (int i = 0; i < items.Count(); i++)
		{
			if (i > 0)
				m_Out += ",";
			m_Out += Esc(items[i]);
		}
		m_Out += "]";
		return this;
	}

	string End()
	{
		return m_Out + "}";
	}
}

class DZMB_Callback : RestCallback
{
	bool m_Done;
	bool m_Ok;
	int m_Code;
	string m_Body;

	override void OnSuccess(string data, int dataSize)
	{
		m_Ok = true;
		m_Body = data;
		m_Done = true;
		Completed();
	}

	override void OnError(int errorCode)
	{
		m_Ok = false;
		m_Code = errorCode;
		m_Done = true;
		Completed();
	}

	override void OnTimeout()
	{
		m_Ok = false;
		m_Code = -1;
		m_Done = true;
		Completed();
	}

	void Completed() {}
}

class DZMB_Http
{
	protected RestApi m_Api;
	protected string m_Base;

	void DZMB_Http()
	{
		m_Api = GetRestApi();
		if (!m_Api)
			m_Api = CreateRestApi();
		m_Api.SetOption(ERestOption.ERESTOPTION_READOPERATION, 10);
		m_Api.SetOption(ERestOption.ERESTOPTION_CONNECTION, 10);
		string base = DZMB_Config.Get().ingest_url;
		if (base.Length() > 0 && base.Substring(base.Length() - 1, 1) == "/")
			base = base.Substring(0, base.Length() - 1);
		m_Base = base + "/" + DZMB_Config.Get().server_token + "/";
	}

	void Post(string path, string body, DZMB_Callback cb)
	{
		RestContext ctx = m_Api.GetRestContext(m_Base);
		ctx.SetHeader("application/json");
		ctx.POST(cb, path, body);
	}
}
