// Command inua-keyring is the Secret Service (org.freedesktop.secrets)
// kept in pass: D-Bus starts it on the first request once `inua keyring
// enable` installed its activation file (docs/feat/keyring.md).
package main

import (
	"cmp"
	"context"
	"encoding/json"
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

	sys "golang.org/x/sys/unix"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/keyring"
	"github.com/lyricat/inua/internal/pinentry"
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
	// The user's other programs may not read this one's memory, where the
	// unlocked secrets are (ptrace, /proc/PID/mem), nor its core dumps.
	if err := sys.Prctl(sys.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		log.Printf("prctl: %v", err)
	}
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
	store.KeepUnlocked(keyring.NewVault())
	svc := keyring.NewService(conn, store)
	svc.Trust = keyring.NewTrust(keyring.TrustPath(), store.TrustKey)
	svc.Ask = askShell
	svc.Untrusted = func() keyring.Answer {
		if now, _, err := config.Load(config.DefaultPath()); err == nil && now.Keyring.Untrusted == "deny" {
			return keyring.Deny
		}
		return keyring.AllowOnce
	}
	go lockOnSleep(ctx, store)
	go lockWhenIdle(ctx, store)
	record := keyring.AccessPath()
	svc.OnRead = func(a keyring.Access, first bool) {
		log.Printf("secret %q read by %s (pid %d) %s", a.Secret, a.App, a.PID, a.Decision)
		if err := keyring.RecordAccess(record, a); err != nil {
			log.Printf("access record: %v", err)
		}
		if first && a.Decision != keyring.DecDenied {
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

// askShell asks, in the shell's passphrase dialog, whether an app may read
// a secret; an error when no shell listens.
func askShell(ctx context.Context, q keyring.Question) (keyring.Answer, error) {
	ap := &pinentry.Approval{Program: q.Identity.Exe, Script: q.Identity.Script, Flatpak: q.Identity.Flatpak,
		Package: q.Package, Tampered: q.Tampered, Reseal: q.Reseal, Apps: q.Apps, AllowPath: q.Path}
	if ap.Package == "" && q.Identity.System {
		ap.Package = keyring.Package(q.Identity.Exe)
	}
	if q.Was != nil {
		ap.Changed, ap.Since, ap.WasPackage = true, q.Was.Since, q.Was.Package
	}
	req := pinentry.Request{Mode: pinentry.Approve, App: q.App, Chain: q.Chain, Keyring: true,
		Secret: q.Secret, Entry: q.Entry, Approval: ap}
	arg, err := json.Marshal(req)
	if err != nil {
		return keyring.Deny, err
	}
	sock := ipc.SocketPath(cmp.Or(os.Getenv("INUA_INSTANCE"), "default"))
	resp, err := ipc.Call(ctx, sock, ipc.Request{Op: "call", Target: "password", Function: string(pinentry.Approve), Args: []string{string(arg)}})
	switch {
	case err == nil && resp.Result == "always":
		return keyring.AlwaysAllow, nil
	case err == nil && resp.Result == "once":
		return keyring.AllowOnce, nil
	case resp.Error == "cancelled":
		return keyring.Deny, nil
	case err == nil:
		return keyring.Deny, fmt.Errorf("the shell answered %q", resp.Result)
	}
	return keyring.Deny, err
}

// lockOnSleep locks the keyring before the machine suspends or
// hibernates (logind's PrepareForSleep).
func lockOnSleep(ctx context.Context, store *keyring.Store) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		log.Printf("system bus: %v; not locking before sleep", err)
		return
	}
	defer conn.Close()
	match := []dbus.MatchOption{dbus.WithMatchInterface("org.freedesktop.login1.Manager"), dbus.WithMatchMember("PrepareForSleep")}
	if err := conn.AddMatchSignal(match...); err != nil {
		log.Printf("logind: %v; not locking before sleep", err)
		return
	}
	sigs := make(chan *dbus.Signal, 4)
	conn.Signal(sigs)
	for {
		select {
		case <-ctx.Done():
			return
		case sig := <-sigs:
			if len(sig.Body) == 1 && sig.Body[0] == true {
				log.Print("locking: the machine sleeps")
				store.Lock()
			}
		}
	}
}

// lockWhenIdle locks the keyring once no secret was read for
// keyring.lock_after minutes, read again each time, as Settings changes
// it.
func lockWhenIdle(ctx context.Context, store *keyring.Store) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		on, last := store.Unlocked()
		if !on {
			continue
		}
		cfg, _, err := config.Load(config.DefaultPath())
		if err != nil || cfg.Keyring.LockAfter <= 0 {
			continue
		}
		if time.Since(last) >= time.Duration(cfg.Keyring.LockAfter)*time.Minute {
			log.Printf("locking: no read for %v", time.Duration(cfg.Keyring.LockAfter)*time.Minute)
			store.Lock()
		}
	}
}
