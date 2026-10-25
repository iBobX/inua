package portal

import (
	"net/url"

	"github.com/godbus/dbus/v5"
)

// WallpaperHandler sets the background for an app; preview asks the user
// first, with the picture shown. It waits, and stops when done is closed.
type WallpaperHandler interface {
	SetWallpaper(appID, path string, preview bool, done <-chan struct{}) bool
}

// Wallpaper serves org.freedesktop.impl.portal.Wallpaper. The lock
// screen shows the background, so set-on is the same for each.
type Wallpaper struct {
	conn *dbus.Conn
	h    WallpaperHandler
}

// NewWallpaper hands requests to h.
func NewWallpaper(conn *dbus.Conn, h WallpaperHandler) *Wallpaper {
	return &Wallpaper{conn: conn, h: h}
}

// Interface is what Export serves.
func (w *Wallpaper) Interface() Interface {
	return Interface{Name: "Wallpaper", Version: 1, Methods: wallpaperObj{w}, XML: `<method name="SetWallpaperURI">
<arg type="o" name="handle" direction="in"/><arg type="s" name="app_id" direction="in"/><arg type="s" name="parent_window" direction="in"/>
<arg type="s" name="uri" direction="in"/><arg type="a{sv}" name="options" direction="in"/><arg type="u" name="response" direction="out"/>
</method>`}
}

type wallpaperObj struct{ w *Wallpaper }

func (o wallpaperObj) SetWallpaperURI(handle dbus.ObjectPath, appID, parent, uri string, options map[string]dbus.Variant) (uint32, *dbus.Error) {
	req, err := NewRequest(o.w.conn, handle)
	if err != nil {
		return Ended, nil
	}
	defer req.End()
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || u.Path == "" {
		return Ended, nil
	}
	if !o.w.h.SetWallpaper(appID, u.Path, Options(options).Bool("show-preview"), req.Done()) {
		return Cancelled, nil
	}
	return Success, nil
}
