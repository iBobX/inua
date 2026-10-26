package ui

import (
	"log"
	"slices"

	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/portal"
	"github.com/lyricat/inua/internal/portalconf"
	"github.com/lyricat/inua/internal/system"
)

// portalGroup is a row of the Default Apps page's [DIALOGS]: interfaces
// inua portals turns on together, and the process that serves them.
type portalGroup struct {
	name, about, off string
	ifaces           []string
	bus              string // the backend's bus name; "" for the share picker, the shell's
	activated        bool   // D-Bus starts it at a request
}

var portalGroups = []portalGroup{
	{name: i18n.M("Open and save"), about: i18n.M("inua's file chooser and Open With, for every app"), off: i18n.M("Off: GTK's file chooser"),
		ifaces: []string{"FileChooser", "AppChooser"}, bus: portal.FilesName, activated: true},
	{name: i18n.M("Share the screen"), about: i18n.M("Choose a screen, a window or an area with inua's screenshot tool"), off: i18n.M("Off: Hyprland's picker"),
		ifaces: []string{portalconf.SharePicker}, bus: portal.ShellName},
	{name: i18n.M("Notifications and the rest"), about: i18n.M("Apps' notifications, screenshots, staying awake, running in the background, the theme and the wallpaper"), off: i18n.M("Off: GTK's"),
		ifaces: []string{"Settings", "Notification", "Inhibit", "Screenshot", "Access", "Background", "Wallpaper"}, bus: portal.ShellName},
	{name: i18n.M("Secrets"), about: i18n.M("Passwords of sandboxed apps, kept by inua-keyring"), off: i18n.M("Off: the keyring's own, if it has one"),
		ifaces: []string{"Secret"}, bus: portal.KeyringName, activated: true},
}

// portalsSettings is [DIALOGS]: a switch a portalGroup, and under its
// text whether it is on, from when, and whether its process runs. Read
// off the main thread each time the page is shown; a switch turns its
// group on or off as inua portals does.
type portalsSettings struct {
	box      gtk.Box
	switches []*gtk.Switch
	states   []*gtk.Label
	busy     bool // a change is being applied
	onSwitch func(gtk.Switch, bool) bool
}

// portalState is what a row shows.
type portalState struct {
	on     bool
	status string
}

func (s *shellUI) getPortalsSettings(ui *gtkx.Builder) {
	p := &s.settings.portals
	ui.Get("portal_rows", &p.box)
	p.switches, p.states = nil, nil
	for i, g := range portalGroups {
		if i > 0 {
			gtkx.Append(&p.box, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
		}
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
		row.AddCssClass("row")
		text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		text.SetHexpand(true)
		text.SetValign(gtk.AlignCenterValue)
		title := gtk.NewLabel(i18n.T(g.name))
		title.AddCssClass("title")
		title.SetXalign(0)
		gtkx.Append(text, &title.Widget)
		sub := gtk.NewLabel(i18n.T(g.about))
		sub.AddCssClass("sub")
		sub.SetXalign(0)
		sub.SetWrap(true)
		gtkx.Append(text, &sub.Widget)
		state := gtk.NewLabel("")
		state.AddCssClass("sub")
		state.SetXalign(0)
		state.SetWrap(true)
		gtkx.Append(text, &state.Widget)
		gtkx.Append(row, &text.Widget)
		sw := gtk.NewSwitch()
		sw.SetValign(gtk.AlignCenterValue)
		sw.SetSensitive(false) // until read
		gtkx.Append(row, &sw.Widget)
		gtkx.Append(&p.box, &row.Widget)
		p.switches, p.states = append(p.switches, sw), append(p.states, state)
	}
	p.onSwitch = func(sw gtk.Switch, on bool) bool {
		if s.settings.filling || p.busy {
			return false
		}
		for i, w := range p.switches {
			if w.GoPointer() == sw.GoPointer() {
				s.setPortals(portalGroups[i], on)
			}
		}
		return false
	}
	for _, sw := range p.switches {
		sw.ConnectStateSet(&p.onSwitch)
	}
}

// showPortals reads the groups' states and shows them.
func (s *shellUI) showPortals() {
	go func() {
		states := readPortals()
		gtkx.Post(func() { s.fillPortals(states) })
	}()
}

func (s *shellUI) fillPortals(states []portalState) {
	st := &s.settings
	p := &st.portals
	if st.win == nil || len(states) != len(p.switches) {
		return
	}
	st.filling = true
	defer func() { st.filling = false }()
	for i, ps := range states {
		p.switches[i].SetActive(ps.on)
		p.switches[i].SetSensitive(!p.busy)
		p.states[i].SetText(ps.status)
	}
}

// readPortals is each group's state: on when any of its interfaces is.
// It asks D-Bus and systemd, so not on the main thread.
func readPortals() []portalState {
	d := portalconf.Default()
	enabled := d.Enabled()
	reads := portalconf.SessionDir() == d.Own
	var conn *dbus.Conn
	if c, err := system.SessionBus(); err == nil {
		conn = c
		defer conn.Close()
	}
	out := make([]portalState, len(portalGroups))
	for i, g := range portalGroups {
		on := slices.ContainsFunc(g.ifaces, func(i string) bool { return slices.Contains(enabled, i) })
		ps := portalState{on: on, status: i18n.T(g.off)}
		switch {
		case !on:
		case !reads:
			ps.status = i18n.T("On from the next inua session")
		case conn != nil && portalconf.HasOwner(conn, g.bus):
			ps.status = i18n.T("On, running")
		case g.activated:
			ps.status = i18n.T("On, starts when an app asks")
		default:
			ps.status = i18n.T("On, but the shell does not serve it: restart the shell")
		}
		out[i] = ps
	}
	return out
}

// setPortals turns a group on or off as inua portals enable or disable
// does: the files, then the frontend restarted, and xdph for the share
// picker. Apps started from then on see it.
func (s *shellUI) setPortals(g portalGroup, on bool) {
	p := &s.settings.portals
	p.busy = true
	for _, sw := range p.switches {
		sw.SetSensitive(false)
	}
	go func() {
		err := applyPortals(g, on)
		states := readPortals()
		gtkx.Post(func() {
			p.busy = false
			if err != nil {
				log.Printf("settings: portals: %v", err)
				s.setStatus(i18n.Tf("Not saved: %s", err.Error()), true)
			} else {
				s.setStatus("", false)
			}
			s.fillPortals(states)
		})
	}()
}

func applyPortals(g portalGroup, on bool) error {
	d := portalconf.Default()
	left, err := d.Set(on, g.ifaces...)
	if err != nil {
		return err
	}
	if _, err := d.Apply(left); err != nil {
		return err
	}
	if slices.Contains(g.ifaces, portalconf.SharePicker) {
		return portalconf.RestartXdph()
	}
	return nil
}
