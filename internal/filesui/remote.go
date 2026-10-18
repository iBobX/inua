package filesui

import (
	"context"
	"errors"
	"log"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/apps/files/disks"
	"github.com/lyricat/inua/internal/apps/files/remote"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// --- remote disks ---

// connectRemote opens the disk at an address in p: one connected to
// before, or a new one named after its share, bucket or server.
func (w *window) connectRemote(p *pane, addr string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}
	d, ok := remote.Find(w.app.settings.Remote, addr)
	if !ok {
		a, err := remote.Parse(addr)
		if err != nil {
			w.flash(addressError(err))
			return
		}
		d = remote.Disk{Name: remote.FreeName(a.Name(), w.app.settings.Remote), Address: a.String()}
	}
	w.openRemote(p, d)
}

// openRemote opens a disk's folder in p, connecting it first: with the
// secret the keyring keeps for it, or one asked for.
func (w *window) openRemote(p *pane, d remote.Disk) {
	point := w.app.remote.Point(d)
	if w.app.mountedAt(point) {
		p.goTo(point, true)
		return
	}
	a, err := remote.Parse(d.Address)
	if err != nil {
		w.flash(addressError(err))
		return
	}
	if err := w.app.remote.Ready(); err != nil {
		w.flash(w.remoteError(d, err))
		return
	}
	w.flash(i18n.Tf("Connecting to %s…", d.Name))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute) // the keyring may ask to unlock
		secret, found, err := w.app.secrets.Lookup(ctx, d.Address)
		cancel()
		if err != nil && !errors.Is(err, remote.ErrNoKeyring) {
			log.Printf("files: the keyring, for %s: %v", d.Name, err)
		}
		gtkx.Post(func() {
			if a.NeedsSecret() && !found {
				w.askSecret(p, d, nil)
				return
			}
			w.mountRemote(p, d, secret, attempt{})
		})
	}()
}

// attempt is where a connection was asked from: a card (the setup, or the
// password asked for), its password field, whether the secret is to be
// remembered, and the disk the setup changes.
type attempt struct {
	card     *card
	secret   *gtk.PasswordEntry
	remember bool
	old      *remote.Disk
}

// mountRemote connects d with secret. From a card, the card says how it
// went, and the secret is kept in the keyring once it works (or forgotten,
// when it is not to be remembered); a secret refused is asked for (again).
// The disk a setup changed gives its place to d.
func (w *window) mountRemote(p *pane, d remote.Disk, secret string, at attempt) {
	c := at.card
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		err := w.app.remote.Connect(ctx, d, secret)
		if err == nil && c != nil {
			var kerr error
			if at.remember && secret != "" {
				kerr = w.app.secrets.Store(ctx, d, secret)
			} else if !at.remember {
				kerr = w.app.secrets.Delete(d.Address)
			}
			if at.old != nil && at.old.Address != d.Address {
				w.app.secrets.Delete(at.old.Address) // moved, or no longer wanted
			}
			if kerr != nil && !errors.Is(kerr, remote.ErrNoKeyring) {
				log.Printf("files: the keyring, for %s: %v", d.Name, kerr)
			}
		}
		gtkx.Post(func() {
			if c != nil && w.card != c {
				return // the card was closed: given up
			}
			if e, ok := remote.AsError(err); ok && e.Reason == remote.Refused {
				if c == nil {
					w.askSecret(p, d, err)
				} else {
					w.cardBusy("")
					w.cardFail(w.remoteError(d, err))
					at.secret.SetText("")
					at.secret.GrabFocus()
				}
				return
			}
			if err != nil {
				if c != nil {
					w.cardBusy("")
					w.cardFail(w.remoteError(d, err))
					return
				}
				w.flash(w.remoteError(d, err))
				return
			}
			if c != nil {
				w.closeCard()
			}
			if at.old != nil {
				w.app.settings.ForgetDisk(at.old.Name)
			}
			w.app.settings.SaveDisk(d)
			w.app.settings.Save()
			p.goTo(w.app.remote.Point(d), true)
			w.app.refreshMounts()
		})
	}()
}

