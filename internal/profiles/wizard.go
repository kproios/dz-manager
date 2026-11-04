package profiles

import "fmt"

// Wizard is the copy-paste material the owner needs to connect a game server.
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
		"Download DZMBridge.zip from the latest GitHub release and extract the @DZMBridge folder next to the DayZ server.",
		"Install MetricZ (upstream, unmodified) as a server-side mod.",
		"Add both to the server-side mods launch parameter (line below). They are server-only: never add them to -mod=.",
		"Start the server once so both mods create their config files, then merge the JSON below into them and restart.",
		"Watch Capabilities on the dashboard: metricz and bridge turn 'ok' when data arrives.",
	}
	switch p.ProviderKind {
	case SelfhostedArchitect:
		steps[2] = "In CFTools Architect, add @DZMBridge and @MetricZ as server mods (-servermod). They are server-only: never add them to -mod=."
	case Managed:
		steps[0] = "Download DZMBridge.zip from the latest GitHub release and upload the @DZMBridge folder via the host's panel file manager."
		steps[3] = "Start the server once, then edit the two config files in the panel file manager (profiles folder) and restart."
	}
	return Wizard{
		ServerID:      p.ID,
		ServerModLine: "-servermod=@MetricZ;@DZMBridge",
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
			"max_queue":            500,
			"log_level":            "info",
		},
		Steps: steps,
	}
}

// String is a short description used in audit details.
func (w Wizard) String() string { return fmt.Sprintf("wizard for %s", w.ServerID) }
