package worldmap

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// TileSize is the Leaflet tile size. Zoom 0 shows the whole world in one tile.
const TileSize = 256

// Meta describes an installed tile set (tiles/<world>/terrain/meta.json).
type Meta struct {
	World       string `json:"world"`
	Source      string `json:"source"` // dzhosts:<slug>_<px> or file:<name>
	SourcePx    int    `json:"source_px"`
	MaxZoom     int    `json:"max_zoom"`
	TileSize    int    `json:"tile_size"`
	Format      string `json:"format"`
	InstalledAt string `json:"installed_at"`
}

// MaxZoomFor is the smallest zoom whose full width (256 << z) covers px.
func MaxZoomFor(px int) int {
	z := 0
	for TileSize<<z < px && z < 8 {
		z++
	}
	return z
}

// TileCount is the number of tiles in a pyramid up to maxZoom.
func TileCount(maxZoom int) int {
	n := 0
	for z := 0; z <= maxZoom; z++ {
		n += 1 << (2 * z)
	}
	return n
}

// sampler reads RGB quickly from the common decoded PNG types.
type sampler struct {
	img  image.Image
	rgba *image.RGBA
	nrgb *image.NRGBA
	w, h int
	minX int
	minY int
}

func newSampler(img image.Image) *sampler {
	b := img.Bounds()
	s := &sampler{img: img, w: b.Dx(), h: b.Dy(), minX: b.Min.X, minY: b.Min.Y}
	s.rgba, _ = img.(*image.RGBA)
	s.nrgb, _ = img.(*image.NRGBA)
	return s
}

func (s *sampler) px(x, y int) (r, g, b float64) {
	if x < 0 {
		x = 0
	} else if x >= s.w {
		x = s.w - 1
	}
	if y < 0 {
		y = 0
	} else if y >= s.h {
		y = s.h - 1
	}
	switch {
	case s.rgba != nil:
		i := s.rgba.PixOffset(x+s.minX, y+s.minY)
		p := s.rgba.Pix[i : i+3 : i+3]
		return float64(p[0]), float64(p[1]), float64(p[2])
	case s.nrgb != nil:
		i := s.nrgb.PixOffset(x+s.minX, y+s.minY)
		p := s.nrgb.Pix[i : i+3 : i+3]
		return float64(p[0]), float64(p[1]), float64(p[2])
	}
	c := color.NRGBAModel.Convert(s.img.At(x+s.minX, y+s.minY)).(color.NRGBA)
	return float64(c.R), float64(c.G), float64(c.B)
}

// bilinear samples at fractional source coordinates (pixel centres at +0.5).
func (s *sampler) bilinear(fx, fy float64) (uint8, uint8, uint8) {
	fx -= 0.5
	fy -= 0.5
	x0, y0 := int(fx), int(fy)
	if fx < 0 {
		x0 = -1
	}
	if fy < 0 {
		y0 = -1
	}
	dx, dy := fx-float64(x0), fy-float64(y0)
	r00, g00, b00 := s.px(x0, y0)
	r10, g10, b10 := s.px(x0+1, y0)
	r01, g01, b01 := s.px(x0, y0+1)
	r11, g11, b11 := s.px(x0+1, y0+1)
	mix := func(a, b, c, d float64) uint8 {
		v := a*(1-dx)*(1-dy) + b*dx*(1-dy) + c*(1-dx)*dy + d*dx*dy
		if v < 0 {
			v = 0
		} else if v > 255 {
			v = 255
		}
		return uint8(v + 0.5)
	}
	return mix(r00, r10, r01, r11), mix(g00, g10, g01, g11), mix(b00, b10, b01, b11)
}

// Tiler builds a JPEG tile pyramid from one world image. The image covers the
// whole terrain: its top edge is north (game z = world size), its left edge
// is west (x = 0). Tiles are built depth-first so only a few are in memory.
type Tiler struct {
	Src      image.Image
	Dir      string // output directory: <dir>/<z>/<x>/<y>.jpg
	Quality  int
	Progress func(done, total int)

	s       *sampler
	maxZoom int
	done    int
	total   int
}

