// Package filesui is inua-files' windows (docs/files.md): the sidebar of
// places, devices and remote disks, one or two panes of a folder as a
// list or a grid, the details of the selection, and the operations
// running. The logic is internal/apps/files; this is the view.
package filesui

import (
	"context"
	"log"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/apps/files/disks"
	"github.com/lyricat/inua/internal/apps/files/fm1"
	"github.com/lyricat/inua/internal/apps/files/remote"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// App is the file manager: one process, any number of windows.
type App struct {
	c        *appui.Ctx
	h        *hub
	settings *files.Settings
	wins     []*window
	ready    bool
	queued   []func() // D-Bus requests that came before the first window

	kinds  kinds
	icons  *themeIcons // the desktop's, for files, folders and places
	thumbs *thumbs
	counts *counts

	disks   *disks.Client // nil without the system bus
	volumes []disks.Volume
	mounts  []files.Mount
	remote  *remote.Engine
	secrets remote.Secrets
	keyring bool // a Secret Service is there to remember passwords in (until found not)

	users, groups map[uint32]string // owners' names, looked up once
	folders       map[string]string // folders' icons, by path

	clip []string // what this app last put on the clipboard
	cut  bool

	archiveExt  string               // the archive format chosen last (".zip")
	archiveOpts files.ArchiveOptions // and its options, never a password
	renameRule  files.RenameRule     // renaming many: the way used last
	undos       []files.Undo
}

// New makes the app; Register gives it to appui once it holds its name.
func New() *App {
	return &App{
		kinds: kinds{}, thumbs: newThumbs(), counts: newCounts(), users: map[uint32]string{}, groups: map[uint32]string{}, folders: map[string]string{},
		remote: remote.New(mountPoints), secrets: remote.NewSecrets(), keyring: true,
	}
}

// Register makes a the "files" light app, its every start its own: a
// holds its single process itself, through D-Bus (fm1).
func Register(a *App) {
	appui.RegisterMany("files", "Files", i18n.M("Files"), func() appui.View { return a })
}

// Build makes the first window; Open then says where it goes.
func (a *App) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.h = newHub()
	a.settings = files.LoadSettings()
	a.icons = newThemeIcons(c.DesktopIcons)
	w := a.newWindow()
	a.watchDevices()
	return &w.win
}

func (a *App) ThemeChanged() {}

// Open shows the arguments of the first start in the first window:
// folders (each after the first in a window of its own), files selected in
// their folders, home without any.
func (a *App) Open(args []string) {
	a.ready = true
	folders, items := fm1.Split(args)
	w := a.wins[0]
	switch {
	case len(folders) > 0:
		w.open(folders[0])
		a.ShowFoldersNow(folders[1:])
	case len(items) > 0:
		w.reveal(items)
		items = nil
	default:
		w.open(files.Home())
	}
	a.ShowItemsNow(items)
	for _, f := range a.queued {
		f()
	}
	a.queued = nil
}

func (a *App) whenReady(f func()) {
	gtkx.Post(func() {
		if a.ready {
			f()
		} else {
			a.queued = append(a.queued, f)
		}
	})
}

// The FileManager1 methods (fm1.Handler), called on D-Bus's goroutine.

func (a *App) ShowFolders(paths []string) { a.whenReady(func() { a.ShowFoldersNow(paths) }) }
func (a *App) ShowItems(paths []string)   { a.whenReady(func() { a.ShowItemsNow(paths) }) }
func (a *App) ShowItemProperties(paths []string) {
	a.whenReady(func() {
		for _, w := range a.ShowItemsNow(paths) {
			w.setDetails(true)
		}
	})
}

// ShowFoldersNow opens a window on each folder.
func (a *App) ShowFoldersNow(paths []string) {
	for _, p := range paths {
		w := a.newWindow()
		w.win.Present()
		w.open(p)
	}
}

// ShowItemsNow opens a window on each folder of the items, with them
// selected.
func (a *App) ShowItemsNow(paths []string) []*window {
	byDir := map[string][]string{}
	var order []string
	for _, p := range paths {
		d := filepath.Dir(p)
		if _, ok := byDir[d]; !ok {
			order = append(order, d)
		}
		byDir[d] = append(byDir[d], p)
	}
	var out []*window
	for _, d := range order {
		w := a.newWindow()
		w.win.Present()
		w.reveal(byDir[d])
		out = append(out, w)
	}
	return out
}

// closed forgets a window that went.
func (a *App) closed(w *window) {
	a.wins = slices.DeleteFunc(a.wins, func(x *window) bool { return x == w })
	a.settings.Save()
}

// folderIcon is a folder's icon, worked out once for each folder.
func (a *App) folderIcon(path string) string {
	if icon, ok := a.folders[path]; ok {
		return icon
	}
	icon := folderIcon(path)
	a.folders[path] = icon
	return icon
}

// --- devices ---

// watchDevices reads the drives and mounts, and follows them: UDisks2's
// signals, the mount table (which remote disks are in too).
func (a *App) watchDevices() {
	go func() {
		ok := a.secrets.Available()
		gtkx.Post(func() { a.keyring = ok })
	}()
	a.refreshMounts()
	if conn, err := dbus.ConnectSystemBus(); err == nil {
		a.disks = disks.New(conn)
		a.refreshVolumes()
		if err := a.disks.Watch(context.Background(), func() { gtkx.Post(a.refreshVolumes) }); err != nil {
			log.Printf("files: following UDisks2: %v", err)
		}
	} else {
		log.Printf("files: no system bus, no drives: %v", err)
	}
	if err := files.WatchMounts(context.Background(), func() { gtkx.Post(a.refreshMounts) }); err != nil {
		log.Printf("files: following mounts: %v", err)
	}
}

