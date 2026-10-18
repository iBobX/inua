// Command inua-remote is Remote Desktop (docs/remote-desktop.md): this
// desktop shared through Sunshine, and other machines' desktops streamed
// here, with the keyboard and mouse sent to them (GameStream, the protocol
// Moonlight speaks).
//
//	inua-remote               open the window
//	inua-remote --share on    share this desktop (off to stop)
//	inua-remote --status      whether it is shared, and what it needs
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/lyricat/inua/internal/apps/remotedesk"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/remoteui"
)

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	share := flag.String("share", "", "on: share this desktop, now and at login; off: stop")
	status := flag.Bool("status", false, "whether this desktop is shared, and what sharing needs")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-remote [-ui-dir DIR] [--share on|off | --status]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := remotedesk.NewSharing()
	switch {
	case *share != "":
		if *share != "on" && *share != "off" {
			flag.Usage()
			os.Exit(2)
		}
		if err := s.SetOn(ctx, *share == "on"); err != nil {
			fmt.Fprintln(os.Stderr, "inua-remote:", err)
			os.Exit(1)
		}
		return
	case *status:
		st := s.Status(ctx)
		fmt.Printf("sunshine: %s\nshared: %v (on at login: %v)\n", orNone(st.Version), st.Running, s.On())
		switch st.Need {
		case remotedesk.NeedSunshine:
			fmt.Println("needs: Sunshine (github.com/LizardByte/Sunshine/releases)")
		case remotedesk.NeedInput:
			fmt.Println("needs: /dev/uinput writable by you (udev rule: " + remotedesk.UinputRule + ")")
		case remotedesk.NeedDisplay:
			fmt.Println("needs: a graphical session")
		}
		return
	}
	remoteui.Register()
	os.Exit(appui.Run("remote", nil, *uiDir))
}

func orNone(s string) string {
	if s == "" {
		return "not installed"
	}
	return s
}
