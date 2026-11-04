package metricz_test

import (
	"github.com/kproios/dz-manager/internal/metricz"
	"testing"
)

func TestHorizontalPositionsDoNotRequireAltitude(t *testing.T) {
	samples, _ := metricz.Parse(`dayz_metricz_player_position_x{steam_id="76561198000000001"} 1000
dayz_metricz_player_position_z{steam_id="76561198000000001"} 2000
dayz_metricz_transport_position_x{hash="car"} 4000
dayz_metricz_transport_position_z{hash="car"} 5000`)
	snap := metricz.Build(samples)
	if len(snap.Players) != 1 || len(snap.Vehicles) != 1 || snap.Players[0].X != 1000 || snap.Vehicles[0].Z != 5000 {
		t.Fatalf("horizontal samples dropped: %+v", snap)
	}
	// Repeated X is not a substitute for a missing Z.
	snap = metricz.Build([]metricz.Sample{samples[0], samples[0], samples[0]})
	if len(snap.Players) != 0 {
		t.Fatal("incomplete entity accepted after duplicate metrics")
	}
}
