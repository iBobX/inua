package assistant

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/apps/sysmon"
	"github.com/lyricat/inua/internal/desktop"
)

// fakeDesktop records what the tools do.
func fakeDesktop() (Desktop, *[]string) {
	var did []string
	clients := `[
	  {"address":"0xa","title":"Inbox — Mail","class":"thunderbird","mapped":true,"focusHistoryID":1,"workspace":{"name":"1"}},
	  {"address":"0xb","title":"inua — Firefox","class":"firefox","mapped":true,"focusHistoryID":0,"workspace":{"name":"2"}},
	  {"address":"0xc","title":"Docs — Firefox","class":"firefox","mapped":true,"focusHistoryID":2,"workspace":{"name":"2"}},
	  {"address":"0xd","title":"hidden","class":"x","mapped":false,"focusHistoryID":3,"workspace":{"name":"1"}}]`
	d := Desktop{
		Call: func(_ context.Context, target, fn string, args ...string) (string, error) {
			did = append(did, strings.Join(append([]string{"call", target, fn}, args...), " "))
			if target == "system" && fn == "status" {
				return `{"wifi":true}`, nil
			}
			return "", nil
		},
		HyprRequest:  func(context.Context, string) ([]byte, error) { return []byte(clients), nil },
		HyprDispatch: func(_ context.Context, a string) error { did = append(did, "dispatch "+a); return nil },
		Apps: func() []*desktop.Entry {
			return []*desktop.Entry{{ID: "org.gnome.Nautilus", Name: "Files"}, {ID: "firefox", Name: "Firefox"}, {ID: "x", Name: "Hidden", NoDisplay: true}}
		},
		Launch: func(e *desktop.Entry) error { did = append(did, "launch "+e.ID); return nil },
		Processes: func() []sysmon.Process {
			return []sysmon.Process{{PID: 100, Name: "mine", UID: os.Getuid()}, {PID: 200, Name: "root's", UID: os.Getuid() + 1}}
		},
	}
	return d, &did
}

func run(t *testing.T, d Desktop, name string, args map[string]any) (string, error) {
	t.Helper()
	for _, tool := range Tools(d) {
		if tool.Name == name {
			out, err := inuaTool{tool}.Execute(context.Background(), args)
			return out, err
		}
	}
	t.Fatalf("no tool %s", name)
	return "", nil
}

func TestWindowTools(t *testing.T) {
	d, did := fakeDesktop()
	out, err := run(t, d, "list_windows", nil)
	var ws []window
	json.Unmarshal([]byte(out), &ws)
	if err != nil || len(ws) != 3 || !ws[1].Focused {
		t.Fatalf("list: %v %+v", err, ws)
	}
	for args, want := range map[string]string{
		`{"window":"mail"}`:                 "dispatch focuswindow address:0xa",
		`{"window":"0xc"}`:                  "dispatch focuswindow address:0xc",
		`{}`:                                "dispatch focuswindow address:0xb", // the focused one
		`{"window":"Mail","workspace":"3"}`: "dispatch movetoworkspacesilent 3,address:0xa",
	} {
		*did = nil
		var a map[string]any
		json.Unmarshal([]byte(args), &a)
		tool := "focus_window"
		if _, ok := a["workspace"]; ok {
			tool = "move_window"
		}
		if _, err := run(t, d, tool, a); err != nil || len(*did) != 1 || (*did)[0] != want {
			t.Errorf("%s %s: %v %q, want %q", tool, args, err, *did, want)
		}
	}
	// Two Firefox windows: an error naming both, nothing done.
	*did = nil
	if _, err := run(t, d, "close_window", map[string]any{"window": "firefox"}); err == nil || !strings.Contains(err.Error(), "0xb") || len(*did) != 0 {
		t.Errorf("ambiguous: %v %q", err, *did)
	}
	if _, err := run(t, d, "move_window", map[string]any{"window": "mail", "workspace": "1,address:0xb"}); err == nil {
		t.Error("a workspace smuggling a second address")
	}
}

func TestSystemTools(t *testing.T) {
	d, did := fakeDesktop()
	if out, _ := run(t, d, "system_status", nil); out != `{"wifi":true}` {
		t.Errorf("status %q", out)
	}
	*did = nil
	run(t, d, "set_system", map[string]any{"setting": "volume", "value": "30%"})
	if len(*did) != 1 || (*did)[0] != "call system set volume 30" {
		t.Errorf("set: %q", *did)
	}
	if _, err := run(t, d, "set_system", map[string]any{"setting": "rm", "value": "-rf"}); err == nil {
		t.Error("an unknown setting went through")
	}
	if _, err := run(t, d, "power", map[string]any{"action": "explode"}); err == nil {
		t.Error("an unknown power action went through")
	}
	*did = nil
	run(t, d, "open_app", map[string]any{"app": "files"})
	if len(*did) != 1 || (*did)[0] != "launch org.gnome.Nautilus" {
		t.Errorf("open: %q", *did)
	}
	if _, err := run(t, d, "open_app", map[string]any{"app": "hidden"}); err == nil {
		t.Error("opened a hidden app")
	}
	if _, err := run(t, d, "end_process", map[string]any{"pid": float64(200)}); err == nil || !strings.Contains(err.Error(), "not the user's") {
		t.Errorf("another user's process: %v", err)
	}
	// Describe says what a call is about to do.
	for _, tool := range Tools(d) {
		if tool.Name == "set_system" {
			if got := tool.Describe(map[string]any{"setting": "focus", "value": "on"}); got != "Turning Focus on" {
				t.Errorf("describe %q", got)
			}
		}
	}
}
