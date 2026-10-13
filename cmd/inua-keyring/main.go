// Command inua-keyring is the Secret Service (org.freedesktop.secrets)
// kept in pass: D-Bus starts it on the first request once `inua keyring
// enable` installed its activation file (docs/feat/keyring.md).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/keyring"
)

// version is the release, set by the release build (-X main.version).
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version")
	flag.Parse()
	if *showVersion {
		fmt.Println("inua-keyring", version)
		return
	}
	log.SetPrefix("inua-keyring: ")
	cfg, warnings, err := config.Load(config.DefaultPath())
	for _, w := range warnings {
		log.Print(w)
	}
	if err != nil {
		log.Print(err)
	}
	i18n.Init(cfg.Language) // for the notifications
	store, err := keyring.OpenPass(keyring.Pass{Dir: keyring.StoreDir(), Folder: cfg.Keyring.Folder})
	if err != nil {
		log.Fatal(err)
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("serving %s from %s/%s", keyring.Name, keyring.StoreDir(), cfg.Keyring.Folder)
	svc := keyring.NewService(conn, store)
	record := keyring.AccessPath()
	svc.OnRead = func(a keyring.Access, first bool) {
		log.Printf("secret %q read by %s (pid %d)", a.Secret, a.App, a.PID)
		if err := keyring.RecordAccess(record, a); err != nil {
			log.Printf("access record: %v", err)
		}
		if first {
			// The settings' switch, as it is now: it changes while this runs.
			if now, _, err := config.Load(config.DefaultPath()); err == nil && now.Keyring.Notify {
				go notify(conn, a)
			}
		}
	}
	if err := svc.Serve(ctx); err != nil {
		log.Fatal(err)
	}
}

// notify tells, in a desktop notification, that an app read its first
// secret; later reads are only recorded.
func notify(conn *dbus.Conn, a keyring.Access) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hints := map[string]dbus.Variant{"desktop-entry": dbus.MakeVariant("io.github.lyricat.Inua.Settings")}
	err := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").CallWithContext(ctx,
		"org.freedesktop.Notifications.Notify", 0,
		i18n.T("Keyring"), uint32(0), "dialog-password-symbolic",
		i18n.Tf("%s read a secret", a.App),
		i18n.Tf("“%s”, by pid %d. Settings → Keyring lists every read.", a.Secret, a.PID),
		[]string{}, hints, int32(-1)).Err
	if err != nil {
		log.Printf("notification: %v", err)
	}
}
