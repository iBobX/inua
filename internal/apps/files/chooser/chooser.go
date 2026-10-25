// Package chooser serves Files' portal interfaces (docs/picker.md):
// org.freedesktop.impl.portal.FileChooser and AppChooser, handing each
// request to the window that answers it.
package chooser

import (
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/apps/files/pick"
	"github.com/lyricat/inua/internal/portal"
)

// Picker shows the picker for a request and waits for the user; done is
// closed when the frontend closes the request, and the picker goes. ok is
// false when the user cancelled.
type Picker interface {
	Pick(r pick.Request, done <-chan struct{}) (a pick.Answer, ok bool)
}

// AppRequest is an app asking which app should open something.
type AppRequest struct {
	AppID       string
	Parent      string
	Choices     []string // desktop IDs, without .desktop
	Last        string   // the one chosen last time
	ContentType string
	URI         string
	Filename    string
}

// AppPicker shows the apps to open something with and waits for the user;
// updates brings a new list of choices while it is open (an app installed
// meanwhile), done is closed when the frontend closes the request.
type AppPicker interface {
	ChooseApp(r AppRequest, updates <-chan []string, done <-chan struct{}) (choice string, ok bool)
}

// Service serves the two interfaces on conn.
type Service struct {
	conn  *dbus.Conn
	files Picker
	apps  AppPicker

	mu      sync.Mutex
	updates map[dbus.ObjectPath]chan []string // AppChooser's open requests
}

// New prepares the service; Interfaces are what portal.Export serves.
func New(conn *dbus.Conn, files Picker, apps AppPicker) *Service {
	return &Service{conn: conn, files: files, apps: apps, updates: map[dbus.ObjectPath]chan []string{}}
}

// Interfaces are FileChooser and AppChooser.
func (s *Service) Interfaces() []portal.Interface {
	call := `<arg type="o" name="handle" direction="in"/><arg type="s" name="app_id" direction="in"/><arg type="s" name="parent_window" direction="in"/>`
	answer := `<arg type="u" name="response" direction="out"/><arg type="a{sv}" name="results" direction="out"/>`
	chooserXML := ""
	for _, m := range []string{"OpenFile", "SaveFile", "SaveFiles"} {
		chooserXML += `<method name="` + m + `">` + call + `<arg type="s" name="title" direction="in"/><arg type="a{sv}" name="options" direction="in"/>` + answer + `</method>`
	}
	return []portal.Interface{
		{Name: "FileChooser", Version: 4, Methods: fileObj{s}, XML: chooserXML},
		{Name: "AppChooser", Version: 2, Methods: appObj{s}, XML: `<method name="ChooseApplication">` + call +
			`<arg type="as" name="choices" direction="in"/><arg type="a{sv}" name="options" direction="in"/>` + answer + `</method>` +
			`<method name="UpdateChoices"><arg type="o" name="handle" direction="in"/><arg type="as" name="choices" direction="in"/></method>`},
	}
}

type fileObj struct{ s *Service }

func (o fileObj) OpenFile(handle dbus.ObjectPath, appID, parent, title string, opts map[string]dbus.Variant) (uint32, map[string]dbus.Variant, *dbus.Error) {
	return o.s.pick(handle, parent, pick.FromOptions(pick.Open, appID, title, opts))
}

func (o fileObj) SaveFile(handle dbus.ObjectPath, appID, parent, title string, opts map[string]dbus.Variant) (uint32, map[string]dbus.Variant, *dbus.Error) {
	return o.s.pick(handle, parent, pick.FromOptions(pick.Save, appID, title, opts))
}

func (o fileObj) SaveFiles(handle dbus.ObjectPath, appID, parent, title string, opts map[string]dbus.Variant) (uint32, map[string]dbus.Variant, *dbus.Error) {
	return o.s.pick(handle, parent, pick.FromOptions(pick.SaveFiles, appID, title, opts))
}

// pick runs a request: the picker waits on the request's goroutine, the
// window on the main thread.
func (s *Service) pick(handle dbus.ObjectPath, parent string, r pick.Request) (uint32, map[string]dbus.Variant, *dbus.Error) {
	r.Parent = parent
	req, err := portal.NewRequest(s.conn, handle)
	if err != nil {
		return portal.Ended, nil, nil
	}
	defer req.End()
	a, ok := s.files.Pick(r, req.Done())
	select {
	case <-req.Done():
		return portal.Ended, map[string]dbus.Variant{}, nil
	default:
	}
	if !ok {
		return portal.Cancelled, map[string]dbus.Variant{}, nil
	}
	return portal.Success, Results(r, a), nil
}

// Results are the answer as the portal sends it back.
func Results(r pick.Request, a pick.Answer) map[string]dbus.Variant {
	out := map[string]dbus.Variant{"uris": dbus.MakeVariant(pick.URIs(a.Paths))}
	if len(r.Choices) > 0 {
		pairs := make([]struct{ ID, Value string }, len(r.Choices))
		for i, c := range r.Choices {
			pairs[i].ID, pairs[i].Value = c.ID, c.Value
			if got, ok := a.Choices[c.ID]; ok {
				pairs[i].Value = got
			}
		}
		out["choices"] = dbus.MakeVariant(pairs)
	}
	if a.Filter >= 0 && a.Filter < len(r.Filters) {
		out["current_filter"] = r.Filters[a.Filter].Variant()
	}
	if r.Kind == pick.Open {
		out["writable"] = dbus.MakeVariant(true)
	}
	return out
}

type appObj struct{ s *Service }

func (o appObj) ChooseApplication(handle dbus.ObjectPath, appID, parent string, choices []string, opts map[string]dbus.Variant) (uint32, map[string]dbus.Variant, *dbus.Error) {
	s := o.s
	req, err := portal.NewRequest(s.conn, handle)
	if err != nil {
		return portal.Ended, nil, nil
	}
	defer req.End()
	updates := make(chan []string, 1)
	s.mu.Lock()
	s.updates[handle] = updates
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.updates, handle)
		s.mu.Unlock()
	}()
	o2 := portal.Options(opts)
	r := AppRequest{AppID: appID, Parent: parent, Choices: choices, Last: o2.String("last_choice"), ContentType: o2.String("content_type"),
		URI: o2.String("uri"), Filename: o2.String("filename")}
	choice, ok := s.apps.ChooseApp(r, updates, req.Done())
	select {
	case <-req.Done():
		return portal.Ended, map[string]dbus.Variant{}, nil
	default:
	}
	if !ok || choice == "" {
		return portal.Cancelled, map[string]dbus.Variant{}, nil
	}
	return portal.Success, map[string]dbus.Variant{"choice": dbus.MakeVariant(choice)}, nil
}

func (o appObj) UpdateChoices(handle dbus.ObjectPath, choices []string) *dbus.Error {
	s := o.s
	s.mu.Lock()
	ch := s.updates[handle]
	s.mu.Unlock()
	if ch != nil {
		select {
		case ch <- choices:
		default: // one waiting already: the newest wins
			select {
			case <-ch:
			default:
			}
			ch <- choices
		}
	}
	return nil
}
