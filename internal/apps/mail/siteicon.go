package mail

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/draw"
	_ "image/jpeg" // the icons' sizes
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"
)

// A site's icon is the face of a message proven to come from its domain
// (Verified) whose domain has no BIMI logo: BIMI asks a certificate few
// senders pay for, and most have an icon on their site. The site is the
// sender's organisational domain (news.quaily.com: quaily.com). Asked in
// order: the icons its home page names (apple-touch-icon, icon, the web
// manifest's), the largest first; /apple-touch-icon.png; /favicon.ico. An
// icon is a PNG, JPEG or ICO at least iconMinPx wide (an SVG with nothing
// unsafe in it), iconMax at most, over https; kept a month, a site with
// none asked again after a week. A PNG's transparent margin is cut away
// and the icon set on a square with a margin of its own: icons come with
// any margin, and each fills its face alike.

// The network a site's icon is asked of; tests replace them.
var (
	siteClient = &http.Client{Timeout: 15 * time.Second}
	siteURL    = func(host string) string { return "https://" + host }
)

const (
	iconKeep  = 30 * 24 * time.Hour // an icon
	iconNone  = 7 * 24 * time.Hour  // a site without one
	iconMax   = 512 << 10
	iconPage  = 1 << 20 // the home page read for its icons
	iconMinPx = 32      // smaller is a blur on a face
)

// iconExts are the kinds of icon kept, by their files' names.
var iconExts = []string{".png", ".jpg", ".ico", ".svg"}

// SiteIcon is the path of the icon of domain's site in the cache, fetched
// when it is not kept: "" when the site has none. An error (the network)
// is not kept: the next call asks again.
func (s *Store) SiteIcon(ctx context.Context, domain string) (string, error) {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	if domain == "" {
		return "", nil
	}
	site := domain
	if o, err := publicsuffix.EffectiveTLDPlusOne(domain); err == nil {
		site = o
	}
	dir := filepath.Join(s.dir, "sites")
	base := filepath.Join(dir, url.PathEscape(site))
	for _, ext := range iconExts {
		if fresh(base+ext, iconKeep) {
			return base + ext, nil
		}
	}
	if fresh(base+".none", iconNone) {
		return "", nil
	}
	data, ext, err := fetchSiteIcon(ctx, site)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	for _, e := range append(iconExts, ".none") {
		os.Remove(base + e)
	}
	if data == nil {
		return "", writeFile(base+".none", nil)
	}
	if ext == ".png" {
		data = trimIcon(data)
	}
	return base + ext, writeFile(base+ext, data)
}

// fetchSiteIcon finds a site's icon, and the extension of its kind; nil
// when there is none. The error is for a site that answered nothing at
// all: the network, not the site.
func fetchSiteIcon(ctx context.Context, site string) ([]byte, string, error) {
	root := siteURL(site)
	var cands []string
	answered := false
	page, at, err := fetch(ctx, root+"/", iconPage)
	if err == nil {
		answered = true
		if page != nil {
			cands = pageIcons(ctx, page, at)
		}
	}
	cands = append(cands, root+"/apple-touch-icon.png", root+"/favicon.ico")
	seen := map[string]bool{}
	for _, c := range cands {
		if seen[c] {
			continue
		}
		seen[c] = true
		data, _, err := fetch(ctx, c, iconMax)
		if err != nil {
			continue
		}
		answered = true
		if ext := iconKind(data); ext != "" {
			return data, ext, nil
		}
	}
	if !answered {
		return nil, "", err
	}
	return nil, "", nil
}

// fetch gets an https address: its body (nil for anything but 200, or
// over max), and where it ended after redirects. The error is the
// network's.
func fetch(ctx context.Context, addr string, max int64) ([]byte, *url.URL, error) {
	u, err := url.Parse(addr)
	if err != nil || u.Scheme != "https" {
		return nil, nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, nil, nil
	}
	resp, err := siteClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.Request.URL, nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(b)) > max {
		return nil, resp.Request.URL, nil
	}
	return b, resp.Request.URL, nil
}

// pageIcon is an icon a page names: where, and how wide it says it is.
type pageIcon struct {
	href string
	size int
}

