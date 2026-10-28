package lightui

import (
	"errors"
	"log"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/workshop"
)

// The Workshop app: the workshop apps installed, to open or remove, and
// how to add more; drawn with the blocks the apps themselves use.

type workshopManager struct {
	note     string // what the last action did, or why it failed
	bad      bool
	dialog   *gtk.FileDialog
	onChosen gio.AsyncReadyCallback
}

// manage shows the installed apps.
func (a *workshopApp) manage() {
	a.win.SetDefaultSize(560, 620)
	a.mg.onChosen = func(_, result, _ uintptr) {
		d := a.mg.dialog
		a.mg.dialog = nil
		if d == nil {
			return
		}
		defer d.Unref()
		f, err := d.SelectFolderFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || f == nil {
			return // cancelled
		}
		dir := f.GetPath()
		unrefObject(f)
		a.installFrom(dir)
	}
	a.drawManager()
}

func (a *workshopApp) drawManager() {
	apps, broken := workshop.Installed()
	installed := workshop.Node{Type: "section", Title: i18n.T("Installed"), Meta: i18n.Nf("%d app", "%d apps", len(apps), len(apps))}
	for _, m := range apps {
		sub := m.ID
		if m.Version != "" {
			sub = m.Version + " · " + sub
		}
		if m.Description != "" {
			sub += "\n" + m.Description
		}
		if len(m.Uses) > 0 {
			sub += "\n" + i18n.Tf("Uses: %s", strings.Join(m.Uses, ", "))
		}
		row := workshop.Node{Type: "row", Title: m.Name, Sub: sub, Icon: m.IconPath(), Children: []workshop.Node{
			{Type: "button", ID: "open:" + m.ID, Label: i18n.T("Open")},
			{Type: "button", ID: "remove:" + m.ID, Icon: "user-trash-symbolic", Style: "plain", Sub: i18n.T("Remove…")},
		}}
		installed.Children = append(installed.Children, row)
	}
	if len(apps) == 0 {
		installed.Children = append(installed.Children, workshop.Node{Type: "empty", Icon: "application-x-executable-symbolic",
			Title: i18n.T("No workshop apps yet"), Text: i18n.T("Install one below, or write your own.")})
	}
	page := workshop.Node{Type: "page"}
	if a.mg.note != "" {
		style := "sub"
		if a.mg.bad {
			style = "title"
		}
		page.Children = append(page.Children, workshop.Node{Type: "text", Text: a.mg.note, Style: style})
	}
	page.Children = append(page.Children, installed)
	if len(broken) > 0 {
		sec := workshop.Node{Type: "section", Title: i18n.T("Broken"), Meta: i18n.T("Their app.toml could not be read")}
		for dir, err := range broken {
			sec.Children = append(sec.Children, workshop.Node{Type: "row", Title: dir, Sub: err.Error()})
		}
		page.Children = append(page.Children, sec)
	}
	page.Children = append(page.Children, workshop.Node{Type: "section", Title: i18n.T("Add"), Children: []workshop.Node{
		{Type: "row", Title: i18n.T("From a folder"), Sub: i18n.T("A folder with an app.toml; it is copied."),
			Children: []workshop.Node{{Type: "button", ID: "install-folder", Label: i18n.T("Choose…")}}},
		{Type: "row", Title: i18n.T("From git"), Sub: i18n.T("In a terminal: inua workshop install URL")},
		{Type: "row", Title: i18n.T("Write your own"), Sub: i18n.T("inua workshop new DIR makes one; inua workshop dev DIR runs it, again on every save. The guide is docs/workshop.md in inua.")},
	}})
	a.render(page)
}

// manageEvent is a click in the Workshop app.
func (a *workshopApp) manageEvent(kind, id string) {
	if kind != "click" {
		return
	}
	verb, appID, _ := strings.Cut(id, ":")
	switch verb {
	case "open":
		if err := desktop.Spawn([]string{"inua-apps", "workshop", appID}, ""); err != nil {
			a.mg.note, a.mg.bad = err.Error(), true
			a.drawManager()
		}
	case "remove":
		m, err := workshop.Find(appID)
		if err != nil {
			return
		}
		answer := make(chan bool, 1)
		a.confirm(i18n.Tf("Remove %s?", m.Name), i18n.T("Its folder and launcher entry go to the trash; its data stays."), i18n.T("Remove"), true, answer)
		go func() {
			if !<-answer {
				return
			}
			_, err := workshop.Remove(appID)
			gtkx.Post(func() {
				a.mg.note, a.mg.bad = i18n.Tf("Removed %s.", m.Name), false
				if err != nil {
					a.mg.note, a.mg.bad = err.Error(), true
				}
				a.drawManager()
			})
		}()
	case "install-folder":
		if a.mg.dialog != nil {
			return
		}
		d := gtk.NewFileDialog()
		d.SetTitle(i18n.T("A workshop app: a folder with an app.toml"))
		a.mg.dialog = d
		d.SelectFolder(&a.win.Window, nil, &a.mg.onChosen, 0)
	}
}

// installFrom installs the app in dir, asking first with what it uses.
func (a *workshopApp) installFrom(dir string) {
	go func() {
		m, err := workshop.Install(dir, func(m workshop.Manifest, replaces bool) bool {
			uses := i18n.T("nothing beyond what every app has")
			if len(m.Uses) > 0 {
				uses = strings.Join(m.Uses, ", ")
			}
			text := i18n.Tf("It runs %s, as you, with your files, like any program you start. It uses: %s.", strings.Join(m.Run, " "), uses)
			title := i18n.Tf("Install %s?", m.Name)
			if replaces {
				title = i18n.Tf("Replace the installed %s?", m.Name)
			}
			answer := make(chan bool, 1)
			gtkx.Post(func() { a.confirm(title, text, i18n.T("Install"), false, answer) })
			return <-answer
		})
		gtkx.Post(func() {
			switch {
			case errors.Is(err, workshop.ErrRefused):
				return
			case err != nil:
				a.mg.note, a.mg.bad = err.Error(), true
			default:
				a.mg.note, a.mg.bad = i18n.Tf("Installed %s; the launcher and the dock's drawer list it.", m.Name), false
			}
			log.Printf("workshop: %s", a.mg.note)
			a.drawManager()
		})
	}()
}
