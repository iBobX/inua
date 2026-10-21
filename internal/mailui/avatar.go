package mailui

import (
	"log"
	"strconv"
	"strings"
	"unicode"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gdkpixbuf"
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
				w.relist() // the rows' faces and badges
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

// face is a sender on a round mark: the photo of one of the user's own
// accounts, else the domain's BIMI logo for a message proven to come from
// it, else the logo of the service the address is at, on its colours,
// else the name's initial in the tone of the address (mail.Face).
type face struct {
	box     gtk.Box
	initial gtk.Label
	logo    gtk.Image
	size    int32
	tone    string
	brand   string // its brand-… class, "" for none
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

// setFace draws a on f; verified, a message from a proven to come from
// its domain, may show the domain's BIMI logo.
func (w *window) setFace(f *face, a mail.Address, verified bool) {
	icon, tone := mail.Face(a)
	if t := "tone-" + strconv.Itoa(tone); t != f.tone {
		if f.tone != "" {
			f.box.RemoveCssClass(f.tone)
		}
		f.box.AddCssClass(t)
		f.tone = t
	}
	photo := w.photoOf(a.Addr)
	if photo == nil && verified {
		photo = w.bimiLogo(mail.Domain(a.Addr))
	}
	if photo != nil {
		icon = "" // a photo or the brand's own logo, not the drawn one
	}
	brand := ""
	if icon != "" {
		brand = "brand-" + icon
	}
	if brand != f.brand {
		if f.brand != "" {
			f.box.RemoveCssClass(f.brand)
		}
		if brand != "" {
			f.box.AddCssClass(brand)
		}
		f.brand = brand
	}
	setClass(&f.box.Widget, "brand", brand != "")
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
func (w *window) senderMark(a mail.Address, size int32, verified bool) *gtk.Widget {
	f := newFace(size)
	w.setFace(f, a, verified)
	return &f.box.Widget
}

// verified says whether a message is proven to come from its sender's
// domain: as the index keeps it, else from the message read (one kept
// from before the index did).
func (w *window) verified(it mail.Item) bool {
	if it.Verified {
		return true
	}
	rd := w.reading[it.Key()]
	return rd != nil && rd.m != nil && mail.Verified(rd.m.Authentication, mail.Domain(rd.m.From.Addr))
}

// bimiLogo is a domain's BIMI logo, nil while it is asked for or when
// there is none; asked once a run, drawn at three times a face's size.
func (w *window) bimiLogo(domain string) *gdk.Texture {
	if tex := w.bimi[domain]; tex != nil || domain == "" || w.bimiAsked[domain] {
		return tex
	}
	w.bimiAsked[domain] = true
	go func() {
		path, err := w.store.BrandLogo(w.ctx, domain)
		if err != nil || path == "" {
			return
		}
		px, err := gdkpixbuf.NewPixbufFromFileAtScale(path, rowFace*3, rowFace*3, true)
		if err != nil {
			return
		}
		gtkx.Post(func() {
			w.bimi[domain] = gdk.NewTextureForPixbuf(px)
			w.relist()
			w.lastDrawn = ""
			w.drawReader()
		})
	}()
	return nil
}

// brandStyle gives the services' faces their own colours, for the theme
// as light or dark as it is.
func (w *window) brandStyle() {
	css := gtk.NewCssProvider()
	css.LoadFromString(mail.BrandCSS(mail.Dark(w.c.Colors["app"])))
	gtk.StyleContextAddProviderForDisplay(gdk.DisplayGetDefault(), css, uint32(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION))
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
