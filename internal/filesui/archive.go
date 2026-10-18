package filesui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// compressor is the card that makes an archive: its name and format, and
// under Advanced its compression, a password and what goes in.
type compressor struct {
	w      *window
	c      *card
	dir    string
	paths  []string
	format files.Format
	opts   files.ArchiveOptions
	ptrs   []uintptr

	name             *gtk.Entry
	password, repeat *gtk.PasswordEntry
	encryptNames     *gtk.CheckButton
	lockRows         []*gtk.Widget // the password's rows, for formats that take one
	lockNote         *gtk.Label
}

// askCompress asks how to make the archive of paths, then makes it beside
// them. The format and options chosen last are offered first; a password
// never is.
func (w *window) askCompress(p *pane, paths []string) {
	k := &compressor{w: w, paths: paths, dir: p.dir, opts: w.app.archiveOpts}
	if p.mode != inFolder {
		k.dir = filepath.Dir(paths[0])
	}
	k.format = files.Formats[0]
	for _, f := range files.Formats {
		if f.Write && f.Ext == w.app.archiveExt && (!f.Needs7z || files.Has7z()) {
			k.format = f
		}
	}
	k.c = &card{def: 1, buttons: []cardButton{
		{label: i18n.T("Cancel")},
		{label: i18n.T("Compress"), class: "suggested-action", action: k.submit},
	}}
	k.c.closed = func() { w.app.h.forget(k.ptrs) }

	title := i18n.Nf("Compress %d item", "Compress %d items", len(paths), len(paths))
	if len(paths) == 1 {
		title = i18n.Tf("Compress “%s”", filepath.Base(paths[0]))
	}
	w.openCard(k.c, cardParts{title: title, text: i18n.Tf("The archive is made in “%s”.", filepath.Base(k.dir)), form: true})
	k.build()
}

