// Command inua-remote is Remote Desktop (docs/remote-desktop.md): this
// desktop shared through Sunshine, and other machines' desktops streamed
// here, with the keyboard and mouse sent to them (GameStream, the protocol
// Moonlight speaks).
//
//	inua-remote                      open the window
//	inua-remote --share on           share this desktop, now and at login (off to stop)
//	inua-remote --virtual-screen on  a screen to share while no monitor is connected (off, or a size)
//	inua-remote --pair PIN           pair the device waiting, with the PIN it shows
//	inua-remote --status             whether it is shared, what sharing needs, the devices
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/apps/remotedesk"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/remoteui"
)

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	share := flag.String("share", "", "on: share this desktop, now and at login; off: stop")
	pair := flag.String("pair", "", "pair the device waiting to pair, with the four-digit `PIN` its screen shows")
	device := flag.String("device", "", "with --pair: the `device` to pair, by its name or address, when several wait")
	name := flag.String("name", "", "with --pair: the `name` to know the device by here (its own, if not given)")
	wait := flag.Duration("wait", time.Minute, "with --pair: how long to wait for the device to ask")
	virtual := flag.String("virtual-screen", "", "on: a screen of "+remotedesk.DefaultVirtualMode+" to share while no monitor is connected, or a `size` like 2560x1440@60; off: none")
	status := flag.Bool("status", false, "whether this desktop is shared, what sharing needs, and the devices waiting and paired")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-remote [-ui-dir DIR]")
		fmt.Fprintln(os.Stderr, "       inua-remote --share on|off")
		fmt.Fprintln(os.Stderr, "       inua-remote --virtual-screen on|off|WIDTHxHEIGHT@HZ")
		fmt.Fprintln(os.Stderr, "       inua-remote --pair PIN [--device NAME|ADDRESS] [--name NAME] [--wait 1m]")
		fmt.Fprintln(os.Stderr, "       inua-remote --status")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	s := remotedesk.NewSharing()
	switch {
	case *share != "":
		if *share != "on" && *share != "off" {
			flag.Usage()
			os.Exit(2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.SetOn(ctx, *share == "on"); err != nil {
			fail(err)
		}
	case *virtual != "":
		setVirtual(s, *virtual)
	case *pair != "":
		pairDevice(s, *pair, *device, *name, *wait)
	case *status:
		printStatus(s)
	default:
		remoteui.Register()
		os.Exit(appui.Run("remote", nil, *uiDir))
	}
}

// setVirtual turns the virtual screen on or off, and makes it so now
// when the session is running.
func setVirtual(s *remotedesk.Sharing, mode string) {
	switch mode {
	case "on":
		mode = remotedesk.DefaultVirtualMode
	case "off":
		mode = ""
	}
	if err := s.SetVirtual(mode); err != nil {
		fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := s.SyncVirtual(ctx)
	switch {
	case errors.Is(err, remotedesk.ErrNoSession):
		fmt.Println("No desktop session is running: the setting takes effect when one starts.")
	case err != nil:
		fail(err)
	}
}

// pairDevice gives the device waiting to pair the PIN its screen shows,
// waiting for it to ask first if it has not yet.
func pairDevice(s *remotedesk.Sharing, pin, which, name string, wait time.Duration) {
	pin = strings.TrimSpace(pin)
	if len(pin) != 4 || strings.Trim(pin, "0123456789") != "" {
		fail(errors.New("a PIN is four digits"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	running := s.Status(ctx).Running
	cancel()
	if !running {
		fail(errors.New("this desktop is not shared: inua-remote --share on"))
	}
	ctx, cancel = context.WithTimeout(context.Background(), wait)
	defer cancel()
	waiting, _ := s.Pending(ctx)
	if _, err := remotedesk.Choose(waiting, which); errors.Is(err, remotedesk.ErrNoDevice) {
		fmt.Fprintln(os.Stderr, "Waiting for the device to ask to pair: pick this computer on it…")
	}
	p, err := s.Await(ctx, which)
	var many *remotedesk.ManyError
	switch {
	case errors.As(err, &many):
		fmt.Fprintln(os.Stderr, "inua-remote: several devices wait to pair; choose one with --device:")
		for _, w := range many.Waiting {
			fmt.Fprintf(os.Stderr, "  %s  %s\n", w.Name, w.Address)
		}
		os.Exit(1)
	case errors.Is(err, remotedesk.ErrNoDevice):
		fail(fmt.Errorf("no device asked to pair within %s", wait))
	case err != nil:
		fail(err)
	}
	if name == "" {
		name = p.Name
	}
	if name == "" {
		name = p.Address
	}
	if err := s.Pair(ctx, p, pin, name); errors.Is(err, remotedesk.ErrPIN) {
		fail(fmt.Errorf("that is not the PIN %s shows", name))
	} else if err != nil {
		fail(err)
	}
	fmt.Printf("Paired %s (%s).\n", name, p.Address)
}

func printStatus(s *remotedesk.Sharing) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st := s.Status(ctx)
	version := st.Version
	if version == "" {
		version = "not installed"
	}
	fmt.Printf("sunshine: %s\nshared: %v (on at login: %v)\n", version, st.Running, s.On())
	if v := s.Virtual(); v != "" {
		fmt.Printf("virtual screen: %s, while no monitor is connected\n", v)
	} else {
		fmt.Println("virtual screen: off")
	}
	switch st.Need {
	case remotedesk.NeedSunshine:
		fmt.Println("needs: Sunshine (github.com/LizardByte/Sunshine/releases)")
	case remotedesk.NeedInput:
		fmt.Println("needs: /dev/uinput writable by you (udev rule: " + remotedesk.UinputRule + ")")
	case remotedesk.NeedDisplay:
		fmt.Println("needs: a graphical session; none is running (log in on this computer)")
	}
	if !st.Running {
		return
	}
	if waiting, err := s.Pending(ctx); err == nil {
		for _, p := range waiting {
			fmt.Printf("waiting to pair: %s (%s)\n", p.Name, p.Address)
		}
	}
	if clients, err := s.Clients(ctx); err == nil {
		for _, c := range clients {
			fmt.Printf("paired: %s\n", c.Name)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "inua-remote:", err)
	os.Exit(1)
}
