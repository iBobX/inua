package filesui

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// showDetails fills the details panel for the pane with the keys: its
// selection, or the folder itself when nothing is selected. The facts
// that take the disk (a stat, a text's first lines, a folder's size) are
// read on a goroutine.
func (w *window) showDetails() {
	if !w.detailsReveal.GetRevealChild() {
		return
	}
	w.detGen++
	gen := w.detGen
	p := w.cur()
	sel := p.selected()
	gtkx.Clear(&w.details)

	switch {
	case len(sel) > 1:
		var size int64
		dirs := 0
		for _, i := range sel {
			if p.all[i].Dir {
				dirs++
			} else {
				size += p.all[i].Size
			}
		}
		w.detailIcon("edit-copy")
		w.detailTitle(i18n.Nf("%d item", "%d items", len(sel), len(sel)))
		w.detailFact(i18n.T("FOLDERS"), strconv.Itoa(dirs))
		w.detailFact(i18n.T("FILES"), strconv.Itoa(len(sel)-dirs))
		w.detailFact(i18n.T("SIZE OF THE FILES"), sizeText(size))
		return
	case len(sel) == 0 && p.mode != inFolder:
		w.detailTitle(map[mode]string{inTrash: i18n.T("Trash"), inSearch: i18n.T("Search")}[p.mode])
		w.detailFact(i18n.T("ITEMS"), strconv.Itoa(len(p.shown)))
		return
	}

	var path string
	var e files.Entry
	if len(sel) == 1 {
		e, path = p.all[sel[0]], p.path(sel[0])
	} else {
		path = p.dir
		e, _ = files.Stat(path)
	}
	k := w.app.kinds.of(&e)
	tex := w.app.thumbs.get(path, &e, p)
	if tex != nil {
		pic := gtk.NewPicture()
		pic.SetPaintable(tex)
		pic.SetCanShrink(true)
		pic.SetContentFit(gtk.ContentFitContainValue)
		pic.AddCssClass("files-preview")
		pic.SetSizeRequest(-1, 200)
		gtkx.Append(&w.details, &pic.Widget)
	} else {
		img := w.detailIcon("")
		if e.Dir {
			w.app.icons.set(img, w.app.folderIcon(path), 72)
		} else {
			w.app.icons.setKind(img, k, 72)
		}
	}
	w.detailTitle(e.Name)
	if p.mode == inTrash && len(sel) == 1 {
		t := p.trash[sel[0]]
		w.detailFact(i18n.T("FROM"), shortHome(filepath.Dir(t.Orig)))
		w.detailFact(i18n.T("TRASHED"), dateText(t.Deleted))
	}
	w.detailFact(i18n.T("KIND"), k.desc)
	if e.Link != "" {
		w.detailFact(i18n.T("LINKS TO"), e.Link)
	}
	sizeLabel := w.detailFact(i18n.T("SIZE"), "…")
	if !e.Dir {
		sizeLabel.SetText(sizeText(e.Size) + " (" + i18n.Nf("%s byte", "%s bytes", int(e.Size), groupDigits(e.Size)) + ")")
	}
	w.detailFact(i18n.T("MODIFIED"), longDate(e.ModTime))
	extra := w.detailBox()
	where := w.detailFact(i18n.T("WHERE"), shortHome(filepath.Dir(path)))
	where.SetSelectable(true)
	var preview *gtk.Label
	if !e.Dir && gio.ContentTypeIsA(k.mime, "text/plain") {
		preview = gtk.NewLabel("")
		preview.AddCssClass("files-text-preview")
		preview.SetXalign(0)
		preview.SetYalign(0)
		preview.SetWrap(true)
		preview.SetWrapMode(2)
		preview.SetSelectable(true)
		preview.SetVisible(false)
		gtkx.Append(&w.details, &preview.Widget)
	}

	go func() {
		var facts [][2]string
		fi, err := os.Lstat(path)
		if err == nil {
			atime, ctime := files.Times(fi)
			facts = append(facts, [2]string{i18n.T("OPENED"), longDate(atime)}, [2]string{i18n.T("CHANGED"), longDate(ctime)})
			facts = append(facts, [2]string{i18n.T("PERMISSIONS"), permText(fi.Mode())})
			if uid, gid, ok := files.Owner(fi); ok {
				facts = append(facts, [2]string{i18n.T("OWNER"), ownerText(uid, gid)})
			}
		}
		size := ""
		if e.Dir {
			size = folderSize(path, func() bool { return gen != w.detGen })
		}
		head := ""
		if preview != nil && e.Size > 0 {
			head = textHead(path)
		}
		gtkx.Post(func() {
			if gen != w.detGen {
				return
			}
			if e.Dir {
				sizeLabel.SetText(size)
			}
			for _, f := range facts {
				w.detailFactIn(extra, f[0], f[1])
			}
			if head != "" {
				preview.SetText(head)
				preview.SetVisible(true)
			}
		})
	}()
}

