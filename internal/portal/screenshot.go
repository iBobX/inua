package portal

import (
	"image/color"
	"net/url"

	"github.com/godbus/dbus/v5"
)

// ScreenshotHandler takes screenshots and picks colours for apps; its
// methods wait for the user, and stop when done is closed. ok is false
// when the user cancelled or it failed.
type ScreenshotHandler interface {
	// Screenshot writes a screenshot to a file the app may read: the
	// whole screen, or what the user chooses when interactive.
	Screenshot(appID string, interactive bool, done <-chan struct{}) (path string, ok bool)
	// PickColor lets the user pick a colour on the screen.
	PickColor(appID string, done <-chan struct{}) (c color.NRGBA, ok bool)
}

// Screenshots serves org.freedesktop.impl.portal.Screenshot. The frontend
// asks the user whether an app may take a screenshot without asking
// (permission_store_checked) and remembers the answer.
type Screenshots struct {
	conn *dbus.Conn
	h    ScreenshotHandler
}

// NewScreenshots hands requests to h.
func NewScreenshots(conn *dbus.Conn, h ScreenshotHandler) *Screenshots {
	return &Screenshots{conn: conn, h: h}
}

// Interface is what Export serves.
func (s *Screenshots) Interface() Interface {
	call := `<arg type="o" name="handle" direction="in"/><arg type="s" name="app_id" direction="in"/><arg type="s" name="parent_window" direction="in"/><arg type="a{sv}" name="options" direction="in"/><arg type="u" name="response" direction="out"/><arg type="a{sv}" name="results" direction="out"/>`
	return Interface{Name: "Screenshot", Version: 2, Methods: screenshotObj{s}, XML: `<method name="Screenshot">` + call + `</method><method name="PickColor">` + call + `</method>`}
}

type screenshotObj struct{ s *Screenshots }

func (o screenshotObj) Screenshot(handle dbus.ObjectPath, appID, parent string, options map[string]dbus.Variant) (uint32, map[string]dbus.Variant, *dbus.Error) {
	req, err := NewRequest(o.s.conn, handle)
	if err != nil {
		return Ended, nil, nil
	}
	defer req.End()
	path, ok := o.s.h.Screenshot(appID, Options(options).Bool("interactive"), req.Done())
	if !ok {
		return Cancelled, map[string]dbus.Variant{}, nil
	}
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	return Success, map[string]dbus.Variant{"uri": dbus.MakeVariant(uri)}, nil
}

func (o screenshotObj) PickColor(handle dbus.ObjectPath, appID, parent string, options map[string]dbus.Variant) (uint32, map[string]dbus.Variant, *dbus.Error) {
	req, err := NewRequest(o.s.conn, handle)
	if err != nil {
		return Ended, nil, nil
	}
	defer req.End()
	c, ok := o.s.h.PickColor(appID, req.Done())
	if !ok {
		return Cancelled, map[string]dbus.Variant{}, nil
	}
	rgb := struct{ R, G, B float64 }{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255}
	return Success, map[string]dbus.Variant{"color": dbus.MakeVariant(rgb)}, nil
}
