package filesui

import (
	"context"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files/remote"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// setup is a remote disk's setup on a card: its kind, then the fields
// that kind asks for, built again when the kind changes; what was typed in
// a field another kind has too stays.
type setup struct {
	w      *window
	c      *card
	p      *pane
	old    *remote.Disk // the disk it changes; nil for a new one
	kind   remote.Kind
	kinds  map[remote.Kind]*gtk.ToggleButton
	fields map[string]*gtk.Entry
	secret *gtk.PasswordEntry
	tls    *gtk.CheckButton
	values map[string]string
	ptrs   []uintptr // the fields' and kinds' handlers

	named   bool // the name was typed, not the one offered
	filling bool // the name is being offered, not typed
}

// field is one of a kind's fields: its key in the form, its label, and
// what it says while it is empty.
type field struct{ key, label, hint string }

func (s *setup) kindFields() []field {
	optional := i18n.T("optional")
	switch s.kind {
	case remote.SFTP:
		return []field{
			{"server", i18n.T("Server"), i18n.T("host, or host:port")},
			{"user", i18n.T("User"), ""},
			{"secret", i18n.T("Password"), i18n.T("empty: your SSH keys")},
			{"folder", i18n.T("Folder"), i18n.T("empty: the home folder")},
		}
	case remote.SMB:
		return []field{
			{"server", i18n.T("Server"), "nas, nas.local, 192.168.1.10"},
			{"share", i18n.T("Share"), ""},
			{"folder", i18n.T("Folder"), optional},
			{"user", i18n.T("User"), i18n.T("empty: as a guest")},
			{"secret", i18n.T("Password"), ""},
		}
	case remote.WebDAV:
		return []field{
			{"url", i18n.T("URL"), "https://cloud.example.org/remote.php/dav/files/you"},
			{"user", i18n.T("User"), ""},
			{"secret", i18n.T("Password"), i18n.T("or an app password")},
		}
	case remote.FTP:
		return []field{
			{"server", i18n.T("Server"), i18n.T("host, or host:port")},
			{"user", i18n.T("User"), i18n.T("empty: anonymous")},
			{"secret", i18n.T("Password"), ""},
			{"folder", i18n.T("Folder"), optional},
			{"tls", i18n.T("Encrypted (FTPS)"), ""},
		}
	case remote.S3:
		return []field{
			{"server", i18n.T("Endpoint"), i18n.T("s3.amazonaws.com, or your server's host")},
			{"bucket", i18n.T("Bucket"), ""},
			{"folder", i18n.T("Folder"), optional},
			{"user", i18n.T("Access key ID"), ""},
			{"secret", i18n.T("Secret access key"), ""},
		}
	case remote.R2:
		return []field{
			{"server", i18n.T("Account ID"), i18n.T("on the R2 page of Cloudflare's dashboard")},
			{"bucket", i18n.T("Bucket"), ""},
			{"folder", i18n.T("Folder"), optional},
			{"user", i18n.T("Access key ID"), i18n.T("an R2 API token's")},
			{"secret", i18n.T("Secret access key"), ""},
		}
	}
	return nil
}

// kindName is a kind as its button says it.
func kindName(k remote.Kind) string {
	switch k {
	case remote.SFTP:
		return "SFTP"
	case remote.SMB:
		return "SMB"
	case remote.WebDAV:
		return "WebDAV"
	case remote.FTP:
		return "FTP"
	case remote.S3:
		return "S3"
	case remote.R2:
		return "R2"
	}
	return string(k)
}

// kindText says what a kind is for, under the card's title.
func kindText(k remote.Kind) string {
	switch k {
	case remote.SFTP:
		return i18n.T("A server you reach with ssh.")
	case remote.SMB:
		return i18n.T("A Windows or Samba share, a NAS.")
	case remote.WebDAV:
		return i18n.T("Nextcloud, ownCloud or another WebDAV server: the URL its settings show.")
	case remote.FTP:
		return i18n.T("An FTP server.")
	case remote.S3:
		return i18n.T("Amazon S3, or another S3 server (MinIO, Backblaze B2, Wasabi) by its endpoint.")
	case remote.R2:
		return i18n.T("A Cloudflare R2 bucket, with an R2 API token's keys.")
	}
	return ""
}

// setupRemote shows a remote disk's setup: a new one's, or old's to change.
func (w *window) setupRemote(old *remote.Disk) {
	s := &setup{w: w, p: w.cur(), old: old, kind: remote.SFTP, values: map[string]string{}}
	title, button := i18n.T("Remote disk"), i18n.T("Connect")
	if old != nil {
		title, button = i18n.Tf("Change %s", old.Name), i18n.T("Save and connect")
		if a, err := remote.Parse(old.Address); err == nil {
			f := remote.FormOf(a)
			s.kind = f.Kind
			server := f.Server
			if f.Port != "" {
				server += ":" + f.Port
			}
			s.values = map[string]string{"name": old.Name, "server": server, "user": f.User, "share": f.Share,
				"bucket": f.Bucket, "folder": f.Folder, "url": f.URL}
			if f.TLS {
				s.values["tls"] = "1"
			}
		}
		s.named = true
	}
	s.c = &card{def: 2, buttons: []cardButton{
		{label: i18n.T("Test connection"), aside: true, action: s.test},
		{label: i18n.T("Cancel")},
		{label: button, class: "suggested-action", action: s.submit},
	}}
	s.c.closed = func() { w.app.h.forget(s.ptrs) }
	parts := cardParts{title: title, text: kindText(s.kind), form: true}
	if w.app.keyring {
		parts.check, parts.checked = i18n.T("Remember in the keyring"), true
	}
	w.openCard(s.c, parts)
	s.build()
	if err := w.app.remote.Ready(); err != nil {
		w.cardFail(w.remoteError(remote.Disk{}, err))
	}
}

// build fills the card's form for the kind: the kinds, the name, the
// kind's fields.
func (s *setup) build() {
	w := s.w
	s.keep()
	w.app.h.forget(s.ptrs)
	s.ptrs = nil
	gtkx.Clear(&w.cardForm)
	w.cardText.SetText(kindText(s.kind))

	kinds := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	kinds.AddCssClass("files-kinds")
	kinds.SetHalign(gtk.AlignStartValue)
	s.kinds = map[remote.Kind]*gtk.ToggleButton{}
	var first *gtk.ToggleButton
	for _, k := range remote.Kinds {
		b := gtk.NewToggleButtonWithLabel(kindName(k))
		if first == nil {
			first = b
		} else {
			b.SetGroup(first)
		}
		b.SetActive(k == s.kind)
		s.kinds[k] = b
		w.app.h.toggle(b, func(on bool) {
			if on && k != s.kind {
				s.kind = k
				w.cardFail("")
				gtkx.Post(s.build) // not from within the button's own signal
			}
		})
		s.ptrs = append(s.ptrs, b.GoPointer())
		gtkx.Append(kinds, &b.Widget)
	}
	gtkx.Append(&w.cardForm, &kinds.Widget)

	grid := gtk.NewGrid()
	grid.SetRowSpacing(8)
	grid.SetColumnSpacing(14)
	s.fields = map[string]*gtk.Entry{}
	s.secret, s.tls = nil, nil
	row := int32(0)
	label := func(text string) {
		l := gtk.NewLabel(text)
		l.AddCssClass("files-form-label")
		l.SetXalign(1)
		grid.Attach(&l.Widget, 0, row, 1, 1)
	}
	entry := func(key, hint string) *gtk.Entry {
		e := gtk.NewEntry()
		e.SetHexpand(true)
		e.SetPlaceholderText(hint)
		e.SetText(s.values[key])
		w.app.h.enter(e, s.submit)
		s.ptrs = append(s.ptrs, e.GoPointer())
		grid.Attach(&e.Widget, 1, row, 1, 1)
		s.fields[key] = e
		return e
	}
	label(i18n.T("Name"))
	name := entry("name", i18n.T("from the server, the share or the bucket"))
	w.app.h.edit(name, func() {
		if !s.filling {
			s.named = strings.TrimSpace(name.GetText()) != ""
		}
	})
	row++
	for _, f := range s.kindFields() {
		switch f.key {
		case "secret":
			label(f.label)
			p := gtk.NewPasswordEntry()
			p.SetShowPeekIcon(true)
			p.SetHexpand(true)
			hint := f.hint
			if s.old != nil {
				hint = i18n.T("empty: the one kept")
			}
			p.SetPropertyPlaceholderText(hint)
			w.app.h.enter(p, s.submit)
			s.ptrs = append(s.ptrs, p.GoPointer())
			grid.Attach(&p.Widget, 1, row, 1, 1)
			s.secret = p
		case "tls":
			s.tls = gtk.NewCheckButtonWithLabel(f.label)
			s.tls.SetActive(s.values["tls"] != "")
			grid.Attach(&s.tls.Widget, 1, row, 1, 1)
		default:
			label(f.label)
			e := entry(f.key, f.hint)
			if f.key != "user" && f.key != "folder" {
				w.app.h.edit(e, s.offerName)
			}
		}
		row++
	}
	gtkx.Append(&w.cardForm, &grid.Widget)
	s.offerName()
	for _, key := range []string{"server", "url"} {
		if e := s.fields[key]; e != nil {
			e.GrabFocus()
			break
		}
	}
}

// keep remembers what the fields hold, for the kind built next.
func (s *setup) keep() {
	for k, e := range s.fields {
		s.values[k] = e.GetText()
	}
	if s.tls != nil {
		s.values["tls"] = ""
		if s.tls.GetActive() {
			s.values["tls"] = "1"
		}
	}
}

// form is the fields as the remote package takes them.
func (s *setup) form() remote.Form {
	s.keep()
	v := s.values
	return remote.Form{Kind: s.kind, Server: v["server"], User: v["user"], Share: v["share"],
		Bucket: v["bucket"], Folder: v["folder"], URL: v["url"], TLS: v["tls"] != ""}
}

// offerName puts the address's name in the name field, until a name is
// typed there.
func (s *setup) offerName() {
	name := s.fields["name"]
	if s.named || name == nil {
		return
	}
	offer := ""
	if a, err := s.form().Address(); err == nil {
		offer = remote.FreeName(a.Name(), s.w.app.settings.Remote)
	}
	s.filling = true
	name.SetText(offer)
	s.filling = false
}

// address is the form's address, or "" and the card says what is wrong.
func (s *setup) address() (remote.Address, bool) {
	a, err := s.form().Address()
	if err != nil {
		s.w.cardFail(addressError(err))
		return remote.Address{}, false
	}
	return a, true
}

// withSecret runs then on the main thread with the secret the form gives,
// or, left empty, the one kept for the disk it changes; meanwhile button
// busy says what is being done.
func (s *setup) withSecret(a remote.Address, busy int, doing string, then func(secret string)) {
	w := s.w
	secret := s.secret.GetText()
	w.cardFail("")
	w.cardBusyAt(busy, doing)
	go func() {
		if secret == "" && s.old != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			secret, _, _ = w.app.secrets.Lookup(ctx, s.old.Address)
			cancel()
		}
		gtkx.Post(func() {
			if w.card != s.c {
				return
			}
			if a.KeyPair() && (a.User == "" || secret == "") {
				w.cardBusyAt(busy, "")
				w.cardFail(i18n.T("Both the key's ID and its secret are needed."))
				return
			}
			then(secret)
		})
	}()
}

