package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/keyring"
)

// cmdKeyring sets up inua-keyring, the Secret Service kept in pass
// (docs/feat/keyring.md).
func cmdKeyring(args []string) error {
	dry := len(args) == 2 && args[0] == "import" && (args[1] == "-n" || args[1] == "--dry-run")
	if len(args) != 1 && !dry {
		usage()
	}
	cfg, _, err := config.Load(config.DefaultPath())
	if err != nil {
		return err
	}
	pass := keyring.Pass{Dir: keyring.StoreDir(), Folder: cfg.Keyring.Folder}
	switch args[0] {
	case "status":
		return keyringStatus(pass)
	case "import":
		if dry {
			return keyringDryRun()
		}
		return keyringImport(pass)
	case "enable":
		return keyringEnable(pass)
	case "disable":
		return keyringDisable()
	case "replace":
		return keyringReplace(pass)
	}
	usage()
	return nil
}

// activationFile is the D-Bus activation file that starts inua-keyring;
// the user's services directory wins over /usr/share's gnome-keyring.
func activationFile() string {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "dbus-1", "services", keyring.Name+".service")
}

func activationIsOurs() bool {
	b, err := os.ReadFile(activationFile())
	return err == nil && strings.Contains(string(b), "inua-keyring")
}

func keyringStatus(pass keyring.Pass) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	who, ours := keyring.Serving(conn)
	switch {
	case who == "":
		fmt.Println("Secret Service: nobody serves it now")
	case ours:
		fmt.Println("Secret Service:", who, "(inua-keyring)")
	default:
		fmt.Println("Secret Service:", who)
	}
	if activationIsOurs() {
		fmt.Println("Started by D-Bus: inua-keyring (" + activationFile() + ")")
	} else {
		fmt.Println("Started by D-Bus: not inua-keyring (inua keyring enable)")
	}
	fmt.Printf("Items: %s/%s\n", pass.Dir, pass.Folder)
	if err := pass.Ready(); err != nil {
		fmt.Println("  ", err)
		return nil
	}
	store, err := keyring.OpenPass(pass)
	if err != nil {
		return err
	}
	for _, c := range store.Collections() {
		if c.ID == keyring.Session {
			continue
		}
		items, _ := store.Items(c.ID)
		fmt.Printf("  %s (%s): %d items\n", c.Label, c.ID, len(items))
	}
	return nil
}

func keyringImport(pass keyring.Pass) error {
	store, err := keyring.OpenPass(pass)
	if err != nil {
		return err
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	who, ours := keyring.Serving(conn)
	if ours {
		return errors.New("inua-keyring serves the Secret Service already: nothing to import from")
	}
	c, err := keyring.Connect(conn, keyring.AlgDH)
	if err != nil {
		return err
	}
	defer c.Close()
	fmt.Printf("Copying from %s into %s/%s; it may ask to unlock its keyring, and gpg for your key …\n", cmp.Or(who, "the Secret Service"), pass.Dir, pass.Folder)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	rep, err := keyring.Import(ctx, c, store)
	if err != nil {
		return err
	}
	fmt.Printf("Added %d, updated %d, already there %d (collections: %s).\n", rep.Added, rep.Updated, rep.Same, strings.Join(rep.Collections, ", "))
	return nil
}

// keyringDryRun lists what import would copy, reading no secret.
func keyringDryRun() error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	who, ours := keyring.Serving(conn)
	if ours {
		return errors.New("inua-keyring serves the Secret Service already: nothing to import from")
	}
	c, err := keyring.Connect(conn, keyring.AlgPlain)
	if err != nil {
		return err
	}
	defer c.Close()
	cols, err := c.Collections()
	if err != nil {
		return err
	}
	fmt.Println("In", who+":")
	for _, rc := range cols {
		if strings.HasSuffix(string(rc.Path), "/session") {
			continue
		}
		var items []dbus.ObjectPath
		if v, err := conn.Object(keyring.Name, rc.Path).GetProperty("org.freedesktop.Secret.Collection.Items"); err == nil {
			v.Store(&items)
		}
		into := "a collection of its own"
		if rc.Default {
			into = "login"
		}
		fmt.Printf("  %s: %d items, into %s\n", rc.Label, len(items), into)
		for _, p := range items {
			var label string
			if v, err := conn.Object(keyring.Name, p).GetProperty("org.freedesktop.Secret.Item.Label"); err == nil {
				v.Store(&label)
			}
			fmt.Println("    " + label)
		}
	}
	return nil
}

func keyringEnable(pass keyring.Pass) error {
	path, err := writeActivation(pass)
	if err != nil {
		return err
	}
	if conn, err := dbus.ConnectSessionBus(); err == nil {
		defer conn.Close()
		if who, ours := keyring.Serving(conn); who != "" && !ours {
			fmt.Printf("Wrote %s.\n\n%s serves the Secret Service now; inua-keyring takes over once it stops.\n", path, who)
			fmt.Println("If you have not yet, copy its items first:  inua keyring import")
			fmt.Println("Then turn gnome-keyring's service off (for every app; log in again after):")
			fmt.Println("  systemctl --user disable --now " + strings.Join(gnomeKeyringUnits, " "))
			fmt.Println("Or have inua do all three:  inua keyring replace")
			return nil
		}
	}
	fmt.Printf("Wrote %s: D-Bus starts inua-keyring when an app asks for a secret.\n", path)
	return nil
}

