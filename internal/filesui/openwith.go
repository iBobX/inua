package filesui

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/i18n"
)

// Files open with the app chosen for their type (their MIME type, from the
// name and the first bytes). The choice is the desktop's, kept by GIO in
// ~/.config/mimeapps.list, so the shell's launcher, the browser and other
// file managers agree; choosing an app in Open With remembers it for the
// type. The app is started through its desktop entry with the files in
// its Exec (desktop.ExecFiles).

// mimeOf is a file's content type, guessed from its name and first bytes.
func mimeOf(path string) string {
	var head []byte
	if f, err := os.Open(path); err == nil {
		buf := make([]byte, 4096)
		n, _ := io.ReadFull(f, buf)
		f.Close()
		head = buf[:n]
	}
	var uncertain bool
	return gio.ContentTypeGuess(filepath.Base(path), head, uint(len(head)), &uncertain)
}

// app is an app that opens a type.
type app struct {
	id, name string
	icon     *gio.IconBase
}

// appsFor are the apps for a type: its default first, then those that say
// they open it (the last used first), then, with all, every other app.
func appsFor(mime string, all bool) []app {
	var out []app
	seen := map[string]bool{}
	add := func(a *gio.AppInfoBase) {
		if a == nil || a.Ptr == 0 {
			return
		}
		id := a.GetId()
		if id == "" || seen[id] || !a.ShouldShow() {
			return
		}
		seen[id] = true
		out = append(out, app{id: id, name: a.GetDisplayName(), icon: a.GetIcon()})
	}
	add(gio.AppInfoGetDefaultForType(mime, false))
	walk := func(l *glib.List) {
		for ; l != nil; l = l.Next {
			add(&gio.AppInfoBase{Ptr: l.Data})
		}
	}
	walk(gio.AppInfoGetRecommendedForType(mime))
	walk(gio.AppInfoGetAllForType(mime))
	if all {
		var rest []app
		n := len(out)
		walk(gio.AppInfoGetAll())
		rest = out[n:]
		slices.SortFunc(rest, func(a, b app) int { return strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name)) })
	}
	return out
}

// launch opens files with the apps chosen for their types, asking (Open
// With) for a type that has none, or for the first file's when ask.
func (w *window) launch(paths []string, ask bool) {
	byType := map[string][]string{}
	var order []string
	for _, p := range paths {
		m := mimeOf(p)
		if _, ok := byType[m]; !ok {
			order = append(order, m)
		}
		byType[m] = append(byType[m], p)
	}
	for _, m := range order {
		if !ask {
			if a := gio.AppInfoGetDefaultForType(m, false); a != nil && a.Ptr != 0 {
				w.openWithApp(a.GetId(), byType[m])
				continue
			}
		}
		w.openWith(byType[m], m)
		return // one question at a time
	}
}

// openWithApp starts the app with desktop ID id on paths.
func (w *window) openWithApp(id string, paths []string) {
	e, err := desktop.Find(desktop.DataDirs(), id, "")
	if err != nil {
		w.flash(i18n.Tf("%s could not be opened: %v", filepath.Base(paths[0]), err))
		return
	}
	cmds, err := desktop.ExecFiles(e.ExecString, e, paths)
	if err != nil {
		w.flash(i18n.Tf("%s could not be opened: %v", filepath.Base(paths[0]), err))
		return
	}
	dir := e.WorkingDir
	if dir == "" {
		dir = filepath.Dir(paths[0])
	}
	for _, argv := range cmds {
		if e.RunInTerminal {
			argv = append([]string{"xdg-terminal-exec"}, argv...)
		}
		if err := desktop.Spawn(argv, dir); err != nil {
			w.flash(i18n.Tf("%s could not be opened: %v", filepath.Base(paths[0]), err))
			return
		}
	}
}

// openWith asks which app opens paths, of type mime: the apps for the
// type, the one chosen kept for it unless that is turned off; typing finds
// any app.
func (w *window) openWith(paths []string, mime string) {
	desc := mimeDesc(mime)
	c := &card{def: -1}
	fill := func(q string) {
		list := appsFor(mime, q != "")
		if q != "" {
			list = files.Rank(list, q, func(a app) string { return a.name + " " + a.id })
		}
		var ids, names, subs []string
		var icons []*gio.IconBase
		for _, a := range list {
			ids, names = append(ids, a.id), append(names, a.name)
			subs = append(subs, strings.TrimSuffix(a.id, ".desktop"))
			icons = append(icons, a.icon)
		}
		w.setCardList(ids, names, subs, icons...)
	}
	c.edited = fill
	c.picked = func(id string) {
		if info := appInfo(id); info != nil {
			if w.cardCheck.GetActive() {
				info.SetAsDefaultForType(mime)
			} else {
				info.SetAsLastUsedForType(mime)
			}
		}
		w.openWithApp(id, paths)
	}
	title := i18n.Tf("Open “%s” with", filepath.Base(paths[0]))
	if len(paths) > 1 {
		title = i18n.Nf("Open %d file with", "Open %d files with", len(paths), len(paths))
	}
	w.openCard(c, cardParts{
		title: title, entry: true, list: true,
		check:   i18n.Tf("Always open %s with it", desc),
		checked: true,
	})
	w.cardEntry.SetPlaceholderText(i18n.T("Find an app"))
	fill("")
}

// appInfo is the GIO app with desktop ID id.
func appInfo(id string) *gio.AppInfoBase {
	for l := gio.AppInfoGetAll(); l != nil; l = l.Next {
		a := &gio.AppInfoBase{Ptr: l.Data}
		if a.GetId() == id {
			return a
		}
	}
	return nil
}

func mimeDesc(mime string) string {
	if d := gio.ContentTypeGetDescription(mime); d != "" {
		return d
	}
	return mime
}
