// Command inua-shell runs the GTK4 shell: bars, launcher, control center and
// tray on Hyprland (internal/ui). Control it with `inua call`:
//
//	inua call launcher toggle|apps|windows|close
//	inua call control  toggle|open|close
//	inua call calendar toggle|open|close
//	inua call settings toggle|open [PAGE]|close
//	inua call shell    state|close|restart|snapshot DIR|tray-menu N
//
// `shell restart` re-executes the binary in place (same PID and log), picking
// up a rebuilt binary; bind it to a key in hyprland.conf.
//
// Settings are read from -config (default ~/.config/inua/config.toml; see
// config/inua/config.toml in the repository) at start, and again on restart.
//
// It is a separate binary from inua because GTK loads during package
// initialization (puregotk), which would add ~50 ms to every `inua call`
// call from a key binding.
package main

import (
	"cmp"
	"flag"
	"log"
	"os"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/ui"
)

func main() {
	instance := flag.String("n", cmp.Or(os.Getenv("INUA_INSTANCE"), "default"), "instance name; names the control socket (default $INUA_INSTANCE, else \"default\")")
	noLayer := flag.Bool("no-layer", false, "use normal windows (for compositors without layer-shell, e.g. GNOME)")
	uiDir := flag.String("ui-dir", "", "load layouts and style.css from this directory (e.g. ui) instead of the embedded copies")
	configPath := flag.String("config", config.DefaultPath(), "settings file (missing: defaults)")
	flag.Parse()
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	desktop.ReapInherited()

	// A broken settings file never keeps the shell from starting: problems
	// are logged (and shown by `inua config check`) and defaults fill in.
	cfg, warnings, err := config.Load(*configPath)
	if err != nil {
		log.Printf("config: %v; using defaults", err)
	}
	for _, w := range warnings {
		log.Printf("config: %s", w)
	}
	os.Exit(ui.Run(ui.Options{NoLayer: *noLayer, UIDir: *uiDir, Instance: *instance, Config: cfg, ConfigPath: *configPath}))
}
