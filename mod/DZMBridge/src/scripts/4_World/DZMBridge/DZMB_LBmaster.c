// Optional LBmaster integration. Everything here is compiled only when the
// matching LBmaster mod is loaded (every CfgMods class name is a script
// define), so @DZMBridge has no dependency on LBmaster and works without it.
// It calls LBmaster's public script API at runtime; no LBmaster code is
// copied or modified. Turn it off with "disable_lbmaster": true in config.json.

#ifdef LBmaster_AdminTools
modded class DZMB_Bridge
{
	// Announcements and welcome messages from DZ Manager appear as LB notifications.
	override void SendNotify(PlayerIdentity identity, string title, string text)
	{
		if (!identity)
			return;
		if (DZMB_Config.Get().disable_lbmaster)
		{
			super.SendNotify(identity, title, text);
			return;
		}
		LBNotificationSystem.LBSendNotificationTranslated(identity, 10, title, new LBTranslatedString(text), "");
	}

	// LB Admin Tools' global banlist, exported for review in the panel.
	override string ExternalBanListName()
	{
		if (DZMB_Config.Get().disable_lbmaster)
			return super.ExternalBanListName();
		return "LB global banlist";
	}

	override void CollectExternalBans(array<ref DZMB_ExtBan> bans)
	{
		super.CollectExternalBans(bans);
		if (DZMB_Config.Get().disable_lbmaster || !LBGlobalBanManager.Get || !LBGlobalBanManager.Get.bans)
			return;
		foreach (LBBanEntry entry : LBGlobalBanManager.Get.bans)
		{
			if (!entry || entry.steamid == "")
				continue;
			DZMB_ExtBan ban = new DZMB_ExtBan();
			ban.steam64 = entry.steamid;
			ban.name = entry.name;
			ban.reason = entry.reason;
			ban.by = entry.bannedByName;
			ban.banned_at = entry.bannedTimestamp;
			ban.duration = entry.duration;
			bans.Insert(ban);
		}
	}
}
#endif

#ifdef LBmaster_GroupDLCPlotpole
// Territory flags of LB groups as base markers (group name, tag and radius).
// Admin map only: these never go to public feeds.
modded class DZMB_Bridge
{
	override void CollectLocalMarkers()
	{
		super.CollectLocalMarkers();
		if (DZMB_Config.Get().disable_lbmaster || !TerritoryFlag.all_Flags)
			return;
		foreach (TerritoryFlag flag : TerritoryFlag.all_Flags)
		{
			if (!flag || flag.IsHologram())
				continue;
			LBGroup owner = flag.GetOwnerGroup();
			if (!owner)
				continue;
			float radius, down, up;
			flag.GetPlotpoleSize(radius, down, up);
			string label = owner.name;
			if (label == "")
				label = owner.shortname;
			TrackLocalMarker(flag, "base_flag", label, owner.shortname, radius);
		}
	}
}
#endif

#ifdef LBmaster_BetterCodelock
// Code lock raids (doors, gates and vehicles) reported as raid events. @DZMBridge
// is a -servermod, which loads after LBmaster's -mod PBOs, so this modded class
// always follows LB's own class.
modded class ActionLBRaidCodelock
{
	override void OnFinishProgressServer(ActionData action_data)
	{
		EntityAI target = null;
		if (action_data && action_data.m_Target)
			target = EntityAI.Cast(action_data.m_Target.GetObject());
		super.OnFinishProgressServer(action_data);
		DZMB_Bridge bridge = DZMB_Bridge.Get();
		if (!bridge || DZMB_Config.Get().disable_lbmaster || !action_data || !action_data.m_Player)
			return;
		string actor = "";
		if (action_data.m_Player.GetIdentity())
			actor = action_data.m_Player.GetIdentity().GetPlainId();
		string tool = "";
		if (action_data.m_MainItem)
			tool = action_data.m_MainItem.GetType();
		EntityAI where = target;
		if (!where)
			where = action_data.m_Player;
		float health = 0;
		if (target && !target.IsSetForDeletion())
			health = target.GetHealth01("", "");
		bridge.EmitRaid("raid.codelock", actor, where, "codelock", tool, health);
	}
}
#endif