// Run writes every tile and returns the max zoom.
func (t *Tiler) Run() (int, error) {
	t.s = newSampler(t.Src)
	if t.s.w < TileSize || t.s.h < TileSize {
		return 0, errors.New("the map image is too small (under 256 px)")
	}
	px := t.s.w
	if t.s.h > px {
		px = t.s.h
	}
	t.maxZoom = MaxZoomFor(px)
	t.total = TileCount(t.maxZoom)
	if t.Quality == 0 {
		t.Quality = 85
	}
	_, err := t.build(0, 0, 0)
	return t.maxZoom, err
}

func (t *Tiler) build(z, x, y int) (*image.RGBA, error) {
	out := image.NewRGBA(image.Rect(0, 0, TileSize, TileSize))
	if z == t.maxZoom {
		full := float64(int(TileSize) << z)
		sx, sy := float64(t.s.w)/full, float64(t.s.h)/full
		for py := 0; py < TileSize; py++ {
			fy := (float64(y*TileSize+py) + 0.5) * sy
			for px := 0; px < TileSize; px++ {
				fx := (float64(x*TileSize+px) + 0.5) * sx
				r, g, b := t.s.bilinear(fx, fy)
				i := out.PixOffset(px, py)
				out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = r, g, b, 255
			}
		}
	} else {
		for q := 0; q < 4; q++ {
			cx, cy := q%2, q/2
			child, err := t.build(z+1, 2*x+cx, 2*y+cy)
			if err != nil {
				return nil, err
			}
			ox, oy := cx*TileSize/2, cy*TileSize/2
			for py := 0; py < TileSize/2; py++ {
				for px := 0; px < TileSize/2; px++ {
					var r, g, b int
					for _, d := range [4][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
						i := child.PixOffset(2*px+d[0], 2*py+d[1])
						r += int(child.Pix[i])
						g += int(child.Pix[i+1])
						b += int(child.Pix[i+2])
					}
					i := out.PixOffset(ox+px, oy+py)
					out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = uint8((r+2)/4), uint8((g+2)/4), uint8((b+2)/4), 255
				}
			}
		}
	}
	if err := t.write(z, x, y, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (t *Tiler) write(z, x, y int, img *image.RGBA) error {
	dir := filepath.Join(t.Dir, strconv.Itoa(z), strconv.Itoa(x))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, strconv.Itoa(y)+".jpg"))
	if err != nil {
		return err
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: t.Quality}); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	t.done++
	if t.Progress != nil {
		t.Progress(t.done, t.total)
	}
	return nil
}

// TerrainDir is where a world's terrain tiles live.
func TerrainDir(dataDir, world string) string {
	return filepath.Join(dataDir, "tiles", world, "terrain")
}

// ReadMeta returns the installed tile set of a world, if any.
func ReadMeta(dataDir, world string) (*Meta, error) {
	b, err := os.ReadFile(filepath.Join(TerrainDir(dataDir, world), "meta.json"))
	if err != nil {
		return nil, err
	}
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// InstallImage tiles img into a temporary folder next to the final one and
// swaps it in only when every tile was written.
func InstallImage(dataDir, world, source string, img image.Image, progress func(done, total int)) (*Meta, error) {
	final := TerrainDir(dataDir, world)
	tmp := final + ".new-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		return nil, err
	}
	t := &Tiler{Src: img, Dir: tmp, Progress: progress}
	maxZoom, err := t.Run()
	if err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	b := img.Bounds()
	px := b.Dx()
	if b.Dy() > px {
		px = b.Dy()
	}
	m := &Meta{World: world, Source: source, SourcePx: px, MaxZoom: maxZoom, TileSize: TileSize, Format: "jpg", InstalledAt: time.Now().UTC().Format(time.RFC3339)}
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(tmp, "meta.json"), mb, 0o640); err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	old := final + ".old-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if _, err := os.Stat(final); err == nil {
		if err := os.Rename(final, old); err != nil {
			os.RemoveAll(tmp)
			return nil, fmt.Errorf("cannot replace the old tiles: %w", err)
		}
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Rename(old, final)
		os.RemoveAll(tmp)
		return nil, err
	}
	os.RemoveAll(old)
	return m, nil
}
