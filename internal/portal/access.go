package portal

import (
	"github.com/godbus/dbus/v5"
)

// AccessQuestion is what the frontend asks the user for an app: the
// camera, the location, running in the background.
type AccessQuestion struct {
	AppID                 string
	Title, Subtitle, Body string
	Grant, Deny           string // the buttons' labels, "" for the usual
	Icon                  string
}

// AccessHandler asks the user; it waits, and stops when done is closed.
type AccessHandler interface {
	Ask(q AccessQuestion, done <-chan struct{}) (granted bool)
}

// Access serves org.freedesktop.impl.portal.Access. The app's choices
// (check boxes and lists under the question) are answered with their
// defaults: none of the frontend's questions has them.
type Access struct {
	conn *dbus.Conn
	h    AccessHandler
}

// NewAccess hands questions to h.
func NewAccess(conn *dbus.Conn, h AccessHandler) *Access { return &Access{conn: conn, h: h} }

// Interface is what Export serves.
func (a *Access) Interface() Interface {
	return Interface{Name: "Access", Version: 1, Methods: accessObj{a}, XML: `<method name="AccessDialog">
<arg type="o" name="handle" direction="in"/><arg type="s" name="app_id" direction="in"/><arg type="s" name="parent_window" direction="in"/>
<arg type="s" name="title" direction="in"/><arg type="s" name="subtitle" direction="in"/><arg type="s" name="body" direction="in"/>
<arg type="a{sv}" name="options" direction="in"/><arg type="u" name="response" direction="out"/><arg type="a{sv}" name="results" direction="out"/>
</method>`}
}

type accessObj struct{ a *Access }

func (o accessObj) AccessDialog(handle dbus.ObjectPath, appID, parent, title, subtitle, body string, options map[string]dbus.Variant) (uint32, map[string]dbus.Variant, *dbus.Error) {
	req, err := NewRequest(o.a.conn, handle)
	if err != nil {
		return Ended, nil, nil
	}
	defer req.End()
	opts := Options(options)
	q := AccessQuestion{AppID: appID, Title: title, Subtitle: subtitle, Body: body,
		Grant: opts.String("grant_label"), Deny: opts.String("deny_label"), Icon: opts.String("icon")}
	if !o.a.h.Ask(q, req.Done()) {
		return Cancelled, map[string]dbus.Variant{}, nil
	}
	results := map[string]dbus.Variant{}
	if cs, ok := options["choices"].Value().([][]any); ok && len(cs) > 0 {
		var pairs []struct{ ID, Value string }
		for _, c := range cs {
			if len(c) == 4 {
				id, _ := c[0].(string)
				def, _ := c[3].(string)
				pairs = append(pairs, struct{ ID, Value string }{id, def})
			}
		}
		results["choices"] = dbus.MakeVariant(pairs)
	}
	return Success, results, nil
}