// askSecret asks for the user and the password, or the access key's ID and
// its secret, to connect d with; why is what was refused before.
func (w *window) askSecret(p *pane, d remote.Disk, why error) {
	a, err := remote.Parse(d.Address)
	if err != nil {
		w.flash(addressError(err))
		return
	}
	var c *card
	c = &card{def: 1, buttons: []cardButton{
		{label: i18n.T("Cancel")},
		{label: i18n.T("Connect"), class: "suggested-action", action: func() {
			user := strings.TrimSpace(w.cardEntry.GetText())
			secret := w.cardSecret.GetText()
			if a.KeyPair() && (user == "" || secret == "") {
				w.cardFail(i18n.T("Both the key's ID and its secret are needed."))
				return
			}
			changed := d
			changed.Address = a.WithUser(user).String()
			w.cardFail("")
			w.cardBusy(i18n.T("Connecting…"))
			w.mountRemote(p, changed, secret, attempt{card: c, secret: &w.cardSecret,
				remember: w.cardCheck.GetVisible() && w.cardCheck.GetActive(), old: &d})
		}},
	}}
	parts := cardParts{
		title: i18n.Tf("Sign in to %s", d.Name), text: d.Address,
		entry: true, entryText: a.User, secret: true,
		entryHint: i18n.T("User"), secretHint: i18n.T("Password"),
	}
	if a.KeyPair() {
		parts.title = i18n.Tf("Keys for %s", d.Name)
		parts.entryHint, parts.secretHint = i18n.T("Access key ID"), i18n.T("Secret access key")
	}
	if w.app.keyring {
		parts.check, parts.checked = i18n.T("Remember in the keyring"), true
	}
	w.openCard(c, parts)
	if a.User != "" {
		w.cardSecret.GrabFocus()
	}
	if why != nil {
		w.cardFail(w.remoteError(d, why))
	}
}

// disconnectRemote unmounts a disk, the panes in it going home first.
func (w *window) disconnectRemote(d remote.Disk) {
	w.leave(w.app.remote.Point(d))
	w.flash(i18n.Tf("Disconnecting %s…", d.Name))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute) // what it still has to send
		defer cancel()
		err := w.app.remote.Disconnect(ctx, d)
		gtkx.Post(func() {
			if err != nil {
				w.flash(w.remoteError(d, err))
			} else {
				w.flash(i18n.Tf("%s is disconnected.", d.Name))
			}
			w.app.refreshMounts()
		})
	}()
}

// confirmForget takes a disk off the list, with its secret, disconnecting
// it first.
func (w *window) confirmForget(d remote.Disk) {
	w.openCard(&card{def: 0, buttons: []cardButton{
		{label: i18n.T("Cancel")},
		{label: i18n.T("Forget"), class: "destructive-action", action: func() {
			point := w.app.remote.Point(d)
			w.leave(point)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				err := w.app.remote.Disconnect(ctx, d)
				if err == nil {
					err = w.app.remote.Forget(d)
				}
				if err == nil {
					if derr := w.app.secrets.Delete(d.Address); derr != nil && !errors.Is(derr, remote.ErrNoKeyring) {
						log.Printf("files: forgetting %s's secret: %v", d.Name, derr)
					}
				}
				gtkx.Post(func() {
					if err != nil {
						w.flash(w.remoteError(d, err))
						return
					}
					w.app.settings.ForgetDisk(d.Name)
					w.app.settings.Save()
					w.app.refreshMounts()
				})
			}()
		}},
	}}, cardParts{
		title: i18n.Tf("Forget %s?", d.Name),
		text:  i18n.T("Its address and its password go from here. Nothing on the server changes."),
	})
}

// remoteSide adds the remote disks to the sidebar: each with its free
// space while it is mounted, then the entry that connects another.
func (w *window) remoteSide() {
	w.sideHead(i18n.T("REMOTE DISKS"))
	for _, d := range w.app.settings.Remote {
		point := w.app.remote.Point(d)
		mounted := w.app.mountedAt(point)
		sub, path := d.Address, ""
		var eject func()
		var extra []menuItem
		if mounted {
			path = point
			// Object storage answers a size it does not have (an exabyte).
			if sp, err := files.SpaceOf(point); err == nil && sp.Total > 0 && sp.Total < 1<<50 {
				sub = i18n.Tf("%s free of %s", disks.Size(sp.Free), disks.Size(sp.Total))
			}
			eject = func() { w.disconnectRemote(d) }
			extra = append(extra, menuItem{label: i18n.T("Disconnect"), run: eject})
		}
		change := func() { w.setupRemote(&d) }
		extra = append(extra, menuItem{label: i18n.T("Edit…"), run: change})
		extra = append(extra, menuItem{label: i18n.T("Forget"), run: func() { w.confirmForget(d) }})
		w.sideEntry(d.Name, sub, "folder-remote", path, func() { w.openRemote(w.cur(), d) }, eject, extra...)
	}
	w.sideEntry(i18n.T("Remote disk…"), "", "network-workgroup", "", func() { w.setupRemote(nil) }, nil)
}

