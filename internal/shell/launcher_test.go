package shell

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/hypr"
)

func titles(rs []Result) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Title)
	}
	return out
}

func TestMatchTiers(t *testing.T) {
	for _, c := range []struct {
		text, q string
		want    int
	}{
		{"Firefox", "firefox", exact},
		{"Firefox", "fire", prefix},
		{"Visual Studio Code", "code", wordStart},
		{"gnome-terminal", "term", wordStart},
		{"Thunderbird", "bird", contains},
		{"Firefox", "ffx", subsequence},
		{"Visual Studio Code", "vs code", subsequence},
		{"文本编辑器", "编辑", contains},
		{"Firefox", "chrome", noMatch},
		{"", "a", noMatch},
		// A later occurrence at a word start beats an earlier inner one.
		{"decode code", "code", wordStart},
	} {
		if got := match(c.text, c.q); got != c.want {
			t.Errorf("match(%q, %q) = %d, want %d", c.text, c.q, got, c.want)
		}
	}
}

func testApps() []*desktop.Entry {
	return []*desktop.Entry{
		{ID: "firefox", Name: "Firefox", GenericName: "Web Browser"},
		{ID: "org.gnome.Nautilus", Name: "Files", Keywords: []string{"folder", "manager"}},
		{ID: "code", Name: "Visual Studio Code", GenericName: "Text Editor"},
		{ID: "foot", Name: "Foot", GenericName: "Terminal"},
		{ID: "hidden", Name: "Fire Helper", NoDisplay: true},
		{ID: "org.gnome.TextEditor", Name: "文本编辑器", GenericName: "Text Editor"},
	}
}

func TestAppSearch(t *testing.T) {
	var l Launcher
	l.SetApps(testApps())
	l.Show(Apps)
	if want := []string{"Files", "Firefox", "Foot", "Visual Studio Code", "文本编辑器"}; !slices.Equal(titles(l.Results), want) {
		t.Fatalf("no query: %q, want %q", titles(l.Results), want)
	}
	for _, c := range []struct {
		q    string
		want []string
	}{
		// Name matches outrank generic names and keywords; NoDisplay is hidden.
		{"fi", []string{"Files", "Firefox", "Toggle Wi-Fi"}}, // and a command, on a word of its title
		{"text", []string{"Visual Studio Code", "文本编辑器"}},
		{"folder", []string{"Files"}},
		{"code", []string{"Visual Studio Code"}},
		{"编辑", []string{"文本编辑器"}},
		{"  FIRE ", []string{"Firefox"}},
		// Subsequences count on names only ("Web Browser" has no f-f-x).
		{"ffx", []string{"Firefox"}},
		{"zzz", nil},
	} {
		l.SetQuery(c.q)
		if !slices.Equal(titles(l.Results), c.want) {
			t.Errorf("query %q: %q, want %q", c.q, titles(l.Results), c.want)
		}
		if l.Selected != 0 {
			t.Errorf("query %q: selected %d", c.q, l.Selected)
		}
	}
	if got := l.Message(); got != "No app matches" {
		t.Errorf("message = %q", got)
	}
}

func TestAppsNotLoaded(t *testing.T) {
	var l Launcher
	l.Show(Apps)
	if got := l.Message(); got != "Reading apps…" {
		t.Errorf("message = %q", got)
	}
	l.SetApps(testApps()) // arriving while open fills the list
	if len(l.Results) != 5 || l.Message() != "" {
		t.Errorf("after load: %d results, message %q", len(l.Results), l.Message())
	}
}

func win(addr, class, title string, ws, history int) hypr.Toplevel {
	return hypr.Toplevel{Address: addr, Class: class, Title: title, WorkspaceID: ws,
		Raw: map[string]any{"focusHistoryID": float64(history)}}
}

func testSnap() hypr.Snapshot {
	return hypr.Snapshot{
		Workspaces: []hypr.Workspace{{ID: 1, Name: "1"}, {ID: 2, Name: "web"}},
		Toplevels: []hypr.Toplevel{
			win("0x1", "foot", "~/src", 1, 2),
			win("0x2", "firefox", "Mozilla Firefox", 2, 0),
			win("0x3", "code", "main.go - Code", 1, 1),
		},
		ActiveToplevel: "0x2",
	}
}

