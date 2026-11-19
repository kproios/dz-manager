// Package worldmap knows which DayZ world a server runs (reported by
// @DZMBridge, set by hand, or the Chernarus default), installs map tiles for
// it from a terrain image (downloaded from maps.dzhosts.com or a local PNG made
// with dayz-terrain-map) and serves the place names the bridge reads from the
// game's own CfgWorlds config.
package worldmap

import (
	"regexp"
	"strings"
)

// DefaultWorld is used until the bridge reports one or an admin picks one.
const DefaultWorld = "chernarusplus"

// known lists worlds whose label and size are certain. The bridge reports the
// real values (GetWorldName, World.GetWorldSize, CfgWorlds description), so a
// world missing here still works once the bridge has connected.
var known = map[string]struct {
	Label string
	Size  float64
}{
	"chernarusplus": {"Chernarus", 15360},
	"enoch":         {"Livonia", 12800},
	"namalsk":       {"Namalsk", 12800},
}

// slugAliases maps CfgWorlds names to dzhosts.com file prefixes where the two
// differ. Others match after removing everything but letters and digits.
var slugAliases = map[string]string{
	"chernarusplus": "chernarus",
	"enoch":         "livonia",
	"takistanplus":  "taxistan-plus",
}

var reWorld = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)

// NormalizeWorld lower-cases a world name and checks it is a safe path segment.
func NormalizeWorld(w string) (string, bool) {
	w = strings.ToLower(strings.TrimSpace(w))
	return w, reWorld.MatchString(w)
}

// KnownLabel returns a display label for a world name.
func KnownLabel(world string) string {
	if k, ok := known[world]; ok {
		return k.Label
	}
	if world == "" {
		return "the map"
	}
	return strings.ToUpper(world[:1]) + world[1:]
}

// KnownSize returns the world size in metres, or 0 when unknown.
func KnownSize(world string) float64 {
	return known[world].Size
}

func squash(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SuggestSlug picks the dzhosts map that belongs to a world, or "".
func SuggestSlug(world string, slugs []string) string {
	want := slugAliases[world]
	for _, s := range slugs {
		if s == want {
			return s
		}
	}
	for _, s := range slugs {
		if squash(s) == squash(world) {
			return s
		}
	}
	return ""
}