// detailIcon adds the panel's big icon, named or set later, and returns
// it.
func (w *window) detailIcon(icon string) *gtk.Image {
	img := gtk.NewImageFromIconName(icon)
	img.SetPixelSize(72)
	img.AddCssClass("files-details-icon")
	gtkx.Append(&w.details, &img.Widget)
	return img
}

func (w *window) detailTitle(t string) {
	l := gtk.NewLabel(t)
	l.AddCssClass("files-details-name")
	l.SetXalign(0)
	l.SetWrap(true)
	l.SetWrapMode(2)
	l.SetSelectable(true)
	gtkx.Append(&w.details, &l.Widget)
}

func (w *window) detailBox() *gtk.Box {
	b := gtk.NewBox(gtk.OrientationVerticalValue, 12)
	gtkx.Append(&w.details, &b.Widget)
	return b
}

// detailFact adds a fact: its name in small capitals over its value; the
// value's label is returned for filling later.
func (w *window) detailFact(name, value string) *gtk.Label {
	return w.detailFactIn(&w.details, name, value)
}

func (w *window) detailFactIn(box *gtk.Box, name, value string) *gtk.Label {
	b := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	k := gtk.NewLabel(name)
	k.AddCssClass("kicker")
	k.SetXalign(0)
	gtkx.Append(b, &k.Widget)
	v := gtk.NewLabel(value)
	v.AddCssClass("files-details-value")
	v.SetXalign(0)
	v.SetWrap(true)
	v.SetWrapMode(2)
	gtkx.Append(b, &v.Widget)
	gtkx.Append(box, &b.Widget)
	return v
}

func longDate(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return i18n.Time(t, "Jan 2 2006 15:04")
}

func permText(m fs.FileMode) string {
	return fmt.Sprintf("%s  %04o", m.String(), m.Perm())
}

func ownerText(uid, gid uint32) string {
	u := strconv.Itoa(int(uid))
	if x, err := user.LookupId(u); err == nil {
		u = x.Username
	}
	g := strconv.Itoa(int(gid))
	if x, err := user.LookupGroupId(g); err == nil {
		g = x.Name
	}
	return u + " · " + g
}

func groupDigits(n int64) string {
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// textHead is the start of a text file, for its preview: up to 2 KB and
// 40 lines, if it is text.
func textHead(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 2048)
	n, _ := io.ReadFull(f, buf)
	buf = buf[:n]
	for !utf8.Valid(buf) && len(buf) > 0 && len(buf) > n-4 {
		buf = buf[:len(buf)-1] // a character cut at the end
	}
	if !utf8.Valid(buf) || strings.ContainsRune(string(buf), 0) {
		return ""
	}
	lines := strings.SplitN(string(buf), "\n", 41)
	if len(lines) > 40 {
		lines = lines[:40]
	}
	return strings.Join(lines, "\n")
}

// folderSize adds up what is below dir, giving up when stop says so or
// after 200 000 items.
func folderSize(dir string, stop func() bool) string {
	var size int64
	var n int
	partial := false
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if n++; n > 200000 || n%1000 == 0 && stop() {
			partial = true
			return fs.SkipAll
		}
		if d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				size += fi.Size()
			}
		}
		return nil
	})
	s := sizeText(size) + " · " + i18n.Nf("%d item", "%d items", n-1, n-1)
	if partial {
		s = i18n.Tf("more than %s", s)
	}
	return s
}