func TestWindowSearch(t *testing.T) {
	var l Launcher
	l.SetConnected(true)
	l.SetSnapshot(testSnap())
	l.Show(Windows)
	// Focus order, with the previously focused window selected (Alt-Tab).
	if want := []string{"Mozilla Firefox", "main.go - Code", "~/src"}; !slices.Equal(titles(l.Results), want) {
		t.Fatalf("no query: %q, want %q", titles(l.Results), want)
	}
	if l.Selected != 1 {
		t.Errorf("selected %d, want 1", l.Selected)
	}
	if got := l.Results[0].Detail; got != "firefox · workspace web" {
		t.Errorf("detail = %q", got)
	}
	l.SetQuery("foot")
	if want := []string{"~/src"}; !slices.Equal(titles(l.Results), want) || l.Selected != 0 {
		t.Errorf("query foot: %q selected %d", titles(l.Results), l.Selected)
	}
	l.SetQuery("x")
	if !slices.Equal(titles(l.Results), []string{"Mozilla Firefox"}) {
		t.Errorf("query x: %q", titles(l.Results))
	}
}

func TestWindowListFollowsSnapshot(t *testing.T) {
	var l Launcher
	l.SetConnected(true)
	snap := testSnap()
	l.SetSnapshot(snap)
	l.Show(Windows)
	l.Move(1) // ~/src
	if l.Results[l.Selected].Address != "0x1" {
		t.Fatalf("selected %+v", l.Results[l.Selected])
	}
	// A new window appears first; the selection stays on ~/src.
	snap.Toplevels = append(snap.Toplevels, win("0x4", "mpv", "video", 2, 0))
	l.SetSnapshot(snap)
	if l.Results[l.Selected].Address != "0x1" {
		t.Errorf("selection moved to %+v", l.Results[l.Selected])
	}
	// The selected window closes: the selection falls back to the top.
	snap.Toplevels = slices.DeleteFunc(snap.Toplevels, func(t hypr.Toplevel) bool { return t.Address == "0x1" })
	l.SetSnapshot(snap)
	if len(l.Results) != 3 || l.Selected >= len(l.Results) {
		t.Errorf("after close: %q selected %d", titles(l.Results), l.Selected)
	}
}

func TestMove(t *testing.T) {
	var l Launcher
	l.SetApps(testApps())
	l.Show(Apps)
	l.Move(-1)
	if l.Selected != 0 {
		t.Errorf("up from top: %d", l.Selected)
	}
	l.Move(100)
	if l.Selected != len(l.Results)-1 {
		t.Errorf("down past end: %d", l.Selected)
	}
	l.SetQuery("zzz")
	l.Move(1)
	if l.Selected != 0 {
		t.Errorf("empty list: %d", l.Selected)
	}
}

func TestToggleModeKeepsQuery(t *testing.T) {
	var l Launcher
	l.SetConnected(true)
	l.SetApps(testApps())
	l.SetSnapshot(testSnap())
	l.Show(Apps)
	l.SetQuery("fire")
	l.ToggleMode()
	if l.Mode != Windows || l.Query != "fire" || !slices.Equal(titles(l.Results), []string{"Mozilla Firefox"}) {
		t.Errorf("mode %v query %q results %q", l.Mode, l.Query, titles(l.Results))
	}
	l.ToggleMode()
	if l.Mode != Apps || !slices.Equal(titles(l.Results), []string{"Firefox"}) {
		t.Errorf("back: mode %v results %q", l.Mode, titles(l.Results))
	}
}

func TestCancelReturnsFocus(t *testing.T) {
	var l Launcher
	snap := testSnap()
	l.SetSnapshot(snap)
	l.Show(Apps)
	// The window focused at open time gets focus back...
	snap.ActiveToplevel = "0x1" // even if something else got focus meanwhile
	l.SetSnapshot(snap)
	if a := l.Cancel(); a != (Action{Kind: Focus, Address: "0x2"}) || l.Open {
		t.Errorf("cancel: %+v open=%v", a, l.Open)
	}
	if a := l.Cancel(); a.Kind != None {
		t.Errorf("cancel when closed: %+v", a)
	}

	// ...unless it closed meanwhile.
	l.SetSnapshot(testSnap())
	l.Show(Apps)
	snap = testSnap()
	snap.Toplevels = snap.Toplevels[:1]
	l.SetSnapshot(snap)
	if a := l.Cancel(); a.Kind != None {
		t.Errorf("cancel after the window closed: %+v", a)
	}

	// Nothing was focused.
	l.SetSnapshot(hypr.Snapshot{})
	l.Show(Apps)
	if a := l.Cancel(); a.Kind != None {
		t.Errorf("cancel without a previous window: %+v", a)
	}
}

func TestShowWhileOpenKeepsPrevious(t *testing.T) {
	var l Launcher
	snap := testSnap()
	l.SetSnapshot(snap)
	l.Show(Apps)
	snap.ActiveToplevel = "0x3"
	l.SetSnapshot(snap)
	l.Show(Windows) // switching modes is not a new open
	if a := l.Cancel(); a.Address != "0x2" {
		t.Errorf("cancel focuses %q, want 0x2", a.Address)
	}
}

