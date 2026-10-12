// Command inua-assistant is the assistant's window: an agent with inua's
// tools (docs/feat/assistant.md). `inua-assistant --ask "…"` asks a
// question; with the window already open, the question goes to it.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/assistant/keys"
	"github.com/lyricat/inua/internal/assistantui"
)

func main() {
	ask := flag.String("ask", "", "a question to ask")
	background := flag.Bool("background", false, "start the assistant without showing its window")
	serveToken := flag.Bool("serve-token", false, "print the Console token from the keyring, creating it if missing")
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory")
	flag.Parse()
	if *serveToken {
		token, err := keys.New().ServeToken(true)
		if err != nil {
			fmt.Fprintln(os.Stderr, "inua-assistant:", err)
			os.Exit(1)
		}
		fmt.Println(token)
		return
	}
	if *ask != "" && activateRunning("ask", []dbus.Variant{dbus.MakeVariant(*ask)}) {
		return
	}
	if *background && *ask == "" && activateRunning("background", nil) {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	assistantui.Register(ctx, *background)
	var args []string
	if *ask != "" {
		args = []string{"--ask", *ask}
	}
	code := appui.Run("assistant", args, *uiDir)
	stop()
	os.Exit(code)
}

// activateRunning reaches the existing process without showing its window
// for a background start (including after the shell restarts).
func activateRunning(action string, params []dbus.Variant) bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	const name = appui.IDBase + ".Assistant"
	var has bool
	if conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, name).Store(&has) != nil || !has {
		return false
	}
	obj := conn.Object(name, "/io/github/lyricat/Inua/Assistant")
	err = obj.Call("org.gtk.Actions.Activate", 0, action, params, map[string]dbus.Variant{}).Err
	if err != nil {
		fmt.Fprintln(os.Stderr, "inua-assistant:", err)
		return false
	}
	return true
}
