package termui

import (
	"context"
	"errors"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/settings"
	"github.com/lyricat/inua/internal/term/webterm"
)

// The settings' Web Terminal: inua-term serve in its unit
// (internal/term/webterm), on and off, where it listens, and its link to
// copy or make anew. Starting it, stopping it and reading its link run
// off the main thread; what they say comes back to the section, an error
// under it.

type webPrefs struct {
	on    *gtk.Switch
	where *gtk.DropDown
	link  *gtk.Label
	copy  *gtk.Button
	renew *gtk.Button
	err   *gtk.Label
	// busy is set while a start or a stop runs: the controls wait.
	busy bool
	// setting is set while the switch is set from here, not by a click.
	setting bool
}

// webTimeout bounds a start: the keyring may ask, Tailscale may be slow.
const webTimeout = 30 * time.Second

func (pr *prefs) webSection(page *gtk.Box, s *settings.Settings, save func()) {
	w := &webPrefs{}
	sec := pr.section(page, i18n.T("WEB TERMINAL"), i18n.T("The terminals in a browser, a phone's over Tailscale"))

	w.on = gtk.NewSwitch()
	w.on.SetActive(s.Web.On)
	onToggle := func(uintptr, uintptr, uintptr) {
		if w.setting {
			return
		}
		s.Web.On = w.on.GetActive()
		save()
		w.apply(pr, s, save)
	}
	pr.cbs = append(pr.cbs, &onToggle)
	gtkx.ConnectNotify(w.on, "active", &onToggle)
	pr.row(sec, i18n.T("Web Terminal"), i18n.T("Serve the terminals to a browser; it starts again at login while on"), &w.on.Widget)

	// Each place with its address: the loopback, and this computer's on
	// the tailnet.
	tailnet := i18n.T("no address: is Tailscale up?")
	if ip, err := webterm.TailscaleIP(); err == nil {
		tailnet = ip
	}
	w.where = gtk.NewDropDownFromStrings([]string{
		i18n.T("This computer only") + " · 127.0.0.1",
		i18n.T("Tailscale") + " · " + tailnet,
	})
	if s.Web.Tailscale {
		w.where.SetSelected(1)
	}
	pr.selected(w.where, func(i uint) {
		if s.Web.Tailscale == (i == 1) {
			return
		}
		s.Web.Tailscale = i == 1
		save()
		if s.Web.On {
			w.apply(pr, s, save)
		}
	})
	pr.row(sec, i18n.T("Where"), i18n.T("Tailscale: on this computer's Tailscale address, for a phone on the tailnet"), &w.where.Widget)

	// The link on a line of its own under its row, the section's width:
	// in the row's control column its length pushed it past the window.
	w.link = gtk.NewLabel("")
	w.link.AddCssClass("mono-sub")
	w.link.AddCssClass("web-link")
	w.link.SetSelectable(true)
	w.link.SetXalign(0)
	w.link.SetWrap(true)
	w.link.SetWrapMode(2) // PANGO_WRAP_WORD_CHAR: the token has no spaces
	w.copy = gtk.NewButtonWithLabel(i18n.T("Copy"))
	w.copy.AddCssClass("outlined")
	pr.click(w.copy, func() {
		clip := w.link.GetClipboard()
		clip.SetText(w.link.GetText())
		clip.Unref()
	})
	w.renew = gtk.NewButtonWithLabel(i18n.T("New link"))
	w.renew.AddCssClass("outlined")
	pr.click(w.renew, func() { w.newLink(pr, s) })
	buttons := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	buttons.Append(&w.copy.Widget)
	buttons.Append(&w.renew.Widget)
	pr.row(sec, i18n.T("Link"), i18n.T("Open it on the phone; New link stops the old one working"), &buttons.Widget)
	sec.Append(&w.link.Widget)

	w.err = gtk.NewLabel("")
	w.err.AddCssClass("row-error")
	w.err.AddCssClass("flush")
	w.err.SetXalign(0)
	w.err.SetWrap(true)
	w.err.SetSelectable(true)
	w.err.SetVisible(false)
	sec.Append(&w.err.Widget)

	w.show(s, "")
	if s.Web.On {
		w.refresh(pr, s)
	}
}

// show sets the controls for the settings, the link (or what is being
// waited for), and the error; "" hides it.
func (w *webPrefs) show(s *settings.Settings, link string) {
	w.where.SetSensitive(!w.busy)
	w.on.SetSensitive(!w.busy)
	switch {
	case !s.Web.On:
		w.link.SetText(i18n.T("Off"))
	case link != "":
		w.link.SetText(link)
	}
	ready := s.Web.On && !w.busy && link != ""
	w.copy.SetSensitive(ready)
	w.renew.SetSensitive(ready)
}

func (w *webPrefs) fail(err error) {
	w.err.SetText(webError(err))
	w.err.SetVisible(err != nil)
}

// webError says what went wrong in the settings' words.
func webError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, webterm.ErrNoKeyring):
		return i18n.T("The link's token could be kept neither in the keyring nor in a file.")
	case errors.Is(err, webterm.ErrNoTailscale):
		return i18n.T("This computer has no Tailscale address: is Tailscale up? (tailscale up)")
	case errors.Is(err, context.DeadlineExceeded):
		return i18n.T("The Web Terminal did not start in time.")
	}
	return err.Error()
}

// apply starts or stops the Web Terminal as the settings say, the link
// shown once it listens. A start that fails turns it off again.
func (w *webPrefs) apply(pr *prefs, s *settings.Settings, save func()) {
	web := s.Web
	w.busy = true
	w.fail(nil)
	if web.On {
		w.link.SetText(i18n.T("Starting…"))
	}
	w.show(s, "")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), webTimeout)
		defer cancel()
		var err error
		var link string
		if web.On {
			if err = webterm.Start(ctx, web); err == nil {
				link, err = webterm.Link(ctx, web)
			}
		} else {
			webterm.Stop(ctx)
		}
		gtkx.Post(func() {
			w.busy = false
			if err != nil && web.On && s.Web.On {
				s.Web.On = false
				save()
				w.setting = true
				w.on.SetActive(false)
				w.setting = false
			}
			w.fail(err)
			w.show(s, link)
		})
	}()
}

// refresh shows the link of a Web Terminal that is on, and starts it if
// it is not running (stopped by hand, or before the session's login).
func (w *webPrefs) refresh(pr *prefs, s *settings.Settings) {
	web := s.Web
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), webTimeout)
		defer cancel()
		var err error
		if !webterm.Running(ctx) {
			err = webterm.Start(ctx, web)
		}
		var link string
		if err == nil {
			link, err = webterm.Link(ctx, web)
		}
		gtkx.Post(func() {
			w.fail(err)
			w.show(s, link)
		})
	}()
}

// newLink makes a new token and starts the Web Terminal again with it:
// links with the old one stop working.
func (w *webPrefs) newLink(pr *prefs, s *settings.Settings) {
	web := s.Web
	w.busy = true
	w.fail(nil)
	w.show(s, "")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), webTimeout)
		defer cancel()
		_, err := webterm.Token(ctx, true)
		if err == nil {
			err = webterm.Start(ctx, web)
		}
		var link string
		if err == nil {
			link, err = webterm.Link(ctx, web)
		}
		gtkx.Post(func() {
			w.busy = false
			w.fail(err)
			w.show(s, link)
		})
	}()
}
