// Command inua-files is inua's file manager (docs/files.md): inua-files
// [PATH|URI ...] opens windows on folders, with files selected in theirs.
// One process serves every window: a start while it runs hands its paths
// over through D-Bus and exits. inua-files -default makes it the
// desktop's file manager, and -default=false gives the job back. The
// process also serves the file chooser portal (docs/picker.md):
// inua-files -picker makes it the session's file chooser, -picker=false
// takes that back, and D-Bus starts it with -portal for a request.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/apps/files/chooser"
	"github.com/lyricat/inua/internal/apps/files/fm1"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/filesui"
	"github.com/lyricat/inua/internal/portal"
	"github.com/lyricat/inua/internal/portalconf"
)

const desktopID = "io.github.lyricat.Inua.Files.desktop"

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	var makeDefault, makePicker optionalBool
	flag.Var(&makeDefault, "default", "make inua-files the desktop's file manager (-default=false gives the job back)")
	flag.Var(&makePicker, "picker", "make inua-files the session's file chooser (-picker=false gives the job back)")
	forPortal := flag.Bool("portal", false, "started by D-Bus for the portal: no window until one is asked for")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-files [-ui-dir DIR] [PATH|URI ...]")
		fmt.Fprintln(os.Stderr, "       inua-files -default[=false] | -picker[=false]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if makeDefault.set || makePicker.set {
		var err error
		if makeDefault.set {
			err = setDefault(makeDefault.value)
		}
		if err == nil && makePicker.set {
			err = setPicker(makePicker.value)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "inua-files:", err)
			os.Exit(1)
		}
		return
	}
	args := flag.Args()

	app := filesui.New()
	app.Portal = *forPortal
	if conn, err := dbus.ConnectSessionBus(); err == nil {
		ok, err := fm1.Claim(conn, app)
		if err == nil && !ok {
			if *forPortal {
				return // the one running serves the portal
			}
			// Running already: it opens the windows.
			if err := fm1.Hand(conn, args); err != nil {
				fmt.Fprintln(os.Stderr, "inua-files:", err)
				os.Exit(1)
			}
			return
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "inua-files: D-Bus:", err)
		} else {
			servePortal(conn, app)
		}
	}
	filesui.Register(app)
	os.Exit(appui.Run("files", args, *uiDir))
}

// optionalBool is a flag that may be given with or without a value.
type optionalBool struct{ set, value bool }

func (b *optionalBool) IsBoolFlag() bool { return true }
func (b *optionalBool) String() string   { return "" }
func (b *optionalBool) Set(s string) error {
	switch strings.ToLower(s) {
	case "true", "1", "yes":
		b.value = true
	case "false", "0", "no":
		b.value = false
	default:
		return fmt.Errorf("%q is not true or false", s)
	}
	b.set = true
	return nil
}

// serviceFile is the D-Bus service that starts inua-files when something
// asks for org.freedesktop.FileManager1; in the user's data folder it
// outranks the one Nautilus installs in /usr/share.
func serviceFile() string {
	return filepath.Join(files.DataHome(), "dbus-1", "services", fm1.Standard+".service")
}

// setDefault makes inua-files the file manager for folders and for
// "Show in folder", or gives both back to Nautilus when it is there.
func setDefault(on bool) error {
	if on {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		service := fmt.Sprintf("[D-BUS Service]\nName=%s\nExec=%s\n", fm1.Standard, exe)
		if err := os.MkdirAll(filepath.Dir(serviceFile()), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(serviceFile(), []byte(service), 0o644); err != nil {
			return err
		}
		if err := exec.Command("xdg-mime", "default", desktopID, "inode/directory").Run(); err != nil {
			return fmt.Errorf("xdg-mime: %w", err)
		}
		fmt.Println("inua-files opens folders now, and \"Show in folder\" starts it.")
		fmt.Println("Set `$files = inua-files` in ~/.config/hypr/hyprland.conf for Super+E.")
		return nil
	}
	if err := os.Remove(serviceFile()); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := exec.LookPath("nautilus"); err == nil {
		if err := exec.Command("xdg-mime", "default", "org.gnome.Nautilus.desktop", "inode/directory").Run(); err != nil {
			return fmt.Errorf("xdg-mime: %w", err)
		}
		fmt.Println("Nautilus opens folders again.")
		return nil
	}
	fmt.Println("Removed the D-Bus service; choose another app for folders with xdg-mime.")
	return nil
}

// servePortal serves the file chooser and the app chooser under Files'
// backend name; a request waits on its D-Bus goroutine for its window.
func servePortal(conn *dbus.Conn, app *filesui.App) {
	svc := chooser.New(conn, app, app)
	if _, err := portal.Export(conn, svc.Interfaces()...); err != nil {
		fmt.Fprintln(os.Stderr, "inua-files: portal:", err)
		return
	}
	if err := portal.Own(conn, portal.FilesName); err != nil {
		fmt.Fprintln(os.Stderr, "inua-files: portal:", err)
	}
}

// setPicker makes the picker the session's file chooser and app chooser,
// or gives them back (docs/portals.md).
func setPicker(on bool) error {
	d := portalconf.Default()
	left, err := d.Set(on, "FileChooser", "AppChooser")
	if err != nil {
		return err
	}
	msg, err := d.Apply(left)
	if err != nil {
		return err
	}
	if on {
		fmt.Println("inua-files chooses files for apps now.")
	} else {
		fmt.Println("Apps choose files with GTK's file chooser again.")
	}
	fmt.Println(msg)
	return nil
}