// writeActivation installs the activation file and has D-Bus read it.
func writeActivation(pass keyring.Pass) (string, error) {
	if err := pass.Ready(); err != nil {
		return "", err
	}
	exe, err := exec.LookPath("inua-keyring")
	if err != nil {
		if self, err2 := os.Executable(); err2 == nil {
			exe = filepath.Join(filepath.Dir(self), "inua-keyring")
		}
		if _, err := os.Stat(exe); err != nil {
			return "", errors.New("inua-keyring is not installed")
		}
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return "", err
	}
	path := activationFile()
	if _, err := os.Stat(path); err == nil && !activationIsOurs() {
		return "", fmt.Errorf("%s exists and is not inua-keyring's; move it away first", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	content := fmt.Sprintf("# Written by `inua keyring enable`; `inua keyring disable` removes it.\n[D-BUS Service]\nName=%s\nExec=%s\n", keyring.Name, exe)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	if conn, err := dbus.ConnectSessionBus(); err == nil {
		conn.BusObject().Call("org.freedesktop.DBus.ReloadConfig", 0)
		conn.Close()
	}
	return path, nil
}

// gnomeKeyringUnits start gnome-keyring in the user's systemd.
var gnomeKeyringUnits = []string{"gnome-keyring-daemon.service", "gnome-keyring-daemon.socket"}

// enabledUnits are those of units the user's systemd has enabled.
func enabledUnits(units []string) []string {
	var on []string
	for _, u := range units {
		out, _ := exec.Command("systemctl", "--user", "is-enabled", u).Output()
		if strings.TrimSpace(string(out)) == "enabled" {
			on = append(on, u)
		}
	}
	return on
}

// keyringReplace makes inua-keyring the Secret Service, asking first when
// another keyring serves it (yes by default): it copies that keyring's
// items into pass, installs the activation file, and turns gnome-keyring's
// systemd units off.
func keyringReplace(pass keyring.Pass) error {
	if err := pass.Ready(); err != nil {
		return err
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("no session bus (%v): run it in your desktop session", err)
	}
	defer conn.Close()
	who, ours := keyring.Serving(conn)
	if ours && activationIsOurs() {
		fmt.Println("inua-keyring serves the Secret Service already")
		return nil
	}
	units := enabledUnits(gnomeKeyringUnits)
	if who == "" && !activationIsOurs() && activatable(conn) {
		who = "the keyring D-Bus starts on demand"
		if len(units) > 0 {
			who = "gnome-keyring (started on demand)"
		}
	}
	if who != "" && !ours {
		if !askYes(fmt.Sprintf("%s keeps apps' secrets. Replace it with inua-keyring, keeping them in pass under %s/ (its items are copied first)?", who, pass.Folder)) {
			fmt.Println("Left as it is.")
			return nil
		}
		if err := keyringImport(pass); err != nil {
			return fmt.Errorf("copying its items: %w (nothing was replaced)", err)
		}
	}
	path, err := writeActivation(pass)
	if err != nil {
		return err
	}
	fmt.Println("Wrote", path)
	if len(units) > 0 {
		args := append([]string{"--user", "disable", "--now"}, units...)
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		fmt.Println("Turned off", strings.Join(units, " and "))
	}
	// The first request starts whoever serves now: inua-keyring, unless
	// another keyring still runs.
	c, err := keyring.Connect(conn, keyring.AlgPlain)
	if err == nil {
		defer c.Close()
	}
	if err == nil && c.Ours() {
		fmt.Println("inua-keyring serves the Secret Service now. To undo: inua keyring disable")
		return nil
	}
	now, _ := keyring.Serving(conn)
	fmt.Printf("%s still serves the Secret Service; inua-keyring takes over once it stops (log in again).\n", cmp.Or(now, "nobody"))
	return nil
}

// activatable reports whether D-Bus can start a Secret Service.
func activatable(conn *dbus.Conn) bool {
	var names []string
	conn.BusObject().Call("org.freedesktop.DBus.ListActivatableNames", 0).Store(&names)
	return slices.Contains(names, keyring.Name)
}

func keyringDisable() error {
	path := activationFile()
	if !activationIsOurs() {
		return fmt.Errorf("%s is not inua-keyring's: nothing to do", path)
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if conn, err := dbus.ConnectSessionBus(); err == nil {
		conn.BusObject().Call("org.freedesktop.DBus.ReloadConfig", 0)
		conn.Close()
	}
	fmt.Printf("Removed %s. A running inua-keyring keeps serving until it stops (log out, or pkill inua-keyring).\n", path)
	fmt.Println("To bring gnome-keyring back:  systemctl --user enable --now " + strings.Join(gnomeKeyringUnits, " "))
	return nil
}
