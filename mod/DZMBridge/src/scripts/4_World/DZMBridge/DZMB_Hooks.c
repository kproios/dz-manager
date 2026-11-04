// Engine hooks. Kill attribution follows the approach documented in
// DayZ-Takaro-Integration (MIT): since DayZ 1.20 EEKilled's killer is often the
// weapon or vehicle, so resolve the hierarchy root and the last EEHitBy source.

class DZMB_KillResolver
{
	static PlayerBase Resolve(Object killer, EntityAI lastSource)
	{
		PlayerBase pb;
		if (killer)
		{
			pb = PlayerBase.Cast(killer);
			if (pb)
				return pb;
			EntityAI ek = EntityAI.Cast(killer);
			if (ek)
			{
				pb = PlayerBase.Cast(ek.GetHierarchyRootPlayer());
				if (pb)
					return pb;
			}
		}
		if (lastSource)
		{
			pb = PlayerBase.Cast(lastSource);
			if (pb)
				return pb;
			pb = PlayerBase.Cast(lastSource.GetHierarchyRootPlayer());
			if (pb)
				return pb;
		}
		return null;
	}
}

modded class PlayerBase
{
	protected EntityAI m_DZMB_LastSource;
	protected string m_DZMB_LastAmmo;

	override void EEHitBy(TotalDamageResult damageResult, int damageType, EntityAI source, int component, string dmgZone, string ammo, vector modelPos, float speedCoef)
	{
		super.EEHitBy(damageResult, damageType, source, component, dmgZone, ammo, modelPos, speedCoef);
		if (source)
		{
			m_DZMB_LastSource = source;
			m_DZMB_LastAmmo = ammo;
		}
	}

	override void EEKilled(Object killer)
	{
		super.EEKilled(killer);
		DZMB_Bridge b = DZMB_Bridge.Get();
		if (!b)
			return;
		PlayerBase attacker = DZMB_KillResolver.Resolve(killer, m_DZMB_LastSource);
		if (attacker == this)
			attacker = null;
		string weapon = "";
		string cause = "";
		if (attacker)
		{
			if (killer && killer != attacker)
				weapon = killer.GetType();
			else
				weapon = m_DZMB_LastAmmo;
		}
		else
		{
			// No player attacker: never invent one. Record what we know as the cause.
			if (m_DZMB_LastSource && m_DZMB_LastSource != this)
				cause = m_DZMB_LastSource.GetType();
			else if (m_DZMB_LastAmmo != "")
				cause = m_DZMB_LastAmmo;
			else
				cause = "environment";
		}
		b.OnKilled(this, attacker, weapon, cause);
	}
}

// EntityAI is native and cannot be modded. Inventory objects use ItemBase;
// world event structures use the script-level House hook below.
modded class ItemBase
{
	override void EEInit()
	{
		super.EEInit();
		if (!GetGame().IsDedicatedServer())
			return;
		DZMB_Bridge b = DZMB_Bridge.Get();
		if (!b)
			return;
		// Position is only final after placement: read it a moment later.
		string category;
		if (DZMB_Bridge.EventCategory(this, category))
			GetGame().GetCallQueue(CALL_CATEGORY_SYSTEM).CallLater(DZMB_EmitSpawn, 1000, false);
	}

	void DZMB_EmitSpawn()
	{
		DZMB_Bridge b = DZMB_Bridge.Get();
		if (b)
			b.OnEntitySpawned(this);
	}

	override void EEOnCECreate()
	{
		super.EEOnCECreate();
		GetGame().GetCallQueue(CALL_CATEGORY_SYSTEM).CallLater(DZMB_EmitCE, 1000, false);
	}

	void DZMB_EmitCE()
	{
		DZMB_Bridge b = DZMB_Bridge.Get();
		if (b && DZMB_Config.Get().rare_items.Count() > 0)
			b.OnCESpawn(this);
	}

	override void EEDelete(EntityAI parent)
	{
		DZMB_Bridge b = DZMB_Bridge.Get();
		if (b && GetGame().IsDedicatedServer())
			b.OnEntityDeleted(this);
		super.EEDelete(parent);
	}
}

modded class House
{
	override void EEInit()
	{
		super.EEInit();
		string category;
		if (GetGame().IsDedicatedServer() && DZMB_Bridge.EventCategory(this, category))
			GetGame().GetCallQueue(CALL_CATEGORY_SYSTEM).CallLater(DZMB_EmitSpawn, 1000, false);
	}

	void DZMB_EmitSpawn()
	{
		DZMB_Bridge bridge = DZMB_Bridge.Get();
		if (bridge)
			bridge.OnEntitySpawned(this);
	}

	override void EEDelete(EntityAI parent)
	{
		DZMB_Bridge bridge = DZMB_Bridge.Get();
		if (bridge && GetGame().IsDedicatedServer())
			bridge.OnEntityDeleted(this);
		super.EEDelete(parent);
	}
}

// Weapons do not inherit the script-level ItemBase.
modded class Weapon_Base
{
	override void EEInit()
	{
		super.EEInit();
		string category;
		if (GetGame().IsDedicatedServer() && DZMB_Bridge.EventCategory(this, category))
			GetGame().GetCallQueue(CALL_CATEGORY_SYSTEM).CallLater(DZMB_EmitEvent, 1000, false);
	}
	void DZMB_EmitEvent()
	{
		DZMB_Bridge bridge = DZMB_Bridge.Get();
		if (bridge) bridge.OnEntitySpawned(this);
	}
	override void EEDelete(EntityAI parent)
	{
		DZMB_Bridge bridge = DZMB_Bridge.Get();
		if (bridge && GetGame().IsDedicatedServer()) bridge.OnEntityDeleted(this);
		super.EEDelete(parent);
	}
	override void EEOnCECreate()
	{
		super.EEOnCECreate();
		GetGame().GetCallQueue(CALL_CATEGORY_SYSTEM).CallLater(DZMB_EmitCE, 1000, false);
	}
	void DZMB_EmitCE()
	{
		DZMB_Bridge bridge = DZMB_Bridge.Get();
		if (bridge) bridge.OnCESpawn(this);
	}
}