func (a *App) refreshVolumes() {
	if a.disks == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		vs, err := a.disks.Volumes(ctx)
		if err != nil {
			log.Printf("files: drives: %v", err)
		}
		gtkx.Post(func() {
			// A new removable volume that is not mounted is offered at once.
			old := map[dbus.ObjectPath]bool{}
			for _, v := range a.volumes {
				old[v.Path] = true
			}
			fresh := a.volumes != nil
			a.volumes = vs
			a.sidebarsChanged()
			if !fresh {
				return
			}
			for _, v := range vs {
				if !old[v.Path] && v.Drive.Removable && len(a.wins) > 0 {
					a.wins[0].flash(i18n.Tf("%s is plugged in.", v.Name()))
				}
			}
		})
	}()
}

func (a *App) refreshMounts() {
	go func() {
		ms, _ := files.ReadMounts()
		gtkx.Post(func() {
			a.mounts = ms
			a.sidebarsChanged()
		})
	}()
}

func (a *App) sidebarsChanged() {
	for _, w := range a.wins {
		w.buildSidebar()
	}
}

// --- the clipboard ---

// setClipboard puts paths on the clipboard as files, copied or cut.
func (a *App) setClipboard(w *window, paths []string, cut bool) {
	a.clip, a.cut = paths, cut
	gnome := []byte(files.GnomeCopied(paths, cut))
	uris := []byte(files.URIList(paths))
	text := []byte(strings.Join(paths, "\n"))
	providers := []uintptr{
		gdk.NewContentProviderForBytes("x-special/gnome-copied-files", glib.NewBytes(gnome, uint(len(gnome)))).GoPointer(),
		gdk.NewContentProviderForBytes("text/uri-list", glib.NewBytes(uris, uint(len(uris)))).GoPointer(),
		gdk.NewContentProviderForBytes("text/plain;charset=utf-8", glib.NewBytes(text, uint(len(text)))).GoPointer(),
	}
	union := gdk.NewContentProviderUnion(uintptr(unsafe.Pointer(&providers[0])), uint(len(providers)))
	clip := w.win.GetClipboard()
	clip.SetContent(union)
	clip.Unref()
	union.Unref()
	a.rebindAll()
}

// copyText puts text on the clipboard.
func (a *App) copyText(w *window, text string) {
	clip := w.win.GetClipboard()
	clip.SetText(text)
	clip.Unref()
	a.clip = nil
}

func (a *App) isCut(path string) bool { return a.cut && slices.Contains(a.clip, path) }

// paste copies (or moves, when they were cut) the files on the clipboard
// into dest: this app's own when it holds the clipboard, else what
// another file manager put there, read with wl-paste.
func (a *App) paste(w *window, dest string) {
	clip := w.win.GetClipboard()
	local := clip.IsLocal()
	clip.Unref()
	if local && len(a.clip) > 0 {
		a.pasteFiles(w, a.clip, a.cut, dest)
		return
	}
	go func() {
		var paths []string
		var cut bool
		for _, t := range []string{"x-special/gnome-copied-files", "text/uri-list", "text/plain"} {
			out, err := exec.Command("wl-paste", "--no-newline", "--type", t).Output()
			if err == nil && len(out) > 0 {
				if paths, cut = files.ParseClipboard(string(out)); len(paths) > 0 {
					break
				}
			}
		}
		gtkx.Post(func() {
			if len(paths) == 0 {
				w.flash(i18n.T("There are no files on the clipboard."))
				return
			}
			a.pasteFiles(w, paths, cut, dest)
		})
	}()
}

func (a *App) pasteFiles(w *window, paths []string, cut bool, dest string) {
	kind := files.Copy
	if cut {
		kind = files.Move
		// Moved once: the clipboard is spent, as in other file managers.
		a.clip, a.cut = nil, false
		clip := w.win.GetClipboard()
		clip.SetContent(&gdk.ContentProvider{})
		clip.Unref()
	}
	a.run(w, files.Job{Kind: kind, Sources: paths, Dest: dest})
}

// updateRows changes the rows on screen that show path, in every pane.
func (a *App) updateRows(path string, f func(*row)) {
	for _, w := range a.wins {
		for _, p := range w.p {
			p.updateRows(path, f)
		}
	}
}

// rebindAll binds every pane's rows again (the cut files dimmed, the
// zoom changed).
func (a *App) rebindAll() {
	for _, w := range a.wins {
		for _, p := range w.panes() {
			p.rebind()
		}
	}
}

// --- undo ---

func (a *App) pushUndo(u files.Undo) {
	if u.Empty() {
		return
	}
	a.undos = append(a.undos, u)
	if len(a.undos) > 50 {
		a.undos = a.undos[len(a.undos)-50:]
	}
}

func (a *App) undo(w *window) {
	if len(a.undos) == 0 {
		w.flash(i18n.T("Nothing to undo."))
		return
	}
	u := a.undos[len(a.undos)-1]
	a.undos = a.undos[:len(a.undos)-1]
	for _, j := range u.Job() {
		a.runQuiet(w, j)
	}
}
