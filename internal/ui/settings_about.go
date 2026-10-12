package ui

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/upgrade"
)

// aboutSettings is the About page: the version, whether a newer release
// is out (read when the page is first shown, and on Check for updates),
// Upgrade, the daily check's switch, and the latest release's notes.
type aboutSettings struct {
	version, state gtk.Label
	check, upgrade gtk.Button
	daily          gtk.Switch
	changelog      gtk.Box

	// What GitHub said, kept while the shell runs.
	latest    string
	releases  []upgrade.Release
	checked   time.Time
	checking  bool
	upgrading bool
	err       error
}

func (s *shellUI) getAboutSettings(ui *gtkx.Builder) {
	a := &s.settings.about
	ui.Get("about_version", &a.version)
	ui.Get("about_state", &a.state)
	ui.Get("about_check", &a.check)
	ui.Get("about_upgrade", &a.upgrade)
	ui.Get("about_daily", &a.daily)
	ui.Get("about_changelog", &a.changelog)
	a.check.ConnectClicked(&s.onSettingsButton)
	a.upgrade.ConnectClicked(&s.onSettingsButton)
	a.daily.ConnectStateSet(&s.onSettingsSwitch)
	s.onSettingsPage = func(uintptr, uintptr, uintptr) {
		if s.settings.pages.GetVisibleChildName() == "about" && s.settings.about.checked.IsZero() {
			s.checkReleases()
		}
	}
	gtkx.ConnectNotify(&s.settings.pages, "visible-child-name", &s.onSettingsPage)
}

// aboutButton handles the page's buttons; false for another page's.
func (s *shellUI) aboutButton(name string) bool {
	a := &s.settings.about
	switch name {
	case "about_check":
		s.checkReleases()
	case "about_upgrade":
		if a.latest == "" || a.upgrading {
			break
		}
		a.upgrading = true
		s.showAboutSettings()
		tag := a.latest
		go func() {
			s.runUpgrade(tag) // the shell restarts on the new version
			gtkx.Post(func() {
				a.upgrading = false
				s.showAboutSettings()
			})
		}()
	default:
		return false
	}
	return true
}

// checkReleases asks GitHub for the latest release and the notes of the
// last few.
func (s *shellUI) checkReleases() {
	a := &s.settings.about
	if a.checking {
		return
	}
	a.checking = true
	s.showAboutSettings()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		src := upgrade.GitHub()
		src.Client = &http.Client{}
		latest, err := src.Latest(ctx)
		var rels []upgrade.Release
		if err == nil {
			rels, err = src.Releases(ctx, 1)
		}
		gtkx.Post(func() {
			a.checking, a.checked, a.err = false, time.Now(), err
			if err != nil {
				log.Printf("about: %v", err)
			} else {
				a.latest, a.releases = latest, rels
			}
			if s.settings.win != nil {
				s.showAboutSettings()
			}
		})
	}()
}

// showAboutSettings fills the page from what is known.
func (s *shellUI) showAboutSettings() {
	a := &s.settings.about
	if s.settings.win == nil {
		return
	}
	v := s.opts.Version
	order, known := upgrade.Compare(v, a.latest)
	if _, release := upgrade.Compare(v, v); release {
		a.version.SetText("inua " + strings.TrimPrefix(v, "v"))
	} else {
		a.version.SetText(fmt.Sprintf("inua %s, built from source", v))
	}
	a.daily.SetActive(s.cfg.Notify.Updates)
	a.check.SetSensitive(!a.checking && !a.upgrading)
	// A build from source can take the release too (inua upgrade -version).
	a.upgrade.SetVisible(a.latest != "" && (!known || order < 0))
	a.upgrade.SetSensitive(!a.upgrading)
	a.upgrade.SetLabel("Upgrade to " + strings.TrimPrefix(a.latest, "v"))
	if !known {
		a.upgrade.SetLabel("Install " + a.latest)
	}
	switch {
	case a.upgrading:
		a.state.SetText("Upgrading… the shell restarts on the new version")
	case a.checking:
		a.state.SetText("Checking…")
	case a.err != nil:
		a.state.SetText("Could not reach GitHub: " + a.err.Error())
	case a.latest == "":
		a.state.SetText("Not checked yet")
	case !known:
		a.state.SetText(fmt.Sprintf("The latest release is %s. Installing it replaces this build from source; git pull and make install bring a build from source back.", a.latest))
	case order < 0:
		a.state.SetText(fmt.Sprintf("inua %s is out", strings.TrimPrefix(a.latest, "v")))
	case order == 0:
		a.state.SetText(fmt.Sprintf("Up to date: %s is the latest release (checked %s)", a.latest, a.checked.Format("15:04")))
	default:
		a.state.SetText(fmt.Sprintf("Newer than the latest release, %s", a.latest))
	}
	s.showChangelog()
}

// showChangelog lists the latest release's changes.
func (s *shellUI) showChangelog() {
	a := &s.settings.about
	gtkx.Clear(&a.changelog)
	if len(a.releases) == 0 {
		l := gtk.NewLabel("Shown once the releases are read")
		if a.checking {
			l.SetText("Reading the releases…")
		}
		l.AddCssClass("sub")
		l.SetXalign(0)
		gtkx.Append(&a.changelog, &l.Widget)
		return
	}
	for _, r := range a.releases[:1] {
		box := gtk.NewBox(gtk.OrientationVerticalValue, 4)
		box.AddCssClass("row")
		box.AddCssClass("changelog-release")
		head := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		tag := gtk.NewLabel(r.Tag)
		tag.AddCssClass("title")
		gtkx.Append(head, &tag.Widget)
		date := gtk.NewLabel(r.Date.Local().Format("Jan 2, 2006"))
		date.AddCssClass("readout")
		date.SetHexpand(true)
		date.SetXalign(0)
		gtkx.Append(head, &date.Widget)
		if r.Tag == s.opts.Version || "v"+s.opts.Version == r.Tag {
			now := gtk.NewLabel("installed")
			now.AddCssClass("tag")
			gtkx.Append(head, &now.Widget)
		}
		link := gtk.NewButtonWithLabel("On GitHub")
		link.AddCssClass("link")
		link.SetName("about_url:" + r.URL)
		link.ConnectClicked(&s.onAboutLink)
		gtkx.Append(head, &link.Widget)
		gtkx.Append(box, &head.Widget)
		for _, c := range upgrade.Changes(r.Body) {
			l := gtk.NewLabel("•  " + c)
			l.AddCssClass("sub")
			l.SetXalign(0)
			l.SetWrap(true)
			l.SetSelectable(true)
			gtkx.Append(box, &l.Widget)
		}
		gtkx.Append(&a.changelog, &box.Widget)
	}
}

func (s *shellUI) bindAboutHandlers() {
	s.onAboutLink = func(b gtk.Button) {
		url := strings.TrimPrefix(b.GetName(), "about_url:")
		go func() {
			if err := exec.Command("xdg-open", url).Run(); err != nil {
				log.Printf("about: %v", err)
			}
		}()
	}
}