func (k *compressor) build() {
	w := k.w
	grid := gtk.NewGrid()
	grid.SetRowSpacing(10)
	grid.SetColumnSpacing(14)
	row := int32(0)
	label := func(g *gtk.Grid, r int32, text string) *gtk.Label {
		l := gtk.NewLabel(text)
		l.AddCssClass("files-form-label")
		l.SetXalign(1)
		g.Attach(&l.Widget, 0, r, 1, 1)
		return l
	}

	label(grid, row, i18n.T("Name"))
	k.name = gtk.NewEntry()
	k.name.SetHexpand(true)
	k.name.SetText(filepath.Base(files.FreeName(k.dir, files.ArchiveName(k.paths, k.format, i18n.T("Archive")), false)))
	w.app.h.enter(k.name, k.submit)
	w.app.h.edit(k.name, func() { w.cardFail("") })
	k.ptrs = append(k.ptrs, k.name.GoPointer())
	grid.Attach(&k.name.Widget, 1, row, 1, 1)
	row++

	label(grid, row, i18n.T("Format"))
	formats := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	formats.AddCssClass("files-kinds")
	formats.SetHalign(gtk.AlignStartValue)
	var first *gtk.ToggleButton
	for _, f := range files.Formats {
		if !f.Write {
			continue
		}
		b := gtk.NewToggleButtonWithLabel(f.Name)
		if first == nil {
			first = b
		} else {
			b.SetGroup(first)
		}
		b.SetActive(f.Ext == k.format.Ext)
		if f.Needs7z && !files.Has7z() {
			b.SetSensitive(false)
			b.SetTooltipText(i18n.T("Needs the 7z program (7zip)"))
		}
		w.app.h.toggle(b, func(on bool) {
			if on && f.Ext != k.format.Ext {
				k.setFormat(f)
			}
		})
		k.ptrs = append(k.ptrs, b.GoPointer())
		gtkx.Append(formats, &b.Widget)
	}
	grid.Attach(&formats.Widget, 1, row, 1, 1)
	gtkx.Append(&w.cardForm, &grid.Widget)

	// Advanced: open when an option chosen before is not the usual.
	adv := gtk.NewGrid()
	adv.SetRowSpacing(10)
	adv.SetColumnSpacing(14)
	adv.AddCssClass("files-advanced")
	row = 0
	label(adv, row, i18n.T("Compression"))
	levels := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	levels.AddCssClass("files-kinds")
	levels.SetHalign(gtk.AlignStartValue)
	first = nil
	for _, l := range []struct {
		level int
		name  string
	}{{files.LevelFast, i18n.T("Fast")}, {files.LevelNormal, i18n.T("Normal")}, {files.LevelBest, i18n.T("Smallest")}} {
		b := gtk.NewToggleButtonWithLabel(l.name)
		if first == nil {
			first = b
		} else {
			b.SetGroup(first)
		}
		b.SetActive(l.level == k.opts.Level)
		w.app.h.toggle(b, func(on bool) {
			if on {
				k.opts.Level = l.level
			}
		})
		k.ptrs = append(k.ptrs, b.GoPointer())
		gtkx.Append(levels, &b.Widget)
	}
	adv.Attach(&levels.Widget, 1, row, 1, 1)
	row++

	k.lockRows = nil
	password := func(text string) *gtk.PasswordEntry {
		l := label(adv, row, text)
		e := gtk.NewPasswordEntry()
		e.SetShowPeekIcon(true)
		e.SetHexpand(true)
		w.app.h.enter(e, k.submit)
		k.ptrs = append(k.ptrs, e.GoPointer())
		adv.Attach(&e.Widget, 1, row, 1, 1)
		k.lockRows = append(k.lockRows, &l.Widget, &e.Widget)
		row++
		return e
	}
	k.password = password(i18n.T("Password"))
	k.password.SetPropertyPlaceholderText(i18n.T("none"))
	k.repeat = password(i18n.T("Repeat it"))
	k.encryptNames = gtk.NewCheckButtonWithLabel(i18n.T("Lock the file names too"))
	k.encryptNames.SetActive(k.opts.EncryptNames)
	k.encryptNames.SetTooltipText(i18n.T("Without the password, not even the list of files shows (7z)"))
	w.app.h.check(k.encryptNames, func(on bool) { k.opts.EncryptNames = on })
	k.ptrs = append(k.ptrs, k.encryptNames.GoPointer())
	adv.Attach(&k.encryptNames.Widget, 1, row, 1, 1)
	row++
	k.lockNote = gtk.NewLabel("")
	k.lockNote.AddCssClass("sub")
	k.lockNote.SetXalign(0)
	k.lockNote.SetWrap(true)
	k.lockNote.SetMaxWidthChars(44) // wraps, not widening the card
	adv.Attach(&k.lockNote.Widget, 1, row, 1, 1)
	row++
	hidden := gtk.NewCheckButtonWithLabel(i18n.T("Leave out hidden files"))
	hidden.SetActive(k.opts.NoHidden)
	hidden.SetTooltipText(i18n.T("Names starting with a dot, inside the folders: .git, .cache …"))
	w.app.h.check(hidden, func(on bool) { k.opts.NoHidden = on })
	k.ptrs = append(k.ptrs, hidden.GoPointer())
	adv.Attach(&hidden.Widget, 1, row, 1, 1)

	exp := gtk.NewExpander(i18n.T("Advanced"))
	exp.AddCssClass("files-expander")
	exp.SetChild(&adv.Widget)
	exp.SetExpanded(k.opts != (files.ArchiveOptions{}))
	gtkx.Append(&w.cardForm, &exp.Widget)

	k.setFormat(k.format)
	k.name.GrabFocus()
	k.selectStem()
}

// setFormat makes f the format: the name's ending follows, and the
// password's rows say whether f takes one.
func (k *compressor) setFormat(f files.Format) {
	text := strings.TrimSpace(k.name.GetText())
	if strings.HasSuffix(strings.ToLower(text), k.format.Ext) {
		text = text[:len(text)-len(k.format.Ext)]
	}
	k.format = f
	k.name.SetText(text + f.Ext)
	k.selectStem()
	for _, wd := range k.lockRows {
		wd.SetSensitive(f.Password)
	}
	k.encryptNames.SetSensitive(f.Ext == ".7z")
	switch {
	case !f.Password:
		k.lockNote.SetText(i18n.Tf("%s archives cannot have a password: choose Zip or 7z for one.", f.Name))
	case !files.Has7z():
		k.lockNote.SetText(i18n.T("A password needs the 7z program (7zip)."))
	case f.Ext == ".zip":
		k.lockNote.SetText(i18n.T("Locked with AES-256: older unzip programs cannot open it; 7-Zip and the file managers of today can."))
	default:
		k.lockNote.SetText("")
	}
	k.lockNote.SetVisible(k.lockNote.GetText() != "")
	k.w.cardFail("")
}