// pageIcons are the icons a home page names in its head, and its web
// manifest's, the largest first: an apple-touch-icon without sizes is
// 180, an icon without them 16, an SVG any size.
func pageIcons(ctx context.Context, page []byte, at *url.URL) []string {
	var icons []pageIcon
	z := html.NewTokenizer(bytes.NewReader(page))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		name, hasAttr := z.TagName()
		if tt == html.EndTagToken && string(name) == "head" || tt == html.StartTagToken && string(name) == "body" {
			break
		}
		if (tt != html.StartTagToken && tt != html.SelfClosingTagToken) || string(name) != "link" || !hasAttr {
			continue
		}
		attrs := map[string]string{}
		for {
			k, v, more := z.TagAttr()
			attrs[strings.ToLower(string(k))] = string(v)
			if !more {
				break
			}
		}
		href, ok := resolve(at, attrs["href"])
		if !ok {
			continue
		}
		rel := " " + strings.ToLower(attrs["rel"]) + " "
		switch {
		case strings.Contains(rel, " apple-touch-icon"):
			icons = append(icons, pageIcon{href, iconSize(attrs["sizes"], href, 180)})
		case strings.Contains(rel, " icon "):
			icons = append(icons, pageIcon{href, iconSize(attrs["sizes"], href, 16)})
		case strings.Contains(rel, " manifest "):
			icons = append(icons, manifestIcons(ctx, href)...)
		}
	}
	sort.SliceStable(icons, func(i, j int) bool { return icons[i].size > icons[j].size })
	out := make([]string, 0, len(icons))
	for _, ic := range icons {
		if ic.size >= iconMinPx {
			out = append(out, ic.href)
		}
	}
	return out
}

// manifestIcons are a web manifest's icons.
func manifestIcons(ctx context.Context, addr string) []pageIcon {
	b, at, err := fetch(ctx, addr, 256<<10)
	if err != nil || b == nil {
		return nil
	}
	var m struct {
		Icons []struct {
			Src     string `json:"src"`
			Sizes   string `json:"sizes"`
			Purpose string `json:"purpose"`
		} `json:"icons"`
	}
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	var icons []pageIcon
	for _, ic := range m.Icons {
		// A maskable icon is cut by whoever shows it: its edges may be
		// the site's colour alone.
		if strings.TrimSpace(ic.Purpose) == "maskable" {
			continue
		}
		if href, ok := resolve(at, ic.Src); ok {
			icons = append(icons, pageIcon{href, iconSize(ic.Sizes, href, 0)})
		}
	}
	return icons
}

func resolve(at *url.URL, href string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" || at == nil || strings.HasPrefix(href, "data:") {
		return "", false
	}
	u, err := at.Parse(href)
	if err != nil {
		return "", false
	}
	return u.String(), true
}

// iconSize is the widest of sizes ("32x32 180x180", "any" for an SVG),
// else def.
func iconSize(sizes, href string, def int) int {
	best := 0
	for _, s := range strings.Fields(strings.ToLower(sizes)) {
		if s == "any" {
			best = max(best, 512)
			continue
		}
		w, _, _ := strings.Cut(s, "x")
		if n, err := strconv.Atoi(w); err == nil {
			best = max(best, n)
		}
	}
	if best == 0 && strings.HasSuffix(strings.ToLower(strings.SplitN(href, "?", 2)[0]), ".svg") {
		best = 512
	}
	if best == 0 {
		best = def
	}
	return best
}

// iconKind is the extension for an icon's kind, "" for what is not one
// (a page, a broken file, an icon too small, an SVG with something unsafe
// in it).
func iconKind(data []byte) string {
	switch ct := http.DetectContentType(data); ct {
	case "image/png", "image/jpeg":
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width < iconMinPx {
			return ""
		}
		if ct == "image/png" {
			return ".png"
		}
		return ".jpg"
	case "image/x-icon", "image/vnd.microsoft.icon":
		if icoWidth(data) < iconMinPx {
			return ""
		}
		return ".ico"
	}
	if safeSVG(data) {
		return ".svg"
	}
	return ""
}

// icoWidth is the widest image in an ICO file's directory (a width of 0
// there is 256); 0 for a broken one.
func icoWidth(data []byte) int {
	if len(data) < 6 || binary.LittleEndian.Uint16(data[0:]) != 0 || binary.LittleEndian.Uint16(data[2:]) != 1 {
		return 0
	}
	n := int(binary.LittleEndian.Uint16(data[4:]))
	widest := 0
	for i := 0; i < n && 6+16*(i+1) <= len(data); i++ {
		w := int(data[6+16*i])
		if w == 0 {
			w = 256
		}
		widest = max(widest, w)
	}
	return widest
}

// trimMargin is the margin a trimmed icon is set in, a share of its side.
const trimMargin = 0.08

// trimIcon cuts a PNG's transparent margin away and sets what is left in
// the middle of a square, trimMargin of it around; data as it was when it
// has no margin to cut, or is not a PNG it reads.
func trimIcon(data []byte) []byte {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return data
	}
	b := img.Bounds()
	ink := image.Rectangle{}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0x1000 {
				ink = ink.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if ink.Empty() || ink == b {
		return data
	}
	side := max(ink.Dx(), ink.Dy())
	side += int(float64(side) * trimMargin * 2)
	out := image.NewNRGBA(image.Rect(0, 0, side, side))
	at := image.Pt((side-ink.Dx())/2, (side-ink.Dy())/2)
	draw.Draw(out, image.Rectangle{at, at.Add(ink.Size())}, img, ink.Min, draw.Src)
	var buf bytes.Buffer
	if png.Encode(&buf, out) != nil {
		return data
	}
	return buf.Bytes()
}
