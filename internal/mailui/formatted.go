package mailui

import (
	"fmt"
	"image"
	"slices"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// pictureWidth is the widest a message's picture is drawn, about the
// reader's text width.
const pictureWidth = 640

// pic is a message's picture as the reader has it: being read, drawn, or
// not to be had.
type pic struct {
	tex     *gdk.Texture
	loading bool
	failed  bool
}

// formatted says whether a message is drawn formatted: it has HTML, and
// neither the settings nor the user's choice for it say text.
func (w *window) formatted(k mail.Key, m *mail.Message) bool {
	if m.HTML == "" {
		return false
	}
	return w.settings.Reading.Text == w.flipped[k]
}

// picturesAllowed says whether a message's pictures from the web load:
// never when the settings say so or the phishing route found it
// suspicious (unless asked for this message), else asked for, always, or
// always from its sender.
func (w *window) picturesAllowed(k mail.Key, m *mail.Message) bool {
	r := w.settings.Reading
	switch {
	case r.Images == "never":
		return false
	case w.picsOK[k]:
		return true
	case w.judgment(k) != nil && w.judgment(k).Verdict == mail.Suspicious:
		return false
	case r.Images == "always":
		return true
	}
	return r.LoadImages(m.From.Addr)
}

// drawFormatted draws a message's HTML as blocks; the part quoting earlier
// mail folds behind "Show quoted text". It says whether there was such a
// part.
func (w *window) drawFormatted(box *gtk.Box, k mail.Key, m *mail.Message) (quoted bool) {
	blocks := w.blocks[k]
	if blocks == nil {
		blocks = mail.Format(m.HTML)
		w.blocks[k] = blocks
	}
	if slices.IndexFunc(blocks, func(b mail.Block) bool { return !b.Quoted }) < 0 {
		for i := range blocks { // nothing but a quote: show it
			blocks[i].Quoted = false
		}
	}
	allowed := w.picturesAllowed(k, m)
	held := 0
	body := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	body.AddCssClass("mail-html")
	for _, b := range blocks {
		if b.Quoted {
			quoted = true
			if !w.quotes[k] {
				continue
			}
		}
		var wd *gtk.Widget
		switch b.Kind {
		case mail.BlockImage:
			if b.Image.Remote() && !allowed {
				held++
				continue
			}
			wd = w.picture(k, m, b.Image)
		case mail.BlockRule:
			s := gtk.NewSeparator(gtk.OrientationHorizontalValue)
			s.AddCssClass("mail-html-rule")
			wd = &s.Widget
		case mail.BlockItem:
			row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
			row.AddCssClass("mail-html-item")
			row.SetMarginStart(int32(b.Depth) * 18)
			mark := gtk.NewLabel(b.Mark)
			mark.AddCssClass("mail-html-mark")
			mark.SetValign(gtk.AlignStartValue)
			gtkx.Append(row, &mark.Widget)
			gtkx.Append(row, &w.markupLabel(b.Markup, "mail-html-p").Widget)
			wd = &row.Widget
		case mail.BlockHeading:
			wd = &w.markupLabel(b.Markup, "mail-html-h", fmt.Sprintf("mail-html-h%d", min(b.Level, 4))).Widget
		case mail.BlockPre:
			wd = &w.markupLabel(b.Markup, "mail-html-pre").Widget
		default:
			wd = &w.markupLabel(b.Markup, "mail-html-p").Widget
		}
		if wd == nil {
			continue
		}
		if b.Depth > 0 && b.Kind != mail.BlockItem || b.Quoted {
			wd.AddCssClass("mail-html-quote")
		}
		if b.Quoted {
			wd.AddCssClass("mail-quoted")
		}
		gtkx.Append(body, wd)
	}
	if held > 0 {
		w.drawHeld(box, k, m, held)
	}
	gtkx.Append(box, &body.Widget)
	return quoted
}

func (w *window) markupLabel(markup string, classes ...string) *gtk.Label {
	l := gtk.NewLabel("")
	for _, c := range classes {
		l.AddCssClass(c)
	}
	l.SetMarkup(markup)
	if l.GetText() == "" && markup != "" { // markup Pango refused: its text
		l.SetText(stripMarkup(markup))
	}
	l.SetXalign(0)
	l.SetWrap(true)
	l.SetWrapMode(2) // word_char: long links
	l.SetSelectable(true)
	l.SetMaxWidthChars(84)
	return l
}

func stripMarkup(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&amp;", "&").Replace(b.String())
}

// drawHeld is the line over a message whose pictures from the web are not
// loaded: load them, for it or always for its sender.
func (w *window) drawHeld(box *gtk.Box, k mail.Key, m *mail.Message, n int) {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	row.AddCssClass("mail-held")
	icon := gtk.NewImageFromIconName("image-x-generic-symbolic")
	icon.AddCssClass("mail-held-icon")
	gtkx.Append(row, &icon.Widget)
	l := gtk.NewLabel(i18n.Nf("%d picture hidden", "%d pictures hidden", n, n))
	l.AddCssClass("mail-held-text")
	l.SetXalign(0)
	l.SetHexpand(true)
	l.SetEllipsize(3)
	tip := i18n.T("Pictures from the web tell the sender you opened the message, and when.")
	if j := w.judgment(k); j != nil && j.Verdict == mail.Suspicious {
		tip = i18n.T("Pictures from the web are not loaded by themselves for a message that looks suspicious.")
	}
	l.SetTooltipText(tip)
	gtkx.Append(row, &l.Widget)
	if w.settings.Reading.Images != "never" {
		w.chip(row, "", i18n.T("Show"), func() { w.picsOK[k] = true; w.lastDrawn = ""; w.drawReader() })
		if addr := strings.ToLower(m.From.Addr); addr != "" {
			w.chip(row, "", i18n.T("Always from this sender"), func() {
				r := &w.settings.Reading
				if !slices.Contains(r.ImagesFrom, addr) {
					r.ImagesFrom = append(r.ImagesFrom, addr)
				}
				if err := w.settings.Save(mail.SettingsPath()); err != nil {
					w.actErr = i18n.Tf("Not saved: %v", err)
				}
				w.lastDrawn = ""
				w.drawReader()
			}).SetTooltipText(addr)
		}
	}
	gtkx.Append(box, &row.Widget)
}

// picture is a message's picture: drawn when it is had, read in the
// background when not (a part's, a data URL's, or the web's), its alt
// text meanwhile. A picture that is a link opens it.
func (w *window) picture(k mail.Key, m *mail.Message, p mail.Picture) *gtk.Widget {
	width := pictureWidth
	if p.Width > 0 {
		width = min(p.Width, pictureWidth)
	}
	id := p.Src
	if id == "" {
		id = fmt.Sprintf("data:%x", p.Data[:min(len(p.Data), 64)])
	}
	id = fmt.Sprintf("%s#%d", id, width)
	got := w.pics[id]
	if got == nil {
		got = &pic{loading: true}
		w.pics[id] = got
		data := p.Data
		if strings.HasPrefix(p.Src, "cid:") {
			cid := strings.Trim(strings.TrimPrefix(p.Src, "cid:"), "<>")
			for _, a := range m.Attachments {
				if strings.Trim(a.ContentID, "<>") == cid {
					data = a.Data
				}
			}
			if data == nil {
				got.loading, got.failed = false, true
			}
		}
		if got.loading {
			src := p.Src
			go func() {
				b := data
				var err error
				if b == nil {
					b, err = mail.FetchPicture(w.ctx, src)
				}
				var px *image.RGBA
				if err == nil {
					px, err = mail.ScalePicture(b, width)
				}
				gtkx.Post(func() {
					got.loading = false
					if err != nil {
						got.failed = true
					} else {
						got.tex = rgbaTexture(px)
					}
					w.picsChanged()
				})
			}()
		}
	}
	if got.tex == nil {
		if p.Alt == "" || got.loading && p.Height > 0 && p.Height < 24 {
			return nil
		}
		l := gtk.NewLabel(p.Alt)
		l.AddCssClass("mail-html-alt")
		l.SetXalign(0)
		l.SetWrap(true)
		if got.loading {
			l.AddCssClass("loading")
		}
		return &l.Widget
	}
	// A picture has no natural size in the reader's column: a small one
	// gets its own, a wide one the column's width at its own height
	// (narrower columns scale it down).
	img := gtk.NewPictureForPaintable(got.tex)
	img.SetCanShrink(true)
	img.SetContentFit(gtk.ContentFitScaleDownValue)
	tw, th := int32(got.tex.GetWidth()), int32(got.tex.GetHeight())
	align := gtk.AlignFillValue
	if tw <= 360 {
		img.SetSizeRequest(tw, th)
		align = gtk.AlignStartValue
	} else {
		// The column's text width now, as the reader is drawn.
		if col := int32(w.readerScroll.GetWidth()) - 96; col > 0 && col < tw {
			th = th * col / tw
		}
		img.SetSizeRequest(-1, th)
	}
	img.SetHalign(align)
	img.AddCssClass("mail-html-img")
	if p.Alt != "" {
		img.SetAlternativeText(p.Alt)
	}
	if p.Link == "" {
		return &img.Widget
	}
	b := gtk.NewButton()
	b.AddCssClass("mail-html-img-link")
	b.SetHalign(align)
	b.SetChild(&img.Widget)
	b.SetTooltipText(p.Link)
	link := p.Link
	w.readerBtns = append(w.readerBtns, w.h.click(b, func() { desktop.Spawn([]string{"xdg-open", link}, "") }))
	return &b.Widget
}

// picsChanged redraws the reader once for the pictures that came in about
// the same time.
func (w *window) picsChanged() {
	if w.picsDue {
		return
	}
	w.picsDue = true
	w.h.later(120, func() {
		w.picsDue = false
		w.lastDrawn = ""
		w.drawReader()
	})
}

// rgbaTexture makes a texture of premultiplied RGBA pixels.
func rgbaTexture(px *image.RGBA) *gdk.Texture {
	b := glib.NewBytes(px.Pix, uint(len(px.Pix)))
	tex := gdk.NewMemoryTexture(int32(px.Rect.Dx()), int32(px.Rect.Dy()), gdk.MemoryR8g8b8a8PremultipliedValue, b, uint(px.Stride))
	b.Unref()
	return &tex.Texture
}
