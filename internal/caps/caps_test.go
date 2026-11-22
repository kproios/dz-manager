package caps

import "testing"

// An optional integration (MetricZ) is not listed until it is in use, and
// disappears again when it is no longer configured.
func TestOptionalCapabilityHiddenUntilUsed(t *testing.T) {
	r := NewRegistry(nil)
	has := func(n Name) bool {
		for _, s := range r.List("s") {
			if s.Name == n {
				return true
			}
		}
		return false
	}
	if has(MetricZ) || !has(Bridge) {
		t.Fatal("unused MetricZ listed, or a required capability hidden")
	}
	r.Set("s", MetricZ, Degraded, "no push")
	if !has(MetricZ) {
		t.Fatal("MetricZ in use but not listed")
	}
	r.Set("s", MetricZ, Unconfigured, "not used")
	if has(MetricZ) {
		t.Fatal("MetricZ still listed after it was no longer used")
	}
}
