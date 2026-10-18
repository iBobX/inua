package filesui

import (
	"fmt"

	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// themeIcons draws files, folders, places and drives with the desktop's
// icon theme itself. The window's theme is the apps' Phosphor set, which
// inherits the desktop's, but GTK takes a symbolic icon of the first theme
// for a plain name (application-pdf finds Phosphor's
// application-pdf-symbolic before the desktop's application-pdf), so
// these are looked up in a GtkIconTheme of the desktop's theme alone, in
// colour, and kept.
type themeIcons struct {
	theme *gtk.IconTheme
	cache map[string]*gtk.IconPaintable
}

func newThemeIcons(name string) *themeIcons {
	t := &themeIcons{theme: gtk.NewIconTheme(), cache: map[string]*gtk.IconPaintable{}}
	if name != "" {
		t.theme.SetThemeName(name)
	}
	return t
}

// read reads the theme, the whole chain of themes it inherits (a quarter
// of a second for a large one), on a thread of its own: once made, a
// GtkIconTheme takes lookups from any thread, and one meanwhile waits for
// the theme.
func (t *themeIcons) read() {
	go t.theme.HasIcon("folder")
}

const regular = gtk.IconLookupForceRegularValue

// named is the icon called name (else "text-x-generic") at size.
func (t *themeIcons) named(name string, size, scale int32) *gtk.IconPaintable {
	key := fmt.Sprintf("%s@%d×%d", name, size, scale)
	if p, ok := t.cache[key]; ok {
		return p
	}
	p := t.theme.LookupIcon(name, []string{"text-x-generic"}, size, scale, gtk.TextDirLtrValue, regular)
	t.cache[key] = p
	return p
}

// forType is GIO's icon for a MIME type (its specific icon, then its
// generic one) at size.
func (t *themeIcons) forType(mime string, g *gio.IconBase, size, scale int32) *gtk.IconPaintable {
	key := fmt.Sprintf("%s@%d×%d", mime, size, scale)
	if p, ok := t.cache[key]; ok {
		return p
	}
	var p *gtk.IconPaintable
	if g != nil && g.Ptr != 0 {
		p = t.theme.LookupByGicon(g, size, scale, gtk.TextDirLtrValue, regular)
	}
	if p == nil {
		p = t.named("text-x-generic", size, scale)
	}
	t.cache[key] = p
	return p
}

// set shows the icon called name in img at its pixel size.
func (t *themeIcons) set(img *gtk.Image, name string, size int32) {
	img.SetPixelSize(size)
	img.SetFromPaintable(t.named(name, size, img.GetScaleFactor()))
}

// setKind shows a kind's icon in img at its pixel size.
func (t *themeIcons) setKind(img *gtk.Image, k kind, size int32) {
	img.SetPixelSize(size)
	if k.gicon == nil {
		img.SetFromPaintable(t.named(k.icon, size, img.GetScaleFactor()))
		return
	}
	img.SetFromPaintable(t.forType(k.mime, k.gicon, size, img.GetScaleFactor()))
}