func TestExecuteApp(t *testing.T) {
	var l Launcher
	l.SetApps(testApps())
	l.Show(Apps)
	l.SetQuery("foot")
	a := l.Execute()
	if a.Kind != Launch || a.Entry.ID != "foot" {
		t.Fatalf("execute: %+v", a)
	}
	if !l.Open {
		t.Error("closed before the launch was confirmed")
	}
	l.Done(errors.New("foot: not found"))
	if !l.Open || l.Message() != "foot: not found" || !l.Failed() {
		t.Errorf("after failure: open=%v message=%q failed=%v", l.Open, l.Message(), l.Failed())
	}
	l.SetQuery("foo") // typing clears the notice
	if l.Message() != "" || l.Failed() {
		t.Errorf("message after typing: %q failed=%v", l.Message(), l.Failed())
	}
	l.Execute()
	l.Done(nil)
	if l.Open {
		t.Error("still open after a successful launch")
	}
}

func TestExecuteWindow(t *testing.T) {
	var l Launcher
	l.SetConnected(true)
	snap := testSnap()
	l.SetSnapshot(snap)
	l.Show(Windows)
	if a := l.Execute(); a != (Action{Kind: Focus, Address: "0x3"}) {
		t.Fatalf("execute: %+v", a)
	}
	l.Done(nil)
	if l.Open {
		t.Error("still open")
	}
}

func TestExecuteClosedWindow(t *testing.T) {
	var l Launcher
	l.SetConnected(true)
	l.SetSnapshot(testSnap())
	l.Show(Windows)
	// The window closes, and the snapshot saying so has not arrived yet
	// when Enter is pressed: the launcher learns it from a direct check
	// against what it has, so simulate a stale list.
	stale := l.Results
	snap := testSnap()
	snap.Toplevels = slices.DeleteFunc(snap.Toplevels, func(t hypr.Toplevel) bool { return t.Address == "0x3" })
	l.snap = snap // state changed without a list refresh
	l.Results = stale
	if a := l.Execute(); a.Kind != None {
		t.Fatalf("execute on a closed window: %+v", a)
	}
	if !l.Open || l.Message() != "That window has closed" || len(l.Results) != 2 {
		t.Errorf("open=%v message=%q results=%q", l.Open, l.Message(), titles(l.Results))
	}
}

func TestExecuteNothing(t *testing.T) {
	var l Launcher
	l.SetApps(testApps())
	if a := l.Execute(); a.Kind != None {
		t.Errorf("closed: %+v", a)
	}
	l.Show(Apps)
	l.SetQuery("zzz")
	if a := l.Execute(); a.Kind != None || !l.Open {
		t.Errorf("no results: %+v open=%v", a, l.Open)
	}
}

func TestWindowMessages(t *testing.T) {
	var l Launcher
	l.Show(Windows)
	if got := l.Message(); got != "Not connected to Hyprland" {
		t.Errorf("disconnected: %q", got)
	}
	l.SetConnected(true)
	if got := l.Message(); got != "No windows open" {
		t.Errorf("no windows: %q", got)
	}
	l.SetSnapshot(testSnap())
	l.SetQuery("zzz")
	if got := l.Message(); got != "No window matches" || l.Failed() {
		t.Errorf("no match: %q failed=%v", got, l.Failed())
	}
	if Apps.Tag() != "[APPS]" || Windows.Tag() != "[WINDOWS]" {
		t.Errorf("tags %q %q", Apps.Tag(), Windows.Tag())
	}
}

func TestLauncherAnswers(t *testing.T) {
	var l Launcher
	l.SetApps([]*desktop.Entry{{ID: "firefox", Name: "Firefox"}})
	l.Show(Apps)
	l.SetQuery("10 km to mi")
	if len(l.Results) == 0 || l.Results[0].Copy != "6.21371" || l.Selected != 0 {
		t.Fatalf("results %+v", l.Results)
	}
	if a := l.Execute(); a.Kind != Copy || a.Text != "6.21371" {
		t.Fatalf("Execute = %+v", a)
	}
	l.SetQuery("fire")
	if len(l.Results) != 1 || l.Results[0].Copy != "" {
		t.Fatalf("an app search has no answer: %+v", l.Results)
	}
}

