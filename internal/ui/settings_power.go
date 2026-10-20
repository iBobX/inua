package ui

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// Settings → Power: the battery and the power mode (UPower and
// power-profiles-daemon, as the bar and the control center read them), the
// idle timers (hypridle's config, which the page reads and writes), and
// what the lid and the power key do (logind's, written as a drop-in through
// pkexec). The sections are made once, when the window is; showPower sets
// them from what was read.

type powerSettings struct {
	page                gtk.Box
	battery, mode       *gtk.Box // their rows, hidden without one
	batteryText         *gtk.Label
	modeDrop            *gtk.DropDown
	modeValues          []string
	idleDrops           [4]*gtk.DropDown
	idleValues          [4][]int
	idleNote            *gtk.Label
	buttonDrops         [3]*gtk.DropDown
	buttonValues        [3][]string
	buttonsNote         *gtk.Label
	buttonsSec, idleSec *gtk.Box

	// What was read, and whether it was: the timers from hypridle.conf,
	// logind's settings, whether this computer can hibernate.
	idle        shell.Idle
	idleRead    bool
	hypridle    bool // installed
	buttons     system.Buttons
	buttonsRead bool
	buttonsErr  string
	hibernate   bool
	applying    bool // a change of logind's being written
	idleErr     string
	drops       map[uintptr]powerDrop
}

// powerDrop is what a drop-down of the page sets: "mode", "idle" (the
// timer at index) or "button" (the button at index).
type powerDrop struct {
	kind  string
	index int
}

// hypridleConf is hypridle's config file.
func hypridleConf() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "hypr", "hypridle.conf")
}

// getPowerSettings makes the page's sections in its box.
func (s *shellUI) getPowerSettings(ui *gtkx.Builder) {
	p := &s.settings.power
	ui.Get("power_page", &p.page)
	p.drops = map[uintptr]powerDrop{}
	if s.onPowerDrop == nil {
		s.onPowerDrop = func(ptr, _, _ uintptr) {
			d, ok := s.settings.power.drops[ptr]
			if ok && !s.settings.filling {
				s.powerChanged(d, int(gtk.DropDownNewFromInternalPtr(ptr).GetSelected()))
			}
		}
	}
	drop := func(body *gtk.Box, title, sub string, d powerDrop) (*gtk.DropDown, *gtk.Box) {
		if body.GetFirstChild() != nil {
			gtkx.Append(body, &gtk.NewSeparator(gtk.OrientationHorizontalValue).Widget)
		}
		row, _ := displayRow(title, sub)
		dd := gtk.NewDropDownFromStrings(nil)
		dd.SetValign(gtk.AlignCenterValue)
		p.drops[dd.GoPointer()] = d
		gtkx.ConnectNotify(dd, "selected", &s.onPowerDrop)
		gtkx.Append(row, &dd.Widget)
		gtkx.Append(body, &row.Widget)
		return dd, row
	}
	note := func(body *gtk.Box) *gtk.Label {
		l := gtk.NewLabel("")
		l.AddCssClass("row-error")
		l.AddCssClass("flush") // the rows have no icon to line up under
		l.SetXalign(0)
		l.SetWrap(true)
		l.SetSelectable(true)
		l.SetVisible(false)
		gtkx.Append(body, &l.Widget)
		return l
	}

	sec, body := displaySection(i18n.T("POWER"), i18n.T("The battery and how the computer runs"))
	var row *gtk.Box
	row, p.batteryText = displayRow(i18n.T("Battery"), " ")
	p.battery = row
	gtkx.Append(body, &row.Widget)
	p.modeDrop, p.mode = drop(body, i18n.T("Power mode"), i18n.T("Saves the battery, or runs faster for more of it"), powerDrop{kind: "mode"})
	gtkx.Append(&p.page, &sec.Widget)

	p.idleSec, body = displaySection(i18n.T("WHEN IDLE"), i18n.T("After no keys and no pointer for a while"))
	for i, title := range []string{i18n.T("Dim the screen after"), i18n.T("Lock after"), i18n.T("Turn the screens off after"), i18n.T("Suspend after")} {
		p.idleDrops[i], _ = drop(body, title, "", powerDrop{kind: "idle", index: i})
	}
	p.idleNote = note(body)
	gtkx.Append(&p.page, &p.idleSec.Widget)

	p.buttonsSec, body = displaySection(i18n.T("LID AND POWER BUTTON"), i18n.T("Changing these asks for an administrator's password"))
	for i, title := range []string{i18n.T("Closing the lid on battery"), i18n.T("Closing the lid on mains power"), i18n.T("Pressing the power button")} {
		p.buttonDrops[i], _ = drop(body, title, "", powerDrop{kind: "button", index: i})
	}
	p.buttonsNote = note(body)
	gtkx.Append(&p.page, &p.buttonsSec.Widget)
	// The drop-downs are new and empty: showPower gives them their choices.
	p.modeValues, p.idleValues, p.buttonValues = nil, [4][]int{}, [3][]string{}
}

