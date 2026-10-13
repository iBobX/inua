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
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
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
// secret; later reads are only recorded. As the passphrase dialog says
// it: the secret, then the app in bold and what it runs under. Its button,
// or a click on it, opens Settings → Keyring.
func notify(conn *dbus.Conn, a keyring.Access) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sigs := make(chan *dbus.Signal, 8)
	conn.Signal(sigs)
	defer conn.RemoveSignal(sigs)
	match := []dbus.MatchOption{dbus.WithMatchInterface("org.freedesktop.Notifications")}
	if err := conn.AddMatchSignal(match...); err == nil {
		defer conn.RemoveMatchSignal(match...)
	}
	title, body := readNote(a)
	hints := map[string]dbus.Variant{"desktop-entry": dbus.MakeVariant("io.github.lyricat.Inua.Settings")}
	var id uint32
	err := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").CallWithContext(ctx,
		"org.freedesktop.Notifications.Notify", 0,
		i18n.T("Keyring"), uint32(0), "dialog-password-symbolic", title, body,
		[]string{"default", "", "reads", i18n.T("Show the reads")}, hints, int32(-1)).Store(&id)
	if err != nil {
		log.Printf("notification: %v", err)
		return
	}
	// Until it is closed, or for an hour.
	wait := time.After(time.Hour)
	for {
		select {
		case <-wait:
			return
		case sig := <-sigs:
			if len(sig.Body) < 2 {
				continue
			}
			if n, _ := sig.Body[0].(uint32); n != id {
				continue
			}
			switch sig.Name {
			case "org.freedesktop.Notifications.NotificationClosed":
				return
			case "org.freedesktop.Notifications.ActionInvoked":
				openReads()
				return
			}
		}
	}
}

// readNote is the notification's title and body (in the notification
// markup): "gh read a secret"; "“Password for …” · <b>gh</b> ← claude ←
// zsh".
func readNote(a keyring.Access) (title, body string) {
	who := "<b>" + markup(a.App) + "</b>"
	if a.Chain != "" {
		who += " ← " + markup(a.Chain)
	}
	return i18n.Tf("%s read a secret", a.App), i18n.Tf("“%s” · %s", markup(a.Secret), who)
}

// markup escapes text for the notification markup.
func markup(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// openReads opens Settings → Keyring, with the inua beside this program.
func openReads() {
	inua := "inua"
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), "inua"); fileExists(p) {
			inua = p
		}
	}
	if out, err := exec.Command(inua, "call", "settings", "open", "keyring").CombinedOutput(); err != nil {
		log.Printf("opening Settings: %v %s", err, strings.TrimSpace(string(out)))
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
