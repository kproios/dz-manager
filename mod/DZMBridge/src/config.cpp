// @DZMBridge — server-side only. Load with -servermod=@DZMBridge, never -mod=.
// Skeleton ported from DayZ-Takaro-Integration (MIT, (c) 2026 mad-001); see LICENSE-THIRD-PARTY.txt.
class CfgPatches
{
    class DZMBridge
    {
        units[] = {};
        weapons[] = {};
        requiredVersion = 0.1;
        requiredAddons[] = {"DZ_Data", "DZ_Scripts"};
    };
};

class CfgMods
{
    class DZMBridge
    {
        type = "mod";
        dir = "DZMBridge";
        name = "DZMBridge";
        credits = "";
        author = "kproios";
        authorID = "0";
        version = "0.1.0";
        extra = 0;
        dependencies[] = {"Game", "World", "Mission"};

        class defs
        {
            class gameScriptModule
            {
                value = "";
                files[] = {"DZMBridge/scripts/3_Game"};
            };
            class worldScriptModule
            {
                value = "";
                files[] = {"DZMBridge/scripts/4_World"};
            };
            class missionScriptModule
            {
                value = "";
                files[] = {"DZMBridge/scripts/5_Mission"};
            };
        };
    };
};
