package mailui

import (
	"strings"
	"unicode"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
)

// avatarSize is an account's photo's side, in the index and the settings.
const avatarSize = 22

// avatarWidget is an account's photo, or its initial while there is none
// (or while the photo is fetched).
func (w *window) avatarWidget(x *acct) *gtk.Widget {
	id := x.s.Acc.ID
	if tex := w.avatars[id]; tex != nil {
		pic := gtk.NewPictureForPaintable(tex)
		pic.SetContentFit(gtk.ContentFitCoverValue)
		pic.SetCanShrink(true)
		pic.SetSizeRequest(avatarSize, avatarSize)
		pic.SetValign(gtk.AlignCenterValue)
		pic.AddCssClass("mail-avatar")
		pic.SetOverflow(gtk.OverflowHiddenValue)
		return &pic.Widget
	}
	if x.s.Acc.Picture != "" && !w.avatarAsked[id] {
		w.avatarAsked[id] = true
		acc := x.s.Acc
		go func() {
			b, err := w.store.Avatar(w.ctx, acc)
			if err != nil {
				return
			}
			gtkx.Post(func() {
				tex, err := gdk.NewTextureFromBytes(glib.NewBytes(b, uint(len(b))))
				if err != nil {
					return
				}
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
