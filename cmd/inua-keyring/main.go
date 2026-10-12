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

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/config"
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
	if err := keyring.NewService(conn, store).Serve(ctx); err != nil {
		log.Fatal(err)
	}
}
