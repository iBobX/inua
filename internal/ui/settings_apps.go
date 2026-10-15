package ui

import (
	"log"
	"path/filepath"
	"slices"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// appsSettings is the Default Apps page: a row a kind of default
// (desktop.Roles), each a menu of the installed apps that can be it, the
// default now chosen. A choice is written at once where other apps read
// it too: mimeapps.list, or for the terminal xdg-terminals.list and the
// launcher's terminal. The rows are laid out again each time the page is
// shown, as apps come and go.
type appsSettings struct {
	box   gtk.Box
	drops []*gtk.DropDown
	ids   [][]string // each row's apps' IDs, as its menu lists them; "" for none
}

func (s *shellUI) getAppsSettings(ui *gtkx.Builder) {
	ui.Get("default_apps", &s.settings.apps.box)
	s.onDefaultApp = func(ptr, _, _ uintptr) {
		a := &s.settings.apps
		if s.settings.filling {
			return
		}
		for i, d := range a.drops {
			if d.GoPointer() == ptr {
				if k := int(d.GetSelected()); k < len(a.ids[i]) && a.ids[i][k] != "" {
					s.setDefaultApp(desktop.Roles[i], a.ids[i][k])
				}
			}
		}
	}
}

// showDefaultApps lays out the page's rows, each with the default now.
func (s *shellUI) showDefaultApps() {
	st := &s.settings
	a := &st.apps
	if st.win == nil {
		return
	}
	st.filling = true
	defer func() { st.filling = false }()
	gtkx.Clear(&a.box)
	a.drops, a.ids = nil, nil
	byID := map[string]*desktop.Entry{}
	for _, e := range s.apps {
		byID[e.ID] = e
	}
	installed := func(id string) bool { return byID[id] != nil }
	for i, role := range desktop.Roles {
		if i > 0 {
			gtkx.Append(&a.box, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
		}
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
		row.AddCssClass("row")
		text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		text.SetHexpand(true)
		text.SetValign(gtk.AlignCenterValue)
		title := gtk.NewLabel(i18n.T(role.Name))
		title.AddCssClass("title")
		title.SetXalign(0)
		gtkx.Append(text, &title.Widget)
		sub := gtk.NewLabel(i18n.T(role.About))
		sub.AddCssClass("sub")
		sub.SetXalign(0)
		sub.SetWrap(true)
		gtkx.Append(text, &sub.Widget)
		gtkx.Append(row, &text.Widget)

		current := s.defaultApp(role, installed)
		apps := role.Candidates(s.apps)
		if e := byID[current]; e != nil && !slices.Contains(apps, e) {
			apps = append(apps, e) // hidden from menus, but chosen
		}
		var names, ids []string
		if current == "" {
			names, ids = append(names, i18n.T("None chosen")), append(ids, "")
		}
		for _, e := range apps {
			names, ids = append(names, appLabel(e, apps)), append(ids, e.ID)
		}
		d := gtk.NewDropDownFromStrings(names)
		d.SetValign(gtk.AlignCenterValue)
		d.SetSelected(uint32(max(0, slices.Index(ids, current))))
		d.SetSensitive(len(apps) > 0)
		gtkx.ConnectNotify(d, "selected", &s.onDefaultApp)
		gtkx.Append(row, &d.Widget)
		gtkx.Append(&a.box, &row.Widget)
		a.drops, a.ids = append(a.drops, d), append(a.ids, ids)
	}
}

// appLabel is an app's name in a menu, told apart when another of the
// menu's apps has the same name: inua's own as inua's, another by its ID.
func appLabel(e *desktop.Entry, apps []*desktop.Entry) string {
	for _, o := range apps {
		if o != e && o.Name == e.Name {
			if strings.HasPrefix(e.ID, "io.github.lyricat.Inua.") {
				return i18n.Tf("%s (inua)", e.Name)
			}
			return e.Name + " (" + e.ID + ")"
		}
	}
	return e.Name
}

// defaultApp is the app a role has now: the type's default, or for the
// terminal the launcher's, then xdg-terminals.list's.
func (s *shellUI) defaultApp(role desktop.Role, installed func(string) bool) string {
	if role.Category == "" {
		return desktop.Default(desktop.MimeAppsPaths(desktop.CurrentDesktops()), role.Types[0], installed)
	}
	if term := s.cfg.TerminalCommand(); len(term) > 0 {
		for _, e := range s.apps {
			if len(e.Command) > 0 && slices.Contains(e.Categories, role.Category) && filepath.Base(e.Command[0]) == filepath.Base(term[0]) {
				return e.ID
			}
		}
	}
	return desktop.DefaultTerminal(desktop.TerminalsPaths(desktop.CurrentDesktops()), installed)
}

// setDefaultApp makes app id the role's default: for the types it opens
// (and the role's first), or the terminal, in the launcher too.
func (s *shellUI) setDefaultApp(role desktop.Role, id string) {
	var e *desktop.Entry
	for _, a := range s.apps {
		if a.ID == id {
			e = a
		}
	}
	if e == nil {
		return
	}
	var err error
	if role.Category == "" {
		types := []string{role.Types[0]}
		for _, t := range role.Types[1:] {
			if slices.Contains(e.MimeTypes, t) {
				types = append(types, t)
			}
		}
		err = desktop.SetDefault(desktop.UserMimeApps(), types, id)
	} else {
		err = desktop.SetTerminal(desktop.UserTerminals(), id)
		if err == nil && len(e.Command) > 0 {
			c := s.cfg
			c.Launcher.Terminal = strings.Join(e.Command, " ")
			s.changeSettings(c)
			s.settings.filling = true // the Launcher page's field, as it is now
			setEntry(&s.settings.terminal, c.Launcher.Terminal)
			s.settings.filling = false
		}
	}
	if err != nil {
		log.Printf("settings: default %s: %v", role.ID, err)
		s.setStatus(i18n.Tf("Not saved: %s", err.Error()), true)
		return
	}
	s.setStatus("", false)
}
