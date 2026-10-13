package ui

import (
	"fmt"
	"path/filepath"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// snapshot renders the shell's windows to PNG files in dir, for checking
// layouts without a screenshot tool. GTK draws them itself, so compositor
// effects (blur, layer animations) are missing; a plain backdrop stands in
// for the wallpaper behind translucent parts.
func (s *shellUI) snapshot(dir string) ([]string, error) {
	var files []string
	save := func(name string, win *gtk.Window) error {
		if !win.GetMapped() {
			return nil
		}
		path := filepath.Join(dir, name+".png")
		if err := renderPNG(&win.Widget, path); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		files = append(files, path)
		return nil
	}
	for i, b := range s.sortedBars() {
		if err := save(fmt.Sprintf("bar-%d-%s", i, b.connector()), &b.win.Window); err != nil {
			return files, err
		}
		if b.wallpaper != nil {
			if err := save(fmt.Sprintf("wallpaper-%d-%s", i, b.connector()), &b.wallpaper.Window); err != nil {
				return files, err
			}
		}
		if b.dock != nil {
			if err := save(fmt.Sprintf("dock-%d-%s", i, b.connector()), &b.dock.win.Window); err != nil {
				return files, err
			}
		}
	}
	if s.clipboard.open {
		if err := save("clipboard", &s.clipboard.win.Window); err != nil {
			return files, err
		}
	}
	if s.settings.win != nil {
		if err := save("settings", &s.settings.win.Window); err != nil {
			return files, err
		}
	}
	if s.control.open {
		if err := save("control", &s.control.win.Window); err != nil {
			return files, err
		}
	}
	if s.notes.open {
		if err := save("center", &s.notes.win.Window); err != nil {
			return files, err
		}
	}
	if s.notes.bwin != nil {
		if err := save("banners", &s.notes.bwin.Window); err != nil {
			return files, err
		}
	}
	if s.tray.menu != nil && s.tray.menu.GetMapped() {
		path := filepath.Join(dir, "tray-menu.png")
		if err := renderPNG(&s.tray.menu.Widget, path); err != nil {
			return files, fmt.Errorf("tray menu: %w", err)
		}
		files = append(files, path)
	}
	if s.launcher.win != nil {
		if err := save("launcher", &s.launcher.win.Window); err != nil {
			return files, err
		}
	}
	for monitor, view := range s.overview.views {
		if view.win != nil {
			if err := save("overview-"+monitor, &view.win.Window); err != nil {
				return files, err
			}
		}
	}
	if s.overview.win != nil {
		if err := save("overview", &s.overview.win.Window); err != nil {
			return files, err
		}
	}
	return files, nil
}

// rect builds a graphene_rect_t. puregotk declares the struct with two
// pointer fields; the C struct is four floats of the same total size.
func rect(x, y, w, h float32) *graphene.Rect {
	var r graphene.Rect
	*(*[4]float32)(unsafe.Pointer(&r)) = [4]float32{x, y, w, h}
	return &r
}

func renderPNG(widget *gtk.Widget, path string) error {
	w, h := float32(widget.GetWidth()), float32(widget.GetHeight())
	native := widget.GetNative()
	if native == nil {
		return fmt.Errorf("not on screen")
	}
	defer (&gobject.Object{Ptr: native.Ptr}).Unref() // GetNative returned a new reference
	renderer := native.GetRenderer()
	if renderer == nil {
		return fmt.Errorf("no renderer")
	}
	paintable := gtk.NewWidgetPaintable(widget)
	defer paintable.Unref()
	snap := gtk.NewSnapshot()
	snap.AppendColor(&gdk.RGBA{Red: 0.23, Green: 0.29, Blue: 0.38, Alpha: 1}, rect(0, 0, w, h))
	paintable.Snapshot(&snap.Snapshot, float64(w), float64(h))
	node := snap.FreeToNode()
	if node == nil {
		return fmt.Errorf("nothing drawn")
	}
	defer node.Unref()
	tex := renderer.RenderTexture(node, nil)
	if tex == nil {
		return fmt.Errorf("render failed")
	}
	defer tex.Unref()
	if !tex.SaveToPng(path) {
		return fmt.Errorf("cannot write %s", path)
	}
	return nil
}