// readPower reads, off the main thread, what the page shows that is not
// followed already: hypridle's timers, logind's buttons, hibernation.
func (s *shellUI) readPower() {
	go func() {
		var idle shell.Idle
		var idleErr string
		data, err := os.ReadFile(hypridleConf())
		switch {
		case err == nil:
			idle = shell.ParseIdle(string(data))
		case errors.Is(err, os.ErrNotExist):
			idle = shell.IdleDefaults // nothing yet: what the page would write
		default:
			idleErr = err.Error()
		}
		_, lookErr := exec.LookPath("hypridle")
		buttons, bErr := system.ReadButtons(system.SystemBus)
		hibernate, _ := system.CanHibernate(system.SystemBus)
		gtkx.Post(func() {
			p := &s.settings.power
			p.idle, p.idleRead, p.idleErr = idle, idleErr == "", idleErr
			p.hypridle = lookErr == nil
			p.buttons, p.buttonsRead = buttons, bErr == nil
			if bErr != nil {
				p.buttonsErr = bErr.Error()
			}
			p.hibernate = hibernate
			s.showPower()
		})
	}()
}

// showPower sets the page from what was read and what the shell follows.
func (s *shellUI) showPower() {
	st := &s.settings
	p := &st.power
	if st.win == nil || p.modeDrop == nil {
		return
	}
	st.filling = true
	defer func() { st.filling = false }()

	b := s.sys.Bat
	p.battery.SetVisible(b.Present)
	if b.Present {
		p.batteryText.SetText(shell.BatteryText(b))
	}
	prof := s.sys.Prof
	p.mode.SetVisible(prof.Available && len(prof.Profiles) > 0)
	if prof.Available && !slices.Equal(prof.Profiles, p.modeValues) {
		p.modeValues = slices.Clone(prof.Profiles)
		labels := make([]string, len(p.modeValues))
		for i, v := range p.modeValues {
			labels[i] = shell.ProfileLabel(v)
		}
		setChoices(p.modeDrop, labels)
	}
	if i := slices.Index(p.modeValues, prof.Active); i >= 0 {
		p.modeDrop.SetSelected(uint32(i))
	}

	// The timers: each its choices, and what hypridle.conf says when that
	// is not one of them (2.5 minutes, say).
	timers := []int{p.idle.Dim, p.idle.Lock, p.idle.Off, p.idle.Suspend}
	for i, dd := range p.idleDrops {
		values := slices.Clone(shell.IdleChoices)
		if !slices.Contains(values, timers[i]) {
			values = append(values, timers[i])
			slices.Sort(values)
		}
		if !slices.Equal(values, p.idleValues[i]) {
			p.idleValues[i] = values
			labels := make([]string, len(values))
			for j, v := range values {
				labels[j] = shell.IdleLabel(v)
			}
			setChoices(dd, labels)
		}
		dd.SetSelected(uint32(slices.Index(values, timers[i])))
		dd.SetSensitive(p.idleRead)
	}
	switch {
	case p.idleErr != "":
		p.idleNote.SetText(i18n.Tf("%s cannot be read: %s", hypridleConf(), p.idleErr))
	case !p.hypridle:
		p.idleNote.SetText(i18n.T("hypridle is not installed, so nothing happens when the computer is idle: install it (sudo apt install hypridle) and log in again."))
	default:
		p.idleNote.SetText("")
	}
	p.idleNote.SetVisible(p.idleNote.GetText() != "")

	// The buttons: the actions offered, hibernation only where it works,
	// and logind's own when it is none of them.
	p.buttonsSec.SetVisible(p.buttonsRead || p.buttonsErr != "")
	have := []string{p.buttons.Lid, p.buttons.LidAC, p.buttons.Power}
	for i, dd := range p.buttonDrops {
		values := []string{}
		for _, a := range system.ButtonActions {
			if a != "hibernate" || p.hibernate || have[i] == a {
				values = append(values, a)
			}
		}
		if have[i] != "" && !slices.Contains(values, have[i]) {
			values = append(values, have[i])
		}
		if !slices.Equal(values, p.buttonValues[i]) {
			p.buttonValues[i] = values
			labels := make([]string, len(values))
			for j, v := range values {
				labels[j] = shell.ButtonLabel(v)
			}
			setChoices(dd, labels)
		}
		dd.SetSelected(uint32(max(0, slices.Index(values, have[i]))))
		dd.SetSensitive(p.buttonsRead && !p.applying)
	}
	switch {
	case p.applying:
		p.buttonsNote.SetText(i18n.T("Asking for the password…"))
	default:
		p.buttonsNote.SetText(p.buttonsErr)
	}
	p.buttonsNote.SetVisible(p.buttonsNote.GetText() != "")
}

