package keyring

import (
	"crypto/rand"
	"log"
	"os"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/portal"
)

// The portal's secrets are items with these attributes, as gnome-keyring
// keeps them, so `inua keyring import` brings an app's along.
const (
	portalSchema = "org.freedesktop.portal.Secret"
	portalSize   = 64 // bytes of a new app's secret
	frontendName = "org.freedesktop.portal.Desktop"
)

// PortalSecret is org.freedesktop.impl.portal.Secret: each sandboxed
// app's own secret, which it keeps its passwords encrypted with. It is
// made the first time the app asks, and kept in the default collection.
func (s *Service) PortalSecret() portal.Interface {
	return portal.Interface{Name: "Secret", Version: 1, Methods: portalObj{s}, XML: `
<method name="RetrieveSecret">
 <arg type="o" name="handle" direction="in"/><arg type="s" name="app_id" direction="in"/>
 <arg type="h" name="fd" direction="in"/><arg type="a{sv}" name="options" direction="in"/>
 <arg type="u" name="response" direction="out"/><arg type="a{sv}" name="results" direction="out"/>
</method>`}
}

type portalObj struct{ s *Service }

// RetrieveSecret writes the app's secret to fd. Only the frontend may ask:
// it has checked which app is calling, and any other process could name
// any app.
func (o portalObj) RetrieveSecret(sender dbus.Sender, handle dbus.ObjectPath, appID string, fd dbus.UnixFD, options map[string]dbus.Variant) (uint32, map[string]dbus.Variant, *dbus.Error) {
	f := os.NewFile(uintptr(fd), "secret")
	defer f.Close()
	s := o.s
	var frontend string
	s.conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, frontendName).Store(&frontend)
	if frontend == "" || string(sender) != frontend {
		return portal.Ended, nil, dbus.NewError("org.freedesktop.portal.Error.NotAllowed", []any{"only xdg-desktop-portal may ask"})
	}
	if appID == "" { // an app outside a sandbox keeps its secrets itself
		return portal.Ended, nil, nil
	}
	secret, label, err := s.appSecret(appID)
	if err != nil {
		log.Printf("portal secret for %s: %v", appID, err)
		return portal.Ended, nil, nil
	}
	defer clear(secret)
	if _, err := f.Write(secret); err != nil {
		log.Printf("portal secret for %s: %v", appID, err)
		return portal.Ended, nil, nil
	}
	if s.OnRead != nil {
		coll := s.defaultCollection()
		item := s.store.Search(coll, map[string]string{"xdg:schema": portalSchema, "app_id": appID})
		read := Access{Time: time.Now(), App: appID, Secret: label, Collection: coll, Command: "xdg-desktop-portal", Bus: string(sender)}
		if len(item) > 0 {
			read.Pass = s.store.Where(coll, item[0].ID)
		}
		s.mu.Lock()
		first := !s.readers[appID]
		s.readers[appID] = true
		s.mu.Unlock()
		s.OnRead(read, first)
	}
	return portal.Success, map[string]dbus.Variant{}, nil
}

func (s *Service) defaultCollection() string {
	if c := s.store.Alias("default"); c != "" {
		return c
	}
	return Login
}

// appSecret reads the app's secret, making it the first time.
func (s *Service) appSecret(appID string) ([]byte, string, error) {
	coll := s.defaultCollection()
	attrs := map[string]string{"xdg:schema": portalSchema, "app_id": appID}
	if items := s.store.Search(coll, attrs); len(items) > 0 {
		it := items[0]
		secret, _, err := s.store.Secret(coll, it.ID, appID+"\t"+it.Label)
		return secret, it.Label, err
	}
	secret := make([]byte, portalSize)
	rand.Read(secret)
	label := i18n.Tf("Passwords of %s", appID)
	if _, _, err := s.store.CreateItem(coll, label, attrs, secret, "application/octet-stream", false); err != nil {
		clear(secret)
		return nil, "", err
	}
	return secret, label, nil
}
