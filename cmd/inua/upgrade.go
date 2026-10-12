package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/upgrade"
)

// cmdUpgrade checks for a newer release and installs it over the running
// programs, then restarts the shell so it runs the new one.
func cmdUpgrade(args []string) error {
	flags := flag.NewFlagSet("upgrade", flag.ExitOnError)
	check := flags.Bool("check", false, "only report whether a newer release is out")
	force := flags.Bool("force", false, "install even if this version is as new, or built from source")
	want := flags.String("version", "", "install this release (such as v0.1.2) instead of the latest")
	flags.Parse(args)
	if flags.NArg() != 0 {
		usage()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	src := upgrade.GitHub()

	tag := *want
	if tag == "" {
		var err error
		if tag, err = src.Latest(ctx); err != nil {
			return fmt.Errorf("checking for a new release: %w", err)
		}
	} else if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	order, known := upgrade.Compare(version, tag)

	if *check {
		switch {
		case !known:
			fmt.Printf("This inua (%s) is built from source; the latest release is %s:\n%s\n", version, tag, src.ReleaseURL(tag))
		case order < 0:
			fmt.Printf("inua %s is out; you have %s. What's new:\n%s\nInstall it with: inua upgrade\n", strings.TrimPrefix(tag, "v"), version, src.ReleaseURL(tag))
		default:
			fmt.Printf("inua %s is the latest release.\n", version)
		}
		return nil
	}
	switch {
	case *force:
	case !known && *want == "":
		return fmt.Errorf("this inua (%s) is built from source: update it with git pull and make install, "+
			"or replace it with release %s: inua upgrade -force", version, tag)
	case order == 0:
		fmt.Printf("inua %s is already installed.\n", version)
		return nil
	case order > 0 && *want == "":
		fmt.Printf("inua %s is newer than the latest release (%s); nothing to do.\n", version, tag)
		return nil
	}

	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return err
	}
	binDir := filepath.Dir(exe)
	name, err := upgrade.ArchiveName()
	if err != nil {
		return err
	}
	fmt.Printf("Downloading inua %s (%s)…\n", tag, name)
	archive, err := src.Download(ctx, tag, name)
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	data := cmp.Or(os.Getenv("XDG_DATA_HOME"), filepath.Join(home, ".local", "share"))
	programs, err := upgrade.Install(archive, binDir, data)
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("cannot write to %s, where inua is installed (%w); "+
			"install the release in ~/.local instead with: curl -fsSL https://inua.lyric.im/install.sh | sh", binDir, err)
	}
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("update-desktop-database"); err == nil {
		exec.Command("update-desktop-database", "-q", filepath.Join(data, "applications")).Run()
	}
	fmt.Printf("Installed inua %s in %s: %s.\n", strings.TrimPrefix(tag, "v"), binDir, strings.Join(programs, ", "))

	if slices.Contains(programs, "inua-shell") {
		restartShell()
	}
	fmt.Println("Light apps that are open keep the old version until you close them.\n" +
		"To add the release's new sample settings, if any, run: inua init")
	return nil
}

// restartShell has a running inua-shell re-execute itself, which runs the
// newly installed binary (`inua call shell restart`).
func restartShell() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := ipc.Request{Op: "call", Target: "shell", Function: "restart"}
	if _, err := ipc.Call(ctx, ipc.SocketPath(defaultInstance), req); err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return // no shell running; the next one starts new
		}
		fmt.Printf("The shell was not restarted (%v); restart it with: inua call shell restart\n", err)
		return
	}
	fmt.Println("Restarted inua-shell.")
}