// powerChanged follows a drop-down of the page: index is what it now has
// selected.
func (s *shellUI) powerChanged(d powerDrop, index int) {
	p := &s.settings.power
	switch d.kind {
	case "mode":
		if index < 0 || index >= len(p.modeValues) {
			return
		}
		target := p.modeValues[index]
		go func() { logErr("power profile", s.svc.prof.SetProfile(target)) }()
	case "idle":
		values := p.idleValues[d.index]
		if index < 0 || index >= len(values) || !p.idleRead {
			return
		}
		idle := p.idle
		*[]*int{&idle.Dim, &idle.Lock, &idle.Off, &idle.Suspend}[d.index] = values[index]
		p.idle = idle
		go func() {
			err := writeIdle(idle)
			gtkx.Post(func() {
				if err != nil {
					s.settings.power.idleErr = err.Error()
				}
				s.showPower()
			})
		}()
	case "button":
		values := p.buttonValues[d.index]
		if index < 0 || index >= len(values) || p.applying {
			return
		}
		b := p.buttons
		*[]*string{&b.Lid, &b.LidAC, &b.Power}[d.index] = values[index]
		if b == p.buttons {
			return
		}
		p.applying, p.buttonsErr = true, ""
		s.showPower()
		go func() {
			err := system.ApplyButtons(b)
			gtkx.Post(func() {
				p := &s.settings.power
				p.applying = false
				switch {
				case errors.Is(err, system.ErrNotAuthorized):
					p.buttonsErr = i18n.T("Not changed: the administrator's password was not given.")
				case err != nil:
					p.buttonsErr = err.Error()
				}
				// What logind has now, changed or not.
				s.readPower()
			})
		}()
	}
}

// writeIdle writes hypridle's config for these timers and starts hypridle
// again to take them. The file the page first replaces is kept beside it
// as hypridle.conf.pre-inua.
func writeIdle(idle shell.Idle) error {
	path := hypridleConf()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if old, err := os.ReadFile(path); err == nil {
		backup := path + ".pre-inua"
		if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(backup, old, 0o644); err != nil {
				return fmt.Errorf("keep %s: %w", backup, err)
			}
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(idle.Conf()), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return restartHypridle()
}

// restartHypridle starts hypridle again, under the compositor as the
// session starts it, so it reads its config anew; a hypridle not running
// is started.
func restartHypridle() error {
	if _, err := exec.LookPath("hypridle"); err != nil {
		return nil // not installed: the page says so
	}
	_ = exec.Command("pkill", "-x", "hypridle").Run()
	out, err := exec.Command("hyprctl", "dispatch", "exec", "hypridle").CombinedOutput()
	if err != nil {
		log.Printf("hypridle: %v: %s", err, out)
		return fmt.Errorf("hypridle could not be started again: %v", err)
	}
	return nil
}

// setChoices gives a drop-down these labels to choose from.
func setChoices(dd *gtk.DropDown, labels []string) {
	list := gtk.NewStringList(labels)
	dd.SetModel(list)
	list.Unref()
}
