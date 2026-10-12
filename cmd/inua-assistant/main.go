// Command inua-assistant is the assistant's window: an agent with inua's
// tools (docs/feat/assistant.md). `inua-assistant --ask "…"` asks a
// question; with the window already open, the question goes to it.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/assistantui"
)

func main() {
	ask := flag.String("ask", "", "a question to ask")
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory")
	flag.Parse()
	if *ask != "" && askRunning(*ask) {
		return
	}
	assistantui.Register()
	var args []string
	if *ask != "" {
		args = []string{"--ask", *ask}
	}
	os.Exit(appui.Run("assistant", args, *uiDir))
}

// askRunning gives the question to an open assistant through its "ask"
// action (GApplication puts actions on D-Bus); false when none is open.
func askRunning(q string) bool {
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
	err = obj.Call("org.gtk.Actions.Activate", 0, "ask", []dbus.Variant{dbus.MakeVariant(q)}, map[string]dbus.Variant{}).Err
	if err != nil {
		fmt.Fprintln(os.Stderr, "inua-assistant:", err)
		return false
	}
	return true
}
