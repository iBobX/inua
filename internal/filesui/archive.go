package filesui

import (
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// askCompress asks for the archive's name and format, then makes it of
// paths in the pane's folder. The format chosen last is offered first.
func (w *window) askCompress(p *pane, paths []string) {
	dir := p.dir
	if p.mode != inFolder {
		dir = filepath.Dir(paths[0])
	}
	var writable []files.Format
	for _, f := range files.Formats {
		if f.Write {
			writable = append(writable, f)
		}
	}
	format := writable[0]
	for _, f := range writable {
		if f.Ext == w.app.archiveExt {
			format = f
		}
	}
	name := filepath.Base(files.FreeName(dir, files.ArchiveName(paths, format, i18n.T("Archive")), false))

	var c *card
	var ptrs []uintptr
	c = &card{def: 1, buttons: []cardButton{
		{label: i18n.T("Cancel")},
		{label: i18n.T("Compress"), class: "suggested-action", action: func() {
			n := strings.TrimSpace(w.cardEntry.GetText())
			if f, ok := files.ArchiveFormat(n); !ok || !f.Write {
				n += format.Ext // the format chosen, when the name does not say one
			}
			if msg := badName(n); msg != "" {
				w.cardFail(msg)
				return
			}
			dest := filepath.Join(dir, n)
			if _, err := os.Lstat(dest); err == nil {
				w.cardFail(i18n.Tf("“%s” is taken here.", n))
				return
			}
			w.closeCard()
			w.app.archiveExt = format.Ext
			w.app.run(w, files.Job{Kind: files.Compress, Sources: paths, Dest: dest})
		}},
	}}
	c.edited = func(t string) { w.cardFail(badName(strings.TrimSpace(t))) }
	c.closed = func() { w.app.h.forget(ptrs) }

	formats := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	formats.AddCssClass("files-kinds")
	formats.SetHalign(gtk.AlignStartValue)
	var first *gtk.ToggleButton
	for _, f := range writable {
		b := gtk.NewToggleButtonWithLabel(f.Name)
		if first == nil {
			first = b
		} else {
			b.SetGroup(first)
		}
		b.SetActive(f.Ext == format.Ext)
		w.app.h.toggle(b, func(on bool) {
			if !on || f.Ext == format.Ext {
				return
			}
			// The name's ending follows the format; the rest is kept.
			text := strings.TrimSpace(w.cardEntry.GetText())
			if strings.HasSuffix(strings.ToLower(text), format.Ext) {
				text = text[:len(text)-len(format.Ext)]
			}
			format = f
			w.cardEntry.SetText(text + f.Ext)
			stem := int32(len([]rune(text)))
			w.cardEntry.SelectRegion(0, stem)
		})
		ptrs = append(ptrs, b.GoPointer())
		gtkx.Append(formats, &b.Widget)
	}

	title := i18n.Nf("Compress %d item", "Compress %d items", len(paths), len(paths))
	if len(paths) == 1 {
		title = i18n.Tf("Compress “%s”", filepath.Base(paths[0]))
	}
	w.openCard(c, cardParts{title: title, entry: true, entryText: name, selectStem: true, form: true})
	gtkx.Append(&w.cardForm, &formats.Widget)
	w.cardEntry.GrabFocus()
	stem := int32(len([]rune(files.ArchiveStem(name))))
	w.cardEntry.SelectRegion(0, stem)
}

// extract takes the archives apart, each beside itself.
func (w *window) extract(paths []string) {
	byDir := map[string][]string{}
	var dirs []string
	for _, p := range paths {
		d := filepath.Dir(p)
		if byDir[d] == nil {
			dirs = append(dirs, d)
		}
		byDir[d] = append(byDir[d], p)
	}
	for _, d := range dirs {
		w.app.run(w, files.Job{Kind: files.Extract, Sources: byDir[d], Dest: d})
	}
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