// selectStem selects the name without its ending, to be typed over.
func (k *compressor) selectStem() {
	stem := files.ArchiveStem(k.name.GetText())
	k.name.SelectRegion(0, int32(len([]rune(stem))))
}

func (k *compressor) submit() {
	w := k.w
	if w.card != k.c {
		return
	}
	n := strings.TrimSpace(k.name.GetText())
	if f, ok := files.ArchiveFormat(n); !ok || f.Ext != k.format.Ext {
		n += k.format.Ext // the format chosen, when the name does not end in it
	}
	if msg := badName(n); msg != "" {
		w.cardFail(msg)
		return
	}
	pw := k.password.GetText()
	switch {
	case k.format.Needs7z && !files.Has7z():
		w.cardFail(i18n.T("7z archives need the 7z program (7zip)."))
		return
	case pw != "" && !k.format.Password:
		w.cardFail(i18n.Tf("%s archives cannot have a password.", k.format.Name))
		return
	case pw != "" && !files.Has7z():
		w.cardFail(i18n.T("A password needs the 7z program (7zip)."))
		return
	case pw != k.repeat.GetText():
		w.cardFail(i18n.T("The two passwords differ."))
		k.repeat.GrabFocus()
		return
	}
	dest := filepath.Join(k.dir, n)
	if _, err := os.Lstat(dest); err == nil {
		w.cardFail(i18n.Tf("“%s” is taken here.", n))
		return
	}
	opts := k.opts
	w.app.archiveExt, w.app.archiveOpts = k.format.Ext, opts // the password is never kept
	opts.Password = pw
	if k.format.Ext != ".7z" {
		opts.EncryptNames = false
	}
	w.closeCard()
	w.app.run(w, files.Job{Kind: files.Compress, Sources: k.paths, Dest: dest, Archive: opts})
}

// extract takes the archives apart, each beside itself; one locked with a
// password asks for it.
func (w *window) extract(paths []string) {
	for _, p := range paths {
		w.extractOne(p, "")
	}
}

func (w *window) extractOne(path, password string) {
	job := files.Job{Kind: files.Extract, Sources: []string{path}, Dest: filepath.Dir(path),
		Archive: files.ArchiveOptions{Password: password}}
	w.queue(queued{job: job, done: func(err error) bool {
		switch {
		case errors.Is(err, files.ErrWrongPassword):
			w.askArchivePassword(path, true)
		case errors.Is(err, files.ErrEncrypted):
			w.askArchivePassword(path, false)
		default:
			return false
		}
		return true
	}})
}

// askArchivePassword asks for the password of the archive at path, then
// extracts it with that; wrong says the last one was not it.
func (w *window) askArchivePassword(path string, wrong bool) {
	text := i18n.T("Type its password to extract it.")
	if wrong {
		text = i18n.T("That is not its password. Try again.")
	}
	var c *card
	c = &card{def: 1, buttons: []cardButton{
		{label: i18n.T("Cancel")},
		{label: i18n.T("Extract"), class: "suggested-action", action: func() {
			pw := w.cardSecret.GetText()
			if pw == "" {
				return
			}
			w.closeCard()
			w.extractOne(path, pw)
		}},
	}}
	w.openCard(c, cardParts{title: i18n.Tf("“%s” is locked", filepath.Base(path)), text: text, secret: true,
		secretHint: i18n.T("Password")})
}

// allArchives says whether every path is an archive Files takes apart.
func allArchives(paths []string) bool {
	for _, p := range paths {
		if !files.IsArchive(filepath.Base(p)) {
			return false
		}
		if fi, err := os.Stat(p); err != nil || fi.IsDir() {
			return false
		}
	}
	return len(paths) > 0
}