// remotePoints are the remote disks' folders, which the devices leave out.
func (a *App) remotePoints() map[string]bool {
	out := map[string]bool{}
	for _, d := range a.settings.Remote {
		out[a.remote.Point(d)] = true
	}
	return out
}

// mountedAt says whether something is mounted at point.
func (a *App) mountedAt(point string) bool {
	return slices.ContainsFunc(a.mounts, func(m files.Mount) bool { return m.Point == point })
}

// mountPoints are the mount table's mount points, for the remote disks.
func mountPoints() ([]string, error) {
	ms, err := files.ReadMounts()
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Point
	}
	return out, err
}

// addressError says what is wrong with an address.
func addressError(err error) string {
	var se *remote.SchemeError
	switch {
	case errors.As(err, &se):
		return i18n.Tf("A remote disk cannot be at %s://.", se.Scheme)
	case errors.Is(err, remote.ErrPassword):
		return i18n.T("Leave the password out of the address: it is asked for, and kept in the keyring.")
	case errors.Is(err, remote.ErrNoServer):
		return i18n.T("A server is needed.")
	case errors.Is(err, remote.ErrNoShare):
		return i18n.T("An SMB disk needs a share: smb://server/share.")
	case errors.Is(err, remote.ErrLineBreak):
		return i18n.T("The address has a line break in it.")
	case errors.Is(err, remote.ErrNoBucket):
		return i18n.T("A bucket is needed.")
	case errors.Is(err, remote.ErrNoURL):
		return i18n.T("A WebDAV URL starts with https:// or http://.")
	case errors.Is(err, remote.ErrPort):
		return i18n.T("A port is a number from 1 to 65535.")
	}
	return i18n.T("An address starts with sftp://, smb://, s3:// …")
}

// remoteError says why d could not be connected or disconnected.
func (w *window) remoteError(d remote.Disk, err error) string {
	a, _ := remote.Parse(d.Address)
	e, ok := remote.AsError(err)
	if !ok {
		if errors.Is(err, context.DeadlineExceeded) {
			return i18n.Tf("%s did not answer in time.", a.Host)
		}
		if errors.Is(err, remote.ErrName) {
			return i18n.Tf("“%s” cannot be a folder's name in ~/Remote.", d.Name)
		}
		return i18n.Tf("%s could not be connected: %s", d.Name, err.Error())
	}
	switch e.Reason {
	case remote.Missing:
		return i18n.Tf("Remote disks need the %s package.", e.Detail)
	case remote.Refused:
		if a.KeyPair() {
			return i18n.T("Those keys were refused.")
		}
		return i18n.T("The server refused that user or password.")
	case remote.HostKey:
		return i18n.Tf("%s's SSH host key is unknown or changed: check it with ssh first.", a.Host)
	case remote.Unreachable:
		return i18n.Tf("%s does not answer.", a.Host)
	case remote.NotFound:
		return i18n.Tf("%s is not there.", d.Address)
	case remote.Occupied:
		return i18n.Tf("%s has files in it: a disk is mounted only on an empty folder.", shortHome(e.Detail))
	case remote.Busy:
		return i18n.Tf("%s is in use: close what has a file open on it.", d.Name)
	case remote.Old:
		return i18n.Tf("This rclone is too old for %s://; 1.60 or later has it.", a.Scheme)
	}
	return i18n.Tf("%s could not be connected: %s", d.Name, e.Detail)
}

// notify shows a desktop notification from Files, sent off the main
// thread.
func notify(title, body string) {
	go func() {
		conn, err := dbus.SessionBus()
		if err == nil {
			err = conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").Call(
				"org.freedesktop.Notifications.Notify", 0, i18n.T("Files"), uint32(0), "folder-remote", title, body,
				[]string{}, map[string]dbus.Variant{"desktop-entry": dbus.MakeVariant("io.github.lyricat.Inua.Files")}, int32(-1)).Err
		}
		if err != nil {
			log.Printf("files: notification: %v", err)
		}
	}()
}
