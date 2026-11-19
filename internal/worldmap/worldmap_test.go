package worldmap

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

const listPage = `<h1><a href="/">DayZ Tools</a> : Terrain Maps</h1>
<h2>Official <small>DayZ v1.30.164.078.000000</small></h2>
<ul><li><h3>Chernarus</h3><ul>
<li><a href="https://maps.dzhosts.com/20261020/chernarus_1024.png">chernarus_1024.png</a> (1.4M)</li>
<li><a href="https://maps.dzhosts.com/20261020/chernarus_7616.png">chernarus_7616.png</a> (83M)</li>
<li><a href="https://maps.dzhosts.com/20261020/chernarus_15232.png">chernarus_15232.png</a> (240M)</li>
</ul></li></ul>
<h2>Workshop</h2>
<ul><li><h3><a href="https://steamcommunity.com/sharedfiles/filedetails/?id=1">Takistan Plus</a></h3><ul>
<li><a href="https://maps.dzhosts.com/20261020/taxistan-plus_2048.png">taxistan-plus_2048.png</a> (7.0M)</li>
</ul></li></ul>`

func TestParseList(t *testing.T) {
	list := ParseList(listPage, 10000)
	if len(list) != 2 {
		t.Fatalf("sources: %+v", list)
	}
	ch := list[0]
	if ch.Slug != "chernarus" || ch.Label != "Chernarus" || ch.Group != "Official" || len(ch.Files) != 3 {
		t.Fatalf("chernarus: %+v", ch)
	}
	if ch.Files[1].Size != "83M" || !ch.Files[1].Fits || ch.Files[2].Fits {
		t.Fatalf("files: %+v", ch.Files)
	}
	if best, ok := ch.BestFile(); !ok || best.Px != 7616 {
		t.Fatalf("best: %+v", best)
	}
	if tk := list[1]; tk.Label != "Takistan Plus" || tk.Group != "Workshop" {
		t.Fatalf("takistan: %+v", tk)
	}
	if s := SuggestSlug("takistanplus", []string{"chernarus", "taxistan-plus"}); s != "taxistan-plus" {
		t.Fatalf("suggest: %q", s)
	}
	if s := SuggestSlug("chernarusplus", []string{"chernarus"}); s != "chernarus" {
		t.Fatalf("suggest: %q", s)
	}
	if s := SuggestSlug("namalsk", []string{"namalsk"}); s != "namalsk" {
		t.Fatalf("suggest: %q", s)
	}
}

func TestLatinName(t *testing.T) {
	for class, want := range map[string]string{
		"Settlement_Chernogorsk": "Chernogorsk", "Hill_SvyatoyRoman": "Svyatoy Roman", "Local_AF_Balota": "AF Balota",
		"LocalOffice_Pavlovo": "Pavlovo", "ViewPoint_17": "", "Names": "", "Marine_PlotinaPobeda": "Plotina Pobeda",
	} {
		if got := LatinName(class); got != want {
			t.Errorf("LatinName(%q) = %q, want %q", class, got, want)
		}
	}
}

func TestCheckDownloadURL(t *testing.T) {
	if err := CheckDownloadURL("https://maps.dzhosts.com/20261020/chernarus_1024.png"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"http://maps.dzhosts.com/a.png", "https://evil.example/a.png", "https://maps.dzhosts.com.evil.example/a.png", "https://maps.dzhosts.com/a.png?x=1", "https://u@maps.dzhosts.com/a.png", "https://maps.dzhosts.com/a.exe"} {
		if CheckDownloadURL(bad) == nil {
			t.Errorf("%s was allowed", bad)
		}
	}
}

func TestMaxZoom(t *testing.T) {
	for px, z := range map[int]int{256: 0, 300: 1, 1024: 2, 7616: 5, 15232: 6} {
		if MaxZoomFor(px) != z {
			t.Errorf("MaxZoomFor(%d) = %d, want %d", px, MaxZoomFor(px), z)
		}
	}
	if TileCount(2) != 21 {
		t.Fatal(TileCount(2))
	}
}

// The image's top-left (north-west) corner must land in the top-left of tile 0/0/0.
func TestTilerOrientation(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 600, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 600; x++ {
			c := color.RGBA{0, 0, 255, 255} // blue
			if x < 300 && y < 300 {
				c = color.RGBA{255, 0, 0, 255} // red north-west quarter
			}
			src.Set(x, y, c)
		}
	}
	dir := t.TempDir()
	m, err := InstallImage(dir, "testworld", "file:test", src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.MaxZoom != 2 || m.SourcePx != 600 {
		t.Fatalf("meta: %+v", m)
	}
	root := TerrainDir(dir, "testworld")
	n := 0
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && filepath.Ext(p) == ".jpg" {
			n++
		}
		return nil
	})
	if n != TileCount(2) {
		t.Fatalf("tiles: %d", n)
	}
	f, err := os.Open(filepath.Join(root, "0", "0", "0.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(20, 20).RGBA()
	if r>>8 < 200 || g>>8 > 60 || b>>8 > 60 {
		t.Fatalf("north-west should be red, got %d %d %d", r>>8, g>>8, b>>8)
	}
	r, _, b, _ = img.At(230, 230).RGBA()
	if b>>8 < 200 || r>>8 > 60 {
		t.Fatalf("south-east should be blue, got r=%d b=%d", r>>8, b>>8)
	}
	if got, err := ReadMeta(dir, "testworld"); err != nil || got.MaxZoom != 2 {
		t.Fatalf("read meta: %+v %v", got, err)
	}
	// A second install replaces the first without leaving temp folders.
	if _, err := InstallImage(dir, "testworld", "file:again", src, nil); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "tiles", "testworld"))
	if len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}
}
