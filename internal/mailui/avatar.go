package mailui

import (
	"log"
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
				w.relist() // the rows' badges
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

// findPicture asks the provider, once, for the photo of an account whose
// sign-in did not give one (some Google Workspace accounts), and keeps its
// address in the settings, from where the index fetches it.
func (w *window) findPicture(x *acct) {
	if x.s.Acc.Picture != "" || x.askedPicture {
		return
	}
	x.askedPicture = true
	go func() {
		pic, err := x.s.Picture(w.ctx)
		if err != nil || pic == "" {
			return
		}
		gtkx.Post(func() {
			x.s.Acc.Picture = pic
			for i := range w.settings.Accounts {
				if w.settings.Accounts[i].ID == x.s.Acc.ID {
					w.settings.Accounts[i].Picture = pic
				}
			}
			if err := w.settings.Save(mail.SettingsPath()); err != nil {
				log.Printf("mail: keeping %s's photo: %v", x.s.Acc.ID, err)
			}
			w.drawSide()
		})
	}()
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
