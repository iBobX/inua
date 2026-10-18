// Command inua-connect is Mobile Link (docs/mobile-link.md): phones and computers
// running KDE Connect, GSConnect or Valent, paired with this one.
//
//	inua-connect                     open the window
//	inua-connect --service           the service, which the window starts in a user unit
//	inua-connect --on | --off        turn the service on (now and at login) or off
//	inua-connect --devices           the devices: paired, found, waiting to pair
//	inua-connect --pair DEVICE       pair with a device, or accept its request
//	inua-connect --unpair DEVICE     refuse a request, or forget a paired device
//	inua-connect --ping DEVICE       ping it
//	inua-connect --ring DEVICE       ring it, to find it
//	inua-connect --send DEVICE FILE… send it files
//	inua-connect --text DEVICE TEXT  send it text, or a link it opens
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lyricat/inua/internal/apps/connect"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/clipboard"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/connectui"
	"github.com/lyricat/inua/internal/i18n"
)

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	service := flag.Bool("service", false, "run the service (the window starts it in the user unit "+connect.Unit+")")
	on := flag.Bool("on", false, "turn the service on, now and at login")
	off := flag.Bool("off", false, "turn the service off")
	devices := flag.Bool("devices", false, "list the devices: paired, found, waiting to pair")
	pair := flag.String("pair", "", "pair with the `device` (its name or ID), or accept its request")
	unpair := flag.String("unpair", "", "refuse the `device`'s request, or forget it")
	ping := flag.String("ping", "", "ping the `device`")
	ring := flag.String("ring", "", "ring the `device`, to find it")
	send := flag.String("send", "", "send the `device` the files named after the flags")
	text := flag.String("text", "", "send the `device` the text after the flags, or a link it opens")
	clip := flag.Bool("clipboard", false, "give the service the text copied, on stdin (wl-paste --watch runs it)")
	reply := flag.String("reply", "", "answer the notification with this reply `ID` (the service runs it)")
	replyDevice := flag.String("device", "", "with --reply: the `device`")
	replyTitle := flag.String("title", "", "with --reply: what is answered")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-connect [-ui-dir DIR]")
		fmt.Fprintln(os.Stderr, "       inua-connect --on | --off | --devices")
		fmt.Fprintln(os.Stderr, "       inua-connect --pair|--unpair|--ping|--ring DEVICE")
		fmt.Fprintln(os.Stderr, "       inua-connect --send DEVICE FILE…")
		fmt.Fprintln(os.Stderr, "       inua-connect --text DEVICE TEXT")
		flag.PrintDefaults()
	}
	flag.Parse()
	cfg, _, _ := config.Load(config.DefaultPath()) // a broken file leaves the defaults
	i18n.Init(cfg.Language)
	switch {
	case *service:
		runService()
	case *clip:
		copied(cfg)
	case *on, *off:
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := connect.SetOn(ctx, *on); err != nil {
			fail(err)
		}
	case *devices:
		listDevices()
	case *pair != "":
		call("Pair", find(*pair))
	case *unpair != "":
		call("Unpair", find(*unpair))
	case *ping != "":
		call("Ping", find(*ping))
	case *ring != "":
		call("Ring", find(*ring))
	case *send != "":
		if flag.NArg() == 0 {
			flag.Usage()
			os.Exit(2)
		}
		var paths []string
		for _, p := range flag.Args() {
			abs, err := filepath.Abs(p)
			if err != nil {
				fail(err)
			}
			paths = append(paths, abs)
		}
		call("Share", find(*send), paths)
	case *text != "":
		if flag.NArg() == 0 {
			flag.Usage()
			os.Exit(2)
		}
		call("ShareText", find(*text), strings.Join(flag.Args(), " "))
	case *reply != "":
		connectui.RegisterReply(*replyDevice, *reply, *replyTitle)
		os.Exit(appui.Run("connect-reply", nil, *uiDir))
	default:
		if flag.NArg() != 0 {
			flag.Usage()
			os.Exit(2)
		}
		connectui.Register()
		os.Exit(appui.Run("connect", nil, *uiDir))
	}
}

// runService runs the service until it is stopped.
func runService() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	name, _ := os.Hostname()
	s, err := connect.New(name)
	if err != nil {
		fail(err)
	}
	if err := s.Run(ctx); err != nil {
		if errors.Is(err, connect.ErrRunning) {
			log.Print(err)
			return
		}
		fail(err)
	}
}

// copied gives the service the text copied, unless it is sensitive (a
// password manager's), as the clipboard's history does not keep it.
func copied(cfg config.Config) {
	from := clipboard.Selection(cfg.Clipboard.SensitiveSources)
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20+1))
	if err != nil || from.Sensitive || len(data) == 0 || len(data) > 1<<20 {
		return
	}
	switch os.Getenv("CLIPBOARD_STATE") {
	case "sensitive", "nil", "clear":
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connect.Call(ctx, "Clipboard", string(data))
}

func serviceDevices() []connect.DeviceView {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var js string
	if err := connect.Call(ctx, "Devices").Store(&js); err != nil {
		fail(errors.New("the service is not running: inua-connect --on"))
	}
	var list []connect.DeviceView
	json.Unmarshal([]byte(js), &list)
	return list
}

// find is the device named which, by its name (in any case) or ID.
func find(which string) string {
	var found []connect.DeviceView
	for _, d := range serviceDevices() {
		if d.ID == which || strings.EqualFold(d.Name, which) {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 0:
		fail(fmt.Errorf("no device %q: inua-connect --devices lists them", which))
	case 1:
		return found[0].ID
	}
	fail(fmt.Errorf("several devices are called %q: name one by its ID", which))
	return ""
}

func call(method string, args ...any) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := connect.Call(ctx, method, args...).Err; err != nil {
		fail(err)
	}
}

func listDevices() {
	for _, d := range serviceDevices() {
		state := connect.StateText(d)
		fmt.Printf("%s  %s  %s\n", d.ID, d.Name, state)
		if d.Key != "" {
			fmt.Printf("    key: %s\n", d.Key)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "inua-connect:", err)
	os.Exit(1)
}
