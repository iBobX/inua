// Command inua-share-picker is xdg-desktop-portal-hyprland's picker
// (screencopy:custom_picker_binary in ~/.config/hypr/xdph.conf): when an
// app asks to share the screen, the shell's screenshot tool opens for
// choosing a screen, a window or an area, and this prints the choice as
// xdph reads it (docs/portals.md). It prints nothing when the user
// cancels, which xdph takes as no. With no shell running, as in a plain
// Hyprland session reading the same xdph.conf, xdph's own picker asks.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/lyricat/inua/internal/ipc"
)

// version is the release, set by the release build (-X main.version).
var version = "dev"

func main() {
	allowToken := flag.Bool("allow-token", false, "let the app share the same again without asking (xdph's allow_token_by_default)")
	showVersion := flag.Bool("version", false, "print the version")
	flag.Parse()
	if *showVersion {
		fmt.Println("inua-share-picker", version)
		return
	}
	instance := cmp.Or(os.Getenv("INUA_INSTANCE"), "default")
	resp, err := ipc.Call(context.Background(), ipc.SocketPath(instance), ipc.Request{Op: "call", Target: "share", Function: "pick"})
	if err != nil {
		if op := (*net.OpError)(nil); errors.As(err, &op) && op.Op == "dial" {
			hyprlandPicker()
		}
		fmt.Fprintln(os.Stderr, "inua-share-picker:", err)
		os.Exit(1)
	}
	sel, err := Selection(resp.Result, os.Getenv("XDPH_WINDOW_SHARING_LIST"), *allowToken)
	if err != nil {
		fmt.Fprintln(os.Stderr, "inua-share-picker:", err)
		os.Exit(1)
	}
	fmt.Println(sel)
}

// hyprlandPicker runs xdph's own picker in this process's place, with
// the same arguments (--allow-token) and environment; it returns only
// when there is none.
func hyprlandPicker() {
	path, err := exec.LookPath("hyprland-share-picker")
	if err != nil {
		return
	}
	err = syscall.Exec(path, append([]string{path}, os.Args[1:]...), os.Environ())
	fmt.Fprintln(os.Stderr, "inua-share-picker:", err)
}

// Selection is the shell's choice as xdph reads it: "[SELECTION]", the
// flags ("r": a restore token allowed), "/", then the choice, a window
// named by the handle xdph listed for its address.
func Selection(choice, windows string, allowToken bool) (string, error) {
	flags := ""
	if allowToken {
		flags = "r"
	}
	if addr, ok := strings.CutPrefix(choice, "window:"); ok {
		handle, ok := Windows(windows)[strings.ToLower(addr)]
		if !ok {
			return "", fmt.Errorf("xdph did not list window %s", addr)
		}
		choice = "window:" + handle
	}
	if !strings.HasPrefix(choice, "screen:") && !strings.HasPrefix(choice, "window:") && !strings.HasPrefix(choice, "region:") {
		return "", fmt.Errorf("no choice (%q)", choice)
	}
	return "[SELECTION]" + flags + "/" + choice, nil
}

// Windows reads XDPH_WINDOW_SHARING_LIST: each window as
// "HANDLE[HC>]CLASS[HT>]TITLE[HE>]ADDRESS[HA>]", one after another. It
// returns the handles by Hyprland address ("0x55d1c0").
func Windows(list string) map[string]string {
	out := map[string]string{}
	for entry := range strings.SplitSeq(list, "[HA>]") {
		handle, rest, ok := strings.Cut(entry, "[HC>]")
		if !ok {
			continue
		}
		_, rest, _ = strings.Cut(rest, "[HT>]")
		_, addr, ok := strings.Cut(rest, "[HE>]")
		if !ok || handle == "" {
			continue
		}
		addr = strings.ToLower(strings.TrimSpace(addr))
		if !strings.HasPrefix(addr, "0x") {
			addr = "0x" + addr
		}
		out[addr] = strings.TrimSpace(handle)
	}
	return out
}
