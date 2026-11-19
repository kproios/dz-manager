package worldmap

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The download source: images rendered from DayZ's terrain textures by
// dayz-terrain-map (MIT) and published by its author for anyone to download.
const (
	ListURL      = "https://tools.dzhosts.com/maps/"
	downloadHost = "maps.dzhosts.com"
)

// SourceFile is one downloadable resolution.
type SourceFile struct {
	Px    int    `json:"px"`
	URL   string `json:"url"`
	Size  string `json:"size,omitempty"` // as listed, e.g. "83M"
	Fits  bool   `json:"fits"`           // within this server's pixel limit
	Name  string `json:"name"`
	Built string `json:"built,omitempty"` // yyyymmdd folder
}

// Source is one map on the list.
type Source struct {
	Slug  string       `json:"slug"`
	Label string       `json:"label"`
	Group string       `json:"group"` // Official | Workshop
	Files []SourceFile `json:"files"`
}

var (
	reToken = regexp.MustCompile(`(?is)<h2[^>]*>(.*?)</h2>|<h3[^>]*>(.*?)</h3>|<a href="(https://maps\.dzhosts\.com/(\d{8})/([a-z0-9-]+)_(\d+)\.png)"[^>]*>[^<]*</a>\s*(?:\(([0-9.]+[KMG])\))?`)
	reTags  = regexp.MustCompile(`(?s)<[^>]*>`)
)

func text(s string) string {
	s = reTags.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

// ParseList reads the dzhosts.com map list page.
func ParseList(page string, maxPx int) []Source {
	var out []*Source
	bySlug := map[string]*Source{}
	group, label := "", ""
	for _, m := range reToken.FindAllStringSubmatch(page, -1) {
		switch {
		case m[1] != "":
			group = text(m[1])
			if i := strings.IndexByte(group, ' '); i > 0 {
				group = group[:i] // "Official DayZ v1.30…" → "Official"
			}
		case m[2] != "":
			label = text(m[2])
		case m[3] != "":
			px, _ := strconv.Atoi(m[6])
			slug := m[5]
			s := bySlug[slug]
			if s == nil {
				l := label
				if l == "" {
					l = slug
				}
				s = &Source{Slug: slug, Label: l, Group: group}
				bySlug[slug] = s
				out = append(out, s)
			}
			s.Files = append(s.Files, SourceFile{Px: px, URL: m[3], Size: m[7], Fits: px <= maxPx, Name: slug + "_" + m[6] + ".png", Built: m[4]})
		}
	}
	res := make([]Source, 0, len(out))
	for _, s := range out {
		sort.Slice(s.Files, func(i, j int) bool { return s.Files[i].Px < s.Files[j].Px })
		res = append(res, *s)
	}
	return res
}

// BestFile is the largest resolution within the limit.
func (s Source) BestFile() (SourceFile, bool) {
	var best SourceFile
	ok := false
	for _, f := range s.Files {
		if f.Fits && f.Px > best.Px {
			best, ok = f, true
		}
	}
	return best, ok
}

// CheckDownloadURL only allows PNGs from the dzhosts.com map store.
func CheckDownloadURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != downloadHost || u.User != nil || u.RawQuery != "" || !strings.HasSuffix(u.Path, ".png") {
		return errors.New("map images can only be downloaded from https://" + downloadHost + "/")
	}
	return nil
}

// FetchList downloads and parses the list page.
func FetchList(ctx context.Context, c *http.Client, maxPx int) ([]Source, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ListURL, nil)
	req.Header.Set("User-Agent", "dz-manager")
	res, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach dzhosts.com: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("dzhosts.com answered HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	list := ParseList(string(b), maxPx)
	if len(list) == 0 {
		return nil, errors.New("the dzhosts.com map list has no maps (page layout changed?)")
	}
	return list, nil
}

// Download saves a map image to dst (at most maxBytes), reporting progress.
func Download(ctx context.Context, c *http.Client, rawURL, dst string, maxBytes int64, progress func(done, total int64)) error {
	if err := CheckDownloadURL(rawURL); err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	req.Header.Set("User-Agent", "dz-manager")
	res, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d", res.StatusCode)
	}
	if res.Request != nil && CheckDownloadURL(res.Request.URL.String()) != nil {
		return errors.New("download was redirected away from " + downloadHost)
	}
	if res.ContentLength > maxBytes {
		return fmt.Errorf("the image is %d MB, over the %d MB limit", res.ContentLength>>20, maxBytes>>20)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > maxBytes {
				f.Close()
				return fmt.Errorf("the image is over the %d MB limit", maxBytes>>20)
			}
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return err
			}
			if progress != nil {
				progress(done, res.ContentLength)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return fmt.Errorf("download interrupted: %w", rerr)
		}
	}
	return f.Close()
}
