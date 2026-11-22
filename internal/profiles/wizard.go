package profiles

import "fmt"

// Wizard is the copy-paste material an operator needs to connect a game server.
type Wizard struct {
	ServerID      string         `json:"server_id"`
	ServerModLine string         `json:"servermod_line"`
	MetricZPath   string         `json:"metricz_config_path"`
	MetricZConfig map[string]any `json:"metricz_config"`
	BridgePath    string         `json:"bridge_config_path"`
	BridgeConfig  map[string]any `json:"bridge_config"`
	Steps         []string       `json:"steps"`
}

// BuildWizard renders the configs for a profile. ingestURL is DZM_INGEST_PUBLIC_URL.
func BuildWizard(p *Profile, sec *Secrets, ingestURL string) Wizard {
	steps := []string{
		"Download DZMBridge.zip from the latest GitHub release (or subscribe to it on the Steam Workshop) and put the @DZMBridge folder next to the DayZ server.",
		"Add it to the server-side mods launch parameter (line below). It is server-only: never add it to -mod=.",
		"Start the server once so the mod creates its config file, then replace that file with the JSON below and restart.",
		"Watch the dashboard: the bridge capability turns 'ok' when the first data arrives.",
	}
	switch p.ProviderKind {
	case Selfhosted:
		steps[1] = "Add @DZMBridge to the server's start line as a server mod (-servermod=@DZMBridge) in your launch script. It is server-only: never add it to -mod=."
	case Managed:
		steps[0] = "Download DZMBridge.zip from the latest GitHub release and upload the @DZMBridge folder with your host panel's file manager (or subscribe to it on the Steam Workshop)."
		steps[1] = "Add @DZMBridge to the panel's server-side mods field. It is server-only: never add it to the client mods."
		steps[2] = "Start the server once, then edit profiles/DZMBridge/config.json in the panel's file manager and restart."
	}
	return Wizard{
		ServerID: p.ID,
		// MetricZ is optional: its config stays available for operators who also run it.
		ServerModLine: "-servermod=@DZMBridge",
		MetricZPath:   "$profile:metricz/config.json",
		MetricZConfig: map[string]any{
			"settings":         map[string]any{"disable_telemetry": true},
			"geo":              map[string]any{"disable_transform_coordinates": true},
			"disabled_metrics": map[string]any{"positions": false, "positions_height": false},
			"http": map[string]any{
				"enabled":    true,
				"serialized": true,
				"url":        ingestURL + "/ingest/metricz",
				"user":       p.MetricZUser,
				"password":   sec.MetricZPassword,
			},
		},
		BridgePath: "$profile:DZMBridge/config.json",
		BridgeConfig: map[string]any{
			"version":              1,
			"enabled":              true,
			"ingest_url":           ingestURL + "/ingest/v1",
			"server_id":            p.ID,
			"server_token":         sec.BridgeToken,
			"event_flush_seconds":  5,
			"command_poll_seconds": 3,
			"heartbeat_seconds":    30,
			"live_seconds":         2,
			"max_queue":            500,
			"log_level":            "info",
		},
		Steps: steps,
	}
}

// String is a short description used in audit details.
func (w Wizard) String() string { return fmt.Sprintf("wizard for %s", w.ServerID) }
