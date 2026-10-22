package mail

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func pngOf(t *testing.T, w int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, w))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// icoOf is an ICO file's header and directory with one image w wide.
func icoOf(w int) []byte {
	b := make([]byte, 6+16)
	binary.LittleEndian.PutUint16(b[2:], 1)
	binary.LittleEndian.PutUint16(b[4:], 1)
	b[6] = byte(w % 256)
	return b
}

// siteNet serves sites over https, a site's files under /<site>/; it
// counts the requests each site gets.
func siteNet(t *testing.T, files map[string][]byte) map[string]int {
	t.Helper()
	var mu sync.Mutex
	asked := map[string]int{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		mu.Lock()
		asked[site]++
		mu.Unlock()
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	oldClient, oldURL := siteClient, siteURL
	t.Cleanup(func() { siteClient, siteURL = oldClient, oldURL })
	siteClient = srv.Client()
	siteURL = func(host string) string { return srv.URL + "/" + host }
	return asked
}

func TestSiteIcon(t *testing.T) {
	touch := pngOf(t, 180)
	manifestIcon := pngOf(t, 192)
	asked := siteNet(t, map[string][]byte{
		// The largest the page names, not its 16-pixel favicon.
		"/quaily.com/": []byte(`<html><head><link rel="icon" href="small.png" sizes="16x16">
<link rel="apple-touch-icon" href="touch.png"></head><body><link rel="icon" href="late.png" sizes="999x999"></body></html>`),
		"/quaily.com/small.png": pngOf(t, 16),
		"/quaily.com/touch.png": touch,
		"/quaily.com/late.png":  pngOf(t, 200), // in the body: not the head's
		// No page: the favicon.
		"/plain.example/favicon.ico": icoOf(48),
		// The manifest's, not its maskable one.
		"/app.example/":                 []byte(`<head><link rel="manifest" href="/app.example/site.webmanifest"></head>`),
		"/app.example/site.webmanifest": []byte(`{"icons":[{"src":"mask.png","sizes":"512x512","purpose":"maskable"},{"src":"icon.png","sizes":"192x192"}]}`),
		"/app.example/mask.png":         pngOf(t, 512),
		"/app.example/icon.png":         manifestIcon,
		// Too small to show.
		"/tiny.example/favicon.ico": icoOf(16),
	})
	s := &Store{dir: t.TempDir()}
	ctx := context.Background()
	for _, c := range []struct {
		domain, ext string
		want        []byte
	}{
		{"news.quaily.com", ".png", touch}, // the organisational domain's site
		{"plain.example", ".ico", icoOf(48)},
		{"app.example", ".png", manifestIcon},
		{"tiny.example", "", nil},
		{"nothing.example", "", nil},
	} {
		path, err := s.SiteIcon(ctx, c.domain)
		if err != nil {
			t.Fatalf("%s: %v", c.domain, err)
		}
		if c.want == nil {
			if path != "" {
				t.Errorf("%s: %s, want none", c.domain, path)
			}
			continue
		}
		if filepath.Ext(path) != c.ext {
			t.Errorf("%s: %s, want a %s", c.domain, path, c.ext)
			continue
		}
		if got := readFile(t, path); !bytes.Equal(got, c.want) {
			t.Errorf("%s: the wrong icon", c.domain)
		}
	}
	// Kept: neither an icon nor a site without one is asked again.
	before := asked["quaily.com"] + asked["nothing.example"]
	s.SiteIcon(ctx, "quaily.com")
	s.SiteIcon(ctx, "nothing.example")
	if after := asked["quaily.com"] + asked["nothing.example"]; after != before {
		t.Errorf("asked again: %d requests, then %d", before, after)
	}
}

// A network that answers nothing is not kept as a site without an icon.
func TestSiteIconOffline(t *testing.T) {
	siteNet(t, nil)
	siteURL = func(string) string { return "https://127.0.0.1:1" }
	s := &Store{dir: t.TempDir()}
	if _, err := s.SiteIcon(context.Background(), "quaily.com"); err == nil {
		t.Error("no error offline")
	}
	if fresh(filepath.Join(s.dir, "sites", "quaily.com.none"), iconNone) {
		t.Error("offline kept as a site without an icon")
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// An icon's transparent margin is cut away, and what is left set in the
// middle of a square with a margin of its own.
func TestTrimIcon(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	for y := 10; y < 50; y++ {
		for x := 10; x < 30; x++ {
			img.Set(x, y, color.NRGBA{0, 0, 0, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	out, err := png.Decode(bytes.NewReader(trimIcon(b.Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	// 40 high, 8% each side: a square of 46.
	if got := out.Bounds(); got.Dx() != 46 || got.Dy() != 46 {
		t.Fatalf("trimmed to %v", got)
	}
	if _, _, _, a := out.At(0, 0).RGBA(); a != 0 {
		t.Error("the margin is not transparent")
	}
	if _, _, _, a := out.At(23, 23).RGBA(); a == 0 {
		t.Error("the mark is not in the middle")
	}
	// Nothing to cut: as it was.
	opaque := image.NewNRGBA(image.Rect(0, 0, 40, 40))
	draw.Draw(opaque, opaque.Bounds(), image.NewUniform(color.NRGBA{255, 0, 0, 255}), image.Point{}, draw.Src)
	b.Reset()
	png.Encode(&b, opaque)
	full := b.Bytes()
	if !bytes.Equal(trimIcon(full), full) {
		t.Error("an icon without a margin changed")
	}
}