// test tries the form's address and secret, mounting nothing and keeping
// nothing, and says in a notification whether it connects.
func (s *setup) test() {
	w := s.w
	if w.card != s.c || s.c.busy {
		return
	}
	a, ok := s.address()
	if !ok {
		return
	}
	if err := w.app.remote.Ready(); err != nil {
		w.cardFail(w.remoteError(remote.Disk{}, err))
		return
	}
	d := remote.Disk{Name: a.Name(), Address: a.String()}
	if name := strings.TrimSpace(s.values["name"]); name != "" {
		d.Name = name
	}
	s.withSecret(a, 0, i18n.T("Testing…"), func(secret string) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			err := w.app.remote.Test(ctx, d, secret)
			cancel()
			gtkx.Post(func() {
				if w.card != s.c {
					return
				}
				w.cardBusyAt(0, "")
				if err != nil {
					notify(i18n.Tf("%s does not connect", d.Name), w.remoteError(d, err))
					return
				}
				notify(i18n.Tf("%s connects", d.Name), i18n.T("Its folder can be read."))
			})
		}()
	})
}

// submit connects the disk the form describes, and keeps it once it works.
// A disk changed while it is mounted is disconnected first.
func (s *setup) submit() {
	w := s.w
	if w.card != s.c || s.c.busy {
		return
	}
	a, ok := s.address()
	if !ok {
		return
	}
	name := strings.TrimSpace(s.values["name"])
	if name == "" {
		name = remote.FreeName(a.Name(), w.app.settings.Remote)
	}
	d := remote.Disk{Name: name, Address: a.String()}
	if remote.CheckName(name) != nil {
		w.cardFail(i18n.Tf("“%s” cannot be a folder's name in ~/Remote.", name))
		return
	}
	for _, o := range w.app.settings.Remote {
		if o.Name == name && (s.old == nil || o.Name != s.old.Name) {
			w.cardFail(i18n.Tf("Another remote disk is called “%s”.", name))
			return
		}
	}
	if err := w.app.remote.Ready(); err != nil {
		w.cardFail(w.remoteError(d, err))
		return
	}
	remember := w.cardCheck.GetVisible() && w.cardCheck.GetActive()
	s.withSecret(a, s.c.def, i18n.T("Connecting…"), func(secret string) {
		connect := func() {
			w.mountRemote(s.p, d, secret, attempt{card: s.c, secret: s.secret, remember: remember, old: s.old})
		}
		if s.old == nil || !w.app.mountedAt(w.app.remote.Point(*s.old)) {
			connect()
			return
		}
		old := *s.old
		w.leave(w.app.remote.Point(old))
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			err := w.app.remote.Disconnect(ctx, old)
			cancel()
			gtkx.Post(func() {
				w.app.refreshMounts()
				if w.card != s.c {
					return
				}
				if err != nil {
					w.cardBusy("")
					w.cardFail(w.remoteError(old, err))
					return
				}
				connect()
			})
		}()
	})
}
