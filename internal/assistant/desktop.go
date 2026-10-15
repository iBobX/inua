package assistant

import (
	"cmp"
	"context"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/lyricat/inua/internal/apps/tailscale"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/ipc"
)

// LocalDesktop connects the tools to this session: the shell's control
// socket ($INUA_INSTANCE, else default), Hyprland, the installed apps
// (terminal runs those that want one), the clipboard, Tailscale.
func LocalDesktop(terminal []string) Desktop {
	sock := ipc.SocketPath(cmp.Or(os.Getenv("INUA_INSTANCE"), "default"))
	d := Desktop{
		Call: func(ctx context.Context, target, fn string, args ...string) (string, error) {
			resp, err := ipc.Call(ctx, sock, ipc.Request{Op: "call", Target: target, Function: fn, Args: args})
			return resp.Result, err
		},
		Launch:    func(e *desktop.Entry) error { return desktop.LaunchWith(e, terminal) },
		Tailscale: tailscale.NewClient(""),
		HTTP:      &http.Client{Timeout: 15 * time.Second},
		ClipRead: func(ctx context.Context) (string, error) {
			out, err := exec.CommandContext(ctx, "wl-paste", "--no-newline", "--type", "text").Output()
			return string(out), err
		},
		ClipWrite: func(ctx context.Context, text string) error {
			cmd := exec.CommandContext(ctx, "wl-copy")
			cmd.Stdin = strings.NewReader(text)
			return cmd.Run()
		},
	}
	var once sync.Once
	var apps []*desktop.Entry
	d.Apps = func() []*desktop.Entry {
		once.Do(func() { apps = desktop.Scan(desktop.DataDirs(), os.Getenv("LANG")) })
		return apps
	}
	if paths, err := hypr.PathsFromEnv(); err == nil {
		client := hypr.NewClient(paths)
		d.HyprRequest = client.Request
		d.HyprDispatch = client.Dispatch
	}
	return d
}
