package mailui

import (
	"strings"
	"unicode"

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

// initial is a name's first letter or digit, upper case.
func initial(name string) string {
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return strings.ToUpper(string(r))
		}
	}
	return "@"
}
