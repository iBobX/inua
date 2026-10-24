package main

import (
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
	"github.com/lyricat/inua/internal/upgrade"
	layouts "github.com/lyricat/inua/ui"
)

// cmdFetch prints the card of Settings → About → System as neofetch and
// its kind print theirs: inua's mark beside the facts, in the theme's
// accent on the terminal's own ground (or the theme's, -ground), or the
// facts as Markdown or JSON. Read as the page reads it, Hyprland's version and
// screens from its socket when it runs.
func cmdFetch(args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	markdown := fs.Bool("markdown", false, "the facts as Markdown, as Copy text gives them")
	asJSON := fs.Bool("json", false, "the card as JSON")
	plain := fs.Bool("no-color", false, "no colours (also when NO_COLOR is set or the output is not a terminal)")
	ground := fs.Bool("ground", false, "on the theme's own ground, all in its colours, as the card is on the page")
	theme := fs.String("theme", "", "the colours of this theme (yoru, washi, … or one of yours), not the one in use")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua fetch [-theme NAME] [-ground] [-markdown | -json] [-no-color]")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if fs.NArg() > 0 {
		fs.Usage()
		os.Exit(2)
	}
	cfg, _, _ := config.Load(config.DefaultPath()) // a broken file leaves the defaults
	i18n.Init(cfg.Language)

	inua := strings.TrimPrefix(version, "v")
	link := upgrade.Repo
	if upgrade.IsRelease(version) {
		link = upgrade.Repo + "/releases/tag/v" + inua
	}
	name := cmp.Or(*theme, cfg.Style.Theme, "yoru")
	if !slices.Contains(cfg.ThemeNames(), name) {
		return fmt.Errorf("no theme %q: want %s", name, strings.Join(cfg.ThemeNames(), ", "))
	}
	css := cfg.ThemeCSS(name, func(file string) string {
		b, _ := layouts.FS.ReadFile(file)
		return string(b)
	})
	wm, screens := readHyprland()
	card := shell.NewCard(system.ReadSysInfo("/"), wm, inua, screens, readBattery(), shell.ThemeTitle(css, name), link, time.Now())

	switch {
	case *asJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(card)
	case *markdown:
		fmt.Print(card.Text())
		return nil
	}
	var ink shell.CardInk
	if !*plain && os.Getenv("NO_COLOR") == "" && isTerminal(os.Stdout) {
		ink = shell.ANSIInk(config.ThemeColors(css))
	}
	fmt.Print(card.Terminal(ink, *ground))
	return nil
}

// readHyprland is the compositor with its version and the screens, as
// Hyprland's socket tells them; "Hyprland" alone and no screens without it.
func readHyprland() (string, []shell.CardScreen) {
	paths, err := hypr.PathsFromEnv()
	if err != nil {
		return "", nil
	}
	client := hypr.NewClient(paths)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	wm := "Hyprland"
	if b, err := client.Request(ctx, "j/version"); err == nil {
		var v struct{ Tag, Version string }
		if json.Unmarshal(b, &v) == nil {
			wm += " " + strings.TrimPrefix(cmp.Or(v.Tag, v.Version), "v")
		}
	}
	var screens []shell.CardScreen
	if b, err := client.Request(ctx, "j/monitors"); err == nil {
		var mons []struct {
			Width, Height int
			Scale         float64
			RefreshRate   float64 `json:"refreshRate"`
		}
		if json.Unmarshal(b, &mons) == nil {
			for _, m := range mons {
				screens = append(screens, shell.CardScreen{Width: m.Width, Height: m.Height, Scale: m.Scale, Hz: math.Round(m.RefreshRate)})
			}
		}
	}
	return wm, screens
}

// readBattery is UPower's battery as it is now, for the card's stamp; none
// when UPower does not answer within a moment.
func readBattery() system.BatteryState {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	var bat system.BatteryState
	system.NewBattery(system.SystemBus).Run(ctx, func(st system.BatteryState) {
		bat = st
		cancel() // the first reading is the one
	})
	return bat
}

// isTerminal reports whether f is a terminal, not a file or a pipe.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
