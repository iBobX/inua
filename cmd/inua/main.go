// Command inua is the shell's command line: it controls a running shell and
// inspects Hyprland. The GTK4 shell itself is cmd/inua-shell.
//
//	inua call [-n instance] <target> <function> [args...]   control a running shell
//	inua show [-n instance]                                  list what call accepts
//	inua hypr dump | watch                           inspect Hyprland state
//	inua config check [path] | path                  check the settings file
//	inua config hyprland [path]                      check that Hyprland starts inua-shell and sources inua.conf
//	inua config apply                                write hypr/inua.conf from [input] and reload Hyprland
//	inua init [-force] [-dir dir]                    write the sample configuration
//
// The instance name defaults to $INUA_INSTANCE, else "default".
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	samples "github.com/lyricat/inua/config"
	"github.com/lyricat/inua/internal/bootstrap"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/hyprconf"
	"github.com/lyricat/inua/internal/ipc"
)

// version is the release, set by the release build (-X main.version).
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "call", "show":
		err = cmdControl(os.Args[1], os.Args[2:])
	case "hypr":
		err = cmdHypr(os.Args[2:])
	case "config":
		err = cmdConfig(os.Args[2:])
	case "init":
		err = cmdInit(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("inua", version)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "inua:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  inua call [-n instance] <target> <function> [args...]
  inua show [-n instance]
  inua hypr dump|watch
  inua config check [path] | path
  inua config hyprland [path]
  inua config apply
  inua init [-force] [-dir dir]
  inua version`)
	os.Exit(2)
}

// defaultInstance names the control socket when -n is not given.
var defaultInstance = cmp.Or(os.Getenv("INUA_INSTANCE"), "default")

// cmdControl sends one request to a running shell's control socket: op
// "call" with a target, function and arguments, or "show".
func cmdControl(op string, args []string) error {
	fs := flag.NewFlagSet(op, flag.ExitOnError)
	instance := fs.String("n", defaultInstance, "instance name")
	fs.Parse(args)
	rest := fs.Args()
	req := ipc.Request{Op: op}
	switch {
	case op == "show" && len(rest) == 0:
	case op == "call" && len(rest) >= 2:
		req.Target, req.Function, req.Args = rest[0], rest[1], rest[2:]
	default:
		usage()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := ipc.Call(ctx, ipc.SocketPath(*instance), req)
	if err != nil {
		return err
	}
	if resp.Result != "" {
		fmt.Println(resp.Result)
	}
	return nil
}

// printer is a hypr.Handler that prints to stdout.
type printer struct{ events bool }

func (p printer) OnSnapshot(s hypr.Snapshot) {
	fmt.Printf("== snapshot: focused monitor %q, active toplevel %q\n", s.FocusedMonitor, s.ActiveToplevel)
	for _, m := range s.Monitors {
		fmt.Printf("monitor   %-10s id=%d %dx%d@%d,%d focused=%v ws=%d\n", m.Name, m.ID, m.Width, m.Height, m.X, m.Y, m.Focused, m.ActiveWorkspaceID)
	}
	for _, w := range s.Workspaces {
		fmt.Printf("workspace %-10s id=%d mon=%s active=%v focused=%v fullscreen=%v urgent=%v\n", w.Name, w.ID, w.Monitor, w.Active, w.Focused, w.HasFullscreen, w.Urgent)
	}
	for _, t := range s.Toplevels {
		fmt.Printf("toplevel  %s ws=%d mon=%s active=%v urgent=%v %s: %q\n", t.Address, t.WorkspaceID, t.Monitor, t.Activated, t.Urgent, t.Class, t.Title)
	}
}

func (p printer) OnEvent(e hypr.Event) {
	if p.events {
		fmt.Printf("event     %s>>%s\n", e.Name, e.Data)
	}
}

func (p printer) OnConnection(up bool, err error) {
	fmt.Printf("== connected=%v err=%v\n", up, err)
}

func cmdHypr(args []string) error {
	if len(args) != 1 {
		usage()
	}
	paths, err := hypr.PathsFromEnv()
	if err != nil {
		return err
	}
	client := hypr.NewClient(paths)
	switch args[0] {
	case "dump":
		q, err := client.Query(context.Background())
		if err != nil {
			return err
		}
		s, err := hypr.BuildSnapshot(q, nil)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	case "watch":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		hypr.NewStore(client, printer{events: true}).Run(ctx)
		return nil
	}
	usage()
	return nil
}

// cmdConfig checks the settings file the way inua-shell reads it.
func cmdConfig(args []string) error {
	if len(args) == 0 {
		usage()
	}
	path := config.DefaultPath()
	switch {
	case args[0] == "path" && len(args) == 1:
		fmt.Println(path)
		return nil
	case args[0] == "check" && len(args) <= 2:
		if len(args) == 2 {
			path = args[1]
		}
	case args[0] == "hyprland" && len(args) <= 2:
		hypr := filepath.Join(filepath.Dir(hyprconf.Path(path)), "hyprland.conf")
		if len(args) == 2 {
			hypr = args[1]
		}
		return checkHyprland(hypr)
	case args[0] == "apply" && len(args) == 1:
		return applyHyprland(path)
	default:
		usage()
	}
	return checkConfig(path)
}

// checkHyprland reports whether a Hyprland config (or a file it sources)
// starts inua-shell, and whether it sources inua.conf. Both the Inua and
// the Hyprland session read this config, and inua init never changes an
// existing one, so without the first line neither session shows inua.
func checkHyprland(path string) error {
	where, err := bootstrap.FindShellStart(path)
	switch {
	case err == nil:
		fmt.Printf("%s: starts inua-shell (%s)\n", path, where)
	case errors.Is(err, bootstrap.ErrNoShell):
		fmt.Printf("%s does not start inua-shell, so the Inua session would show no bar,\n"+
			"launcher or wallpaper. inua init leaves an existing Hyprland config alone;\n"+
			"add this line to it:\n\n    %s\n\n"+
			"and, if you want them, inua's key bindings and layer rules from the sample\n"+
			"(inua init -force replaces the file with the sample, keeping yours as .bak).\n",
			path, bootstrap.ShellLine)
		return fmt.Errorf("%s does not start inua-shell", path)
	case err != nil:
		return err
	}
	checkSource(path, filepath.Join(filepath.Dir(path), hyprconf.Name))
	return nil
}

// checkSource reports whether a Hyprland config sources inua.conf, and
// after the settings inua writes there. Without it inua's keyboard and
// touchpad settings do nothing, but the shell works, so it only warns.
func checkSource(path, target string) {
	where, overrides, err := bootstrap.FindSource(path, target)
	switch {
	case errors.Is(err, bootstrap.ErrNoSource):
		fmt.Printf("%s does not source %s, so the keyboard and touchpad\n"+
			"settings in inua's settings window do nothing. Add this as its last line:\n\n    source = ./%s\n",
			path, hyprconf.Name, hyprconf.Name)
	case err != nil:
		fmt.Printf("%s: %v\n", path, err)
	default:
		fmt.Printf("%s: sources %s (%s)\n", path, hyprconf.Name, where)
		for _, o := range overrides {
			fmt.Printf("%s: set after inua.conf, so it overrides inua's setting; move the source line below it\n", o)
		}
	}
}

// applyHyprland writes hypr/inua.conf from the settings file and has
// Hyprland reload it, as the settings window does when it saves.
func applyHyprland(path string) error {
	c, warnings, err := config.Load(path)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Printf("%s: %s\n", path, w)
	}
	var client *hypr.Client
	if paths, err := hypr.PathsFromEnv(); err == nil {
		client = hypr.NewClient(paths)
	}
	target := hyprconf.Path(path)
	changed, err := hyprconf.Apply(context.Background(), c, target, client)
	switch {
	case err != nil:
		return err
	case !changed:
		fmt.Printf("%s: up to date\n", target)
	case client == nil:
		fmt.Printf("%s: written; Hyprland is not running and reads it when it starts\n", target)
	default:
		fmt.Printf("%s: written; Hyprland reloaded\n", target)
	}
	return nil
}

// checkConfig reports problems in a settings file the way inua-shell
// reads it.
func checkConfig(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("%s: not found; inua-shell uses the defaults\n", path)
		return nil
	}
	_, warnings, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%w (inua-shell would use the defaults)", err)
	}
	for _, w := range warnings {
		fmt.Printf("%s: %s\n", path, w)
	}
	if len(warnings) > 0 {
		return fmt.Errorf("%d problem(s); inua-shell uses the defaults for those settings", len(warnings))
	}
	fmt.Printf("%s: ok\n", path)
	return nil
}

// cmdInit writes the sample configuration (config/ in the repository,
// embedded here) under the config directory, then checks it.
func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	force := fs.Bool("force", false, "replace files that differ from the samples, keeping the old ones as .bak")
	dir := fs.String("dir", filepath.Dir(filepath.Dir(config.DefaultPath())), "config directory to write into")
	fs.Parse(args)
	if fs.NArg() != 0 {
		usage()
	}
	results, err := bootstrap.Install(samples.FS, samples.Files, *dir, *force)
	for _, r := range results {
		fmt.Println(r)
	}
	if err != nil {
		return err
	}
	if *dir == filepath.Dir(filepath.Dir(config.DefaultPath())) { // the user's own setup
		if err := initPinentry(*force); err != nil {
			fmt.Println("gpg:", err)
		}
	}

	fmt.Println()
	failed := false
	if err := checkConfig(filepath.Join(*dir, "inua", "config.toml")); err != nil {
		fmt.Println("inua:", err)
		failed = true
	}
	hyprConf := filepath.Join(*dir, "hypr", "hyprland.conf")
	if _, err := exec.LookPath("Hyprland"); err != nil {
		fmt.Println("Hyprland is not installed (make deps); hyprland.conf not checked")
	} else {
		out, _ := exec.Command("Hyprland", "--verify-config", "-c", hyprConf).CombinedOutput()
		if strings.Contains(string(out), "config ok") {
			fmt.Printf("%s: ok\n", hyprConf)
		} else {
			fmt.Printf("%s: Hyprland reports problems:\n%s\n", hyprConf, strings.TrimSpace(lastLines(string(out), 6)))
			failed = true
		}
	}
	if err := checkHyprland(hyprConf); err != nil {
		failed = true
	}
	if _, err := exec.LookPath("inua-shell"); err != nil {
		fmt.Println("inua-shell is not on PATH; hyprland.conf starts it by name (make install)")
	}
	if failed {
		return fmt.Errorf("the configuration has problems (see above)")
	}
	fmt.Println("\nLog out and choose the Inua session; Super+Space opens the launcher.")
	return nil
}

// initPinentry has gpg-agent ask for passphrases in inua's dialog, and
// reloads it if that changed.
func initPinentry(force bool) error {
	prog, err := exec.LookPath("inua-pinentry")
	if err != nil {
		fmt.Println("inua-pinentry is not on PATH (make install); gpg-agent left as it is")
		return nil
	}
	if prog, err = filepath.Abs(prog); err != nil {
		return err
	}
	changed, msg, err := bootstrap.SetPinentry(bootstrap.GnuPGHome(), prog, force)
	if err != nil {
		return err
	}
	fmt.Println(msg)
	if changed {
		if _, err := exec.LookPath("gpgconf"); err == nil {
			if out, err := exec.Command("gpgconf", "--reload", "gpg-agent").CombinedOutput(); err != nil {
				return fmt.Errorf("reloading gpg-agent: %v %s", err, strings.TrimSpace(string(out)))
			}
		}
	}
	return nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}