func TestCommands(t *testing.T) {
	var l Launcher
	l.SetApps(testApps())
	l.SetSnapshot(hypr.Snapshot{Toplevels: []hypr.Toplevel{{Address: "0x1", Class: "firefox", Title: "Inbox"}}, ActiveToplevel: "0x1"})
	l.Show(Apps)

	l.SetQuery("lock")
	if len(l.Results) == 0 || l.Results[0].Command != "lock" {
		t.Fatalf("lock: %q", titles(l.Results))
	}
	if a := l.Execute(); a.Kind != Run || a.Text != "lock" {
		t.Errorf("lock runs at once: %+v", a)
	}

	// Shutting down asks for Enter again; moving away disarms it.
	l.Show(Apps)
	l.SetQuery("shut down")
	if a := l.Execute(); a.Kind != None || !strings.Contains(l.Message(), "Enter again") || l.Failed() {
		t.Fatalf("first Enter: %+v %q", a, l.Message())
	}
	if a := l.Execute(); a.Kind != Run || a.Text != "poweroff" {
		t.Errorf("second Enter: %+v", a)
	}

	// Window commands act on the window the launcher opened over.
	l.Show(Apps)
	l.SetQuery("close window")
	if a := l.Execute(); a.Kind != Run || a.Text != "close-window" || a.Address != "0x1" {
		t.Errorf("close window: %+v", a)
	}
	l.Show(Apps)
	l.SetQuery("move to workspace 3")
	if a := l.Execute(); a.Kind != Run || a.Text != "move-window" || a.Arg != "3" {
		t.Errorf("move: %+v", a)
	}
	l.SetQuery("ws 2")
	if r := l.Results[0]; r.Command != "workspace" || r.Arg != "2" {
		t.Errorf("ws 2: %+v", r)
	}
}

func TestWeb(t *testing.T) {
	var l Launcher
	l.SetApps(testApps())
	l.SetWeb("https://www.google.com/search?q=%s", []Quicklink{{"gh", "GitHub", "https://github.com/search?q=%s"}, {"g", "Google", "https://www.google.com/search?q=%s"}})
	l.Show(Apps)

	l.SetQuery("gh inua shell")
	if r := l.Results[0]; r.URL != "https://github.com/search?q=inua+shell" || !strings.Contains(r.Title, "GitHub") {
		t.Errorf("quicklink: %+v", r)
	}
	if last := l.Results[len(l.Results)-1]; last.Title != "Search Google for “gh inua shell”" {
		t.Errorf("web search last: %+v", last)
	}

	// The keyword alone: Enter fills it in for the words.
	l.SetQuery("gh")
	if a := l.Execute(); a.Kind != Fill || a.Text != "gh " || l.Query != "gh " {
		t.Errorf("fill: %+v %q", a, l.Query)
	}

	l.SetQuery("example.com/docs")
	if r := l.Results[0]; r.URL != "https://example.com/docs" {
		t.Errorf("address: %+v", r)
	}
	l.SetQuery("firefox")
	if r := l.Results[0]; r.Title != "Firefox" {
		t.Errorf("an app still comes first: %+v", r)
	}
	if a, ok := func() (Action, bool) { l.Move(100); return l.Execute(), true }(); !ok || a.Kind != Open || !strings.HasPrefix(a.Text, "https://www.google.com/search?q=firefox") {
		t.Errorf("search: %+v", a)
	}
}

func TestEmojiMode(t *testing.T) {
	var l Launcher
	l.SetApps(testApps())
	l.SetWeb("https://www.google.com/search?q=%s", nil)
	l.Show(Apps)
	l.SetQuery(":rocket")
	if len(l.Results) == 0 || l.Results[0].Glyph != "🚀" {
		t.Fatalf(":rocket: %+v", l.Results)
	}
	if a := l.Execute(); a.Kind != Copy || a.Text != "🚀" {
		t.Errorf("copies: %+v", a)
	}
	l.Show(Apps)
	l.SetQuery(":zzzqqq")
	if len(l.Results) != 0 || l.Message() != "No emoji or symbol matches" {
		t.Errorf("none: %q %q", titles(l.Results), l.Message())
	}
}

func TestAssistant(t *testing.T) {
	var l Launcher
	l.SetApps(testApps())
	l.SetWeb("https://www.google.com/search?q=%s", nil)
	l.Show(Apps)
	l.SetQuery("firefox")
	if last := l.Results[len(l.Results)-1]; last.Ask != "" {
		t.Errorf("no assistant, no row: %+v", last)
	}

	l.SetAssistant(true)
	if last := l.Results[len(l.Results)-1]; last.Ask != "firefox" || last.Icon != AssistantIcon {
		t.Errorf("the last row asks: %+v", last)
	}
	if r := l.Results[0]; r.Title != "Firefox" {
		t.Errorf("an app still comes first: %+v", r)
	}

	// "?" asks at once: the only row.
	l.SetQuery("? why is the fan loud")
	if len(l.Results) != 1 {
		t.Fatalf("?: %q", titles(l.Results))
	}
	if a := l.Execute(); a.Kind != Ask || a.Text != "why is the fan loud" {
		t.Errorf("asks: %+v", a)
	}
	l.SetQuery("?")
	if len(l.Results) != 0 || l.Message() != "Ask the assistant anything" {
		t.Errorf("? alone: %q %q", titles(l.Results), l.Message())
	}
}
