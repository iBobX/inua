package lightui

import (
	"errors"
	"image"
	"log"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/gst"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/mpris"
	"github.com/lyricat/inua/internal/system"
)

// What Music and Video share: pictures made in Go as textures, their
// MPRIS player on the session bus, and why GStreamer could not play.

// rgbaTexture is a texture of RGBA pixels (not premultiplied).
func rgbaTexture(pix []byte, w, h, stride int) *gdk.Texture {
	b := glib.NewBytes(pix, uint(len(pix)))
	tex := gdk.NewMemoryTexture(int32(w), int32(h), gdk.MemoryR8g8b8a8Value, b, uint(stride))
	b.Unref()
	return &tex.Texture
}

// imageTexture is a texture of an image drawn in Go.
func imageTexture(img *image.NRGBA) *gdk.Texture {
	return rgbaTexture(img.Pix, img.Rect.Dx(), img.Rect.Dy(), img.Stride)
}

// serveMPRIS puts a player on the session bus; nil when it cannot (the
// app plays all the same).
func serveMPRIS(name, identity, desktop string, h mpris.Handler) (*mpris.Server, *dbus.Conn) {
	conn, err := system.SessionBus()
	if err != nil {
		log.Printf("%s: no session bus: %v", name, err)
		return nil, nil
	}
	s, err := mpris.Serve(conn, name, identity, desktop, h)
	if err != nil {
		log.Printf("%s: mpris: %v", name, err)
		conn.Close()
		return nil, nil
	}
	return s, conn
}

// gstProblem says why GStreamer cannot play, for the window.
func gstProblem(err error) string {
	if errors.Is(err, gst.ErrMissing) {
		return i18n.T("GStreamer is not installed: install its plugins (on Ubuntu gstreamer1.0-plugins-base, -good, -bad, -ugly and gstreamer1.0-libav).")
	}
	return err.Error()
}
