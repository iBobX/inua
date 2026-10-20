package mailui

import (
	"strconv"
	"strings"
	"unicode"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/gtkx"
)

// avatarSize is an account's photo's side, in the index and the settings.
const avatarSize = 22

// avatarWidget is an account's photo, or its initial while there is none
// (or while the photo is fetched).
func (w *window) avatarWidget(x *acct) *gtk.Widget {
	id := x.s.Acc.ID
	if tex := w.avatars[id]; tex != nil {
		// An image at a pixel size: a picture would take the photo's own.
		img := gtk.NewImageFromPaintable(tex)
		img.SetPixelSize(avatarSize)
		img.SetValign(gtk.AlignCenterValue)
		img.AddCssClass("mail-avatar")
		img.SetOverflow(gtk.OverflowHiddenValue)
		return &img.Widget
	}
	if x.s.Acc.Picture != "" && !w.avatarAsked[id] {
		w.avatarAsked[id] = true
		acc := x.s.Acc
		go func() {
			b, err := w.store.Avatar(w.ctx, acc)
			if err != nil {
				return
			}
			px, err := mail.ScalePicture(b, avatarSize*3) // sharp at a scale of 3
			if err != nil {
				return
			}
			gtkx.Post(func() {
				tex := rgbaTexture(px)
				w.avatars[id] = tex
				w.drawSide()
				w.relist() // a row may be from this account
			})
		}()
	}
	l := gtk.NewLabel(initial(x.s.Acc.DisplayName()))
	l.AddCssClass("mail-avatar")
	l.AddCssClass("mail-monogram")
	l.SetSizeRequest(avatarSize, avatarSize)
	l.SetValign(gtk.AlignCenterValue)
	return &l.Widget
}

// face is a sender on a round mark: the photo of one of the user's own
// accounts, else the logo of the service the address is at, else the
// name's initial; the mark in the tone of the address (mail.Face).
type face struct {
	box     gtk.Box
	initial gtk.Label
	logo    gtk.Image
	size    int32
	tone    string
}

func newFace(size int32) *face {
	f := &face{size: size}
	f.box = *gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	f.box.AddCssClass("mail-sender-mark")
	f.box.SetSizeRequest(size, size)
	f.box.SetHexpand(false) // set, so the centred child's expanding stops here
	f.box.SetValign(gtk.AlignStartValue)
	f.box.SetOverflow(gtk.OverflowHiddenValue)
	f.initial = *gtk.NewLabel("")
	f.logo = *gtk.NewImage()
	for _, c := range []*gtk.Widget{&f.initial.Widget, &f.logo.Widget} {
		c.SetHexpand(true)
		c.SetHalign(gtk.AlignCenterValue)
		c.SetValign(gtk.AlignCenterValue)
		gtkx.Append(&f.box, c)
	}
	return f
}

// setFace draws a on f.
func (w *window) setFace(f *face, a mail.Address) {
	icon, tone := mail.Face(a)
	if t := "tone-" + strconv.Itoa(tone); t != f.tone {
		if f.tone != "" {
			f.box.RemoveCssClass(f.tone)
		}
		f.box.AddCssClass(t)
		f.tone = t
	}
	photo := w.photoOf(a.Addr)
	setClass(&f.box.Widget, "photo", photo != nil)
	switch {
	case photo != nil:
		f.logo.SetFromPaintable(photo)
		f.logo.SetPixelSize(f.size)
	case icon != "":
		f.logo.SetFromIconName("pass-" + icon + "-symbolic")
		f.logo.SetPixelSize(f.size / 2)
	default:
		name := a.Name
		if name == "" {
			name = a.Addr
		}
		f.initial.SetText(initial(name))
	}
	f.logo.SetVisible(photo != nil || icon != "")
	f.initial.SetVisible(photo == nil && icon == "")
}

// senderMark is a sender's face, made for a reader that is drawn anew.
func (w *window) senderMark(a mail.Address, size int32) *gtk.Widget {
	f := newFace(size)
	w.setFace(f, a)
	return &f.box.Widget
}

// photoOf is the photo of the user's own account at addr, nil when there
// is none (or it is still fetched).
func (w *window) photoOf(addr string) *gdk.Texture {
	for _, x := range w.accts {
		if strings.EqualFold(x.s.Acc.Address, strings.TrimSpace(addr)) {
			return w.avatars[x.s.Acc.ID]
		}
	}
	return nil
}

// initial is a name's first letter or digit, upper case.
func initial(name string) string {
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return strings.ToUpper(string(r))
		}
	}
	return "@"
}
