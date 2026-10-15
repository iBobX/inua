// Command inua-files is inua's file manager (docs/files.md): inua-files
// [PATH|URI ...] opens windows on folders, with files selected in theirs.
// One process serves every window: a start while it runs hands its paths
// over through D-Bus and exits. inua-files -default makes it the
// desktop's file manager, and -default=false gives the job back.
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
	"github.com/lyricat/inua/internal/apps/files/fm1"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/filesui"
)

const desktopID = "io.github.lyricat.Inua.Files.desktop"

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	var makeDefault optionalBool
	flag.Var(&makeDefault, "default", "make inua-files the desktop's file manager (-default=false gives the job back)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-files [-ui-dir DIR] [PATH|URI ...]")
		fmt.Fprintln(os.Stderr, "       inua-files -default[=false]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if makeDefault.set {
		if err := setDefault(makeDefault.value); err != nil {
			fmt.Fprintln(os.Stderr, "inua-files:", err)
			os.Exit(1)
		}
		return
	}
	args := flag.Args()

	app := filesui.New()
	if conn, err := dbus.ConnectSessionBus(); err == nil {
		ok, err := fm1.Claim(conn, app)
		if err == nil && !ok {
			// Running already: it opens the windows.
			if err := fm1.Hand(conn, args); err != nil {
				fmt.Fprintln(os.Stderr, "inua-files:", err)
				os.Exit(1)
			}
			return
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "inua-files: D-Bus:", err)
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
