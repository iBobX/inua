// Package shell holds the shell's state and rules, apart from any toolkit:
// what the launcher shows and does, how the bar summarizes compositor and
// system state. The GTK views in internal/ui only turn this state into
// widgets and feed events back. Everything here runs on the main thread.
package shell

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/convert"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/hypr"
)

// Mode is what the launcher searches.
type Mode int

const (
	Apps Mode = iota
	Windows
)

// ParseMode accepts the names used on the control socket.
func ParseMode(s string) (Mode, bool) {
	switch s {
	case "apps":
		return Apps, true
	case "windows":
		return Windows, true
	}
	return 0, false
}

func (m Mode) String() string {
	if m == Windows {
		return "windows"
	}
	return "apps"
}

// Tag is the mode as a bracketed label in the launcher's head: [APPS].
func (m Mode) Tag() string { return "[" + strings.ToUpper(m.String()) + "]" }

// Label is the mode's name beside its tag.
func (m Mode) Label() string {
	if m == Windows {
		return "Switch windows"
	}
	return "Launch apps"
}

// Result is one launcher row: an app to launch, a window to focus, an
// answer or emoji to copy, a command to run, or a web address to open.
type Result struct {
	Title, Detail string
	Icon          string         // icon name: the app's icon, or the window class
	Glyph         string         // shown instead of the icon: an emoji or symbol
	Entry         *desktop.Entry // set for apps
	Address       string         // set for windows
	Copy          string         // set for answers and emoji: what Enter copies
	Command, Arg  string         // set for commands: Command's ID, and its argument
	URL           string         // set for quicklinks and web searches
	Fill          string         // Enter puts this in the search field instead

	score   int
	history int // windows: Hyprland's focus history, 0 = most recent
}

// ActionKind says what the view has to do after a launcher step.
type ActionKind int

const (
	None   ActionKind = iota
	Launch            // start Action.Entry
	Focus             // focuswindow Action.Address
	Copy              // put Action.Text on the clipboard
	Open              // open the web address Action.Text
	Run               // run command Action.Text with Action.Arg, on window Action.Address
	Fill              // put Action.Text in the search field (the query is set already)
)

// Action is a side effect for the view to carry out; the launcher itself
// never talks to Hyprland or starts processes.
type Action struct {
	Kind    ActionKind
	Entry   *desktop.Entry
	Address string
	Text    string
	Arg     string
}

// Launcher is the search interface: one window, two modes. It is open from
// Show until Cancel or a successful Execute.
type Launcher struct {
	Open     bool
	Mode     Mode
	Query    string
	Results  []Result
	Selected int

	apps      []*desktop.Entry
	appsReady bool
	snap      hypr.Snapshot
	connected bool
	rates     *convert.Rates // exchange rates for answers; nil until read
	previous  string         // window focused before Show
	notice    string         // feedback about the last action, cleared by the next input
	webSearch string         // the web search's address, %s for the words
	links     []Quicklink
	armed     string // the command that asked for Enter again
}

// SetWeb sets the web search and the quicklinks.
func (l *Launcher) SetWeb(search string, links []Quicklink) {
	l.webSearch, l.links = search, links
	if l.Open && l.Mode == Apps {
		l.search(false)
	}
}

// SetApps replaces the installed applications.
func (l *Launcher) SetApps(apps []*desktop.Entry) {
	l.apps, l.appsReady = apps, true
	if l.Open && l.Mode == Apps {
		l.search(false)
	}
}

// SetSnapshot replaces the compositor state. An open window list is
// refreshed, keeping the selected window selected while it exists.
func (l *Launcher) SetSnapshot(snap hypr.Snapshot) {
	l.snap = snap
	if l.Open && l.Mode == Windows {
		l.search(true)
	}
}

// SetRates replaces the exchange rates answers use.
func (l *Launcher) SetRates(r *convert.Rates) {
	l.rates = r
	if l.Open && l.Mode == Apps {
		l.search(false)
	}
}

// SetConnected records whether Hyprland is reachable.
func (l *Launcher) SetConnected(up bool) {
	l.connected = up
	if l.Open && l.Mode == Windows {
		l.search(true)
	}
}

// Show opens the launcher in a mode with an empty query, remembering the
// focused window so Cancel can return to it. Showing an open launcher
// switches its mode and starts over.
func (l *Launcher) Show(m Mode) {
	if !l.Open {
		l.previous = l.snap.ActiveToplevel
	}
	l.Open, l.Mode, l.Query, l.notice, l.armed = true, m, "", "", ""
	l.search(false)
}

// SetQuery updates the search text.
func (l *Launcher) SetQuery(q string) {
	if q == l.Query {
		return
	}
	l.Query, l.notice, l.armed = q, "", ""
	l.search(false)
}

// ToggleMode switches between apps and windows, keeping the query.
func (l *Launcher) ToggleMode() {
	if l.Mode == Apps {
		l.Mode = Windows
	} else {
		l.Mode = Apps
	}
	l.notice = ""
	l.search(false)
}

// Move moves the selection by delta rows, stopping at either end.
func (l *Launcher) Move(delta int) {
	l.Selected = max(0, min(l.Selected+delta, len(l.Results)-1))
	if l.armed != "" {
		l.armed, l.notice = "", ""
	}
}

// Cancel closes the launcher. Focus goes back to the window that had it
// when the launcher opened, if that window still exists; otherwise Hyprland
// keeps whatever focus it has.
func (l *Launcher) Cancel() Action {
	if !l.Open {
		return Action{}
	}
	l.Open = false
	if l.previous != "" && l.windowExists(l.previous) {
		return Action{Kind: Focus, Address: l.previous}
	}
	return Action{}
}

// Execute returns the action for the selected result. The launcher stays
// open until the view reports the outcome with Done. A window that closed
// since the list was shown yields no action: the list is refreshed and a
// notice says why.
func (l *Launcher) Execute() Action {
	if !l.Open || l.Selected >= len(l.Results) {
		return Action{}
	}
	r := l.Results[l.Selected]
	switch {
	case r.Copy != "":
		return Action{Kind: Copy, Text: r.Copy}
	case r.Fill != "":
		l.SetQuery(r.Fill)
		return Action{Kind: Fill, Text: r.Fill}
	case r.URL != "":
		return Action{Kind: Open, Text: r.URL}
	case r.Command != "":
		if c, ok := CommandByID(r.Command); ok && c.Confirm != "" && l.armed != r.Command {
			l.armed = r.Command
			l.notice = "Press Enter again to " + c.Confirm
			return Action{}
		}
		l.armed = ""
		return Action{Kind: Run, Text: r.Command, Arg: r.Arg, Address: l.previous}
	}
	if r.Entry != nil {
		return Action{Kind: Launch, Entry: r.Entry}
	}
	if !l.windowExists(r.Address) {
		l.search(false)
		l.notice = "That window has closed"
		return Action{}
	}
	return Action{Kind: Focus, Address: r.Address}
}

// Done reports the outcome of an executed action: success closes the
// launcher, a failure stays open and is shown.
func (l *Launcher) Done(err error) {
	if err != nil {
		l.notice = err.Error()
		return
	}
	l.Open = false
}

// Message is the line shown with the results: feedback about the last
// action, or why the list is empty. "" when there is nothing to say.
func (l *Launcher) Message() string {
	if l.notice != "" {
		return l.notice
	}
	if len(l.Results) > 0 {
		return ""
	}
	switch {
	case l.Mode == Apps && strings.HasPrefix(strings.TrimSpace(l.Query), ":"):
		return "No emoji or symbol matches"
	case l.Mode == Apps && !l.appsReady:
		return "Reading apps…"
	case l.Mode == Apps:
		return "No app matches"
	case !l.connected:
		return "Not connected to Hyprland"
	case len(l.snap.Toplevels) == 0:
		return "No windows open"
	}
	return "No window matches"
}

// Failed reports whether Message is about a failed action rather than an
// empty list.
func (l *Launcher) Failed() bool { return l.notice != "" && l.armed == "" }

// windowTitle is a window's title, or its class; "" when it is gone.
func (l *Launcher) windowTitle(addr string) string {
	for _, t := range l.snap.Toplevels {
		if t.Address == addr {
			return cmp.Or(t.Title, t.Class)
		}
	}
	return ""
}

func (l *Launcher) windowExists(addr string) bool {
	return slices.ContainsFunc(l.snap.Toplevels, func(t hypr.Toplevel) bool { return t.Address == addr })
}

// search rebuilds the results. keep holds on to the selected window across
// a refresh; otherwise the best match is selected.
func (l *Launcher) search(keep bool) {
	var keepAddr string
	if keep && l.Selected < len(l.Results) {
		keepAddr = l.Results[l.Selected].Address
	}
	q := strings.ToLower(strings.TrimSpace(l.Query))
	if l.Mode == Apps {
		if e, ok := strings.CutPrefix(q, ":"); ok { // emoji and symbols only
			l.Results = emojiResults(e)
			l.Selected = 0
			return
		}
		// Apps and commands, best first; on a tie the app.
		l.Results = append(searchApps(l.apps, q), searchCommands(q, l.windowTitle(l.previous))...)
		slices.SortStableFunc(l.Results, func(a, b Result) int { return b.score - a.score })
		// A question ("10 km to mi", "2^10") answers first, then a
		// quicklink or a web address; the web search comes last.
		first, last := webResults(l.Query, l.webSearch, l.links)
		l.Results = append(append(first, l.Results...), last...)
		if a, ok := convert.Evaluate(l.Query, l.rates); ok {
			l.Results = append([]Result{{Title: a.Text, Detail: a.Detail + " · Enter copies", Icon: "accessories-calculator-symbolic", Copy: a.Copy}}, l.Results...)
		}
	} else {
		l.Results = searchWindows(l.snap, q)
	}
	l.Selected = 0
	if keepAddr != "" {
		if i := slices.IndexFunc(l.Results, func(r Result) bool { return r.Address == keepAddr }); i >= 0 {
			l.Selected = i
			return
		}
	}
	// Like Alt-Tab: with no query the windows are in focus order, and the
	// one to switch to is the previous one, not the one already focused.
	if l.Mode == Windows && q == "" && len(l.Results) > 1 && l.Results[0].Address == l.snap.ActiveToplevel {
		l.Selected = 1
	}
}

func searchApps(apps []*desktop.Entry, q string) []Result {
	var rs []Result
	for _, e := range apps {
		if e.NoDisplay {
			continue
		}
		score := 1
		if q != "" {
			score = rank(q, e.Name, append([]string{e.GenericName, e.ID}, e.Keywords...)...)
		}
		if score == 0 {
			continue
		}
		detail := cmp.Or(e.GenericName, e.Comment)
		rs = append(rs, Result{Title: e.Name, Detail: detail, Icon: e.Icon, Entry: e, score: score})
	}
	slices.SortStableFunc(rs, func(a, b Result) int {
		return cmp.Or(b.score-a.score, cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)))
	})
	return rs
}

func searchWindows(snap hypr.Snapshot, q string) []Result {
	var rs []Result
	for i, t := range snap.Toplevels {
		score := 1
		if q != "" {
			score = rank(q, t.Title, t.Class)
		}
		if score == 0 {
			continue
		}
		detail := t.Class
		if t.WorkspaceID != 0 {
			detail = fmt.Sprintf("%s · workspace %s", t.Class, workspaceName(snap, t.WorkspaceID))
		}
		rs = append(rs, Result{
			Title: cmp.Or(t.Title, t.Class), Detail: detail, Icon: t.Class,
			Address: t.Address, score: score, history: focusHistory(t, len(snap.Toplevels)+i),
		})
	}
	slices.SortStableFunc(rs, func(a, b Result) int {
		return cmp.Or(b.score-a.score, a.history-b.history)
	})
	return rs
}

// focusHistory is Hyprland's focusHistoryID (0 = focused last); windows
// without one sort after all that have one, in client order.
func focusHistory(t hypr.Toplevel, fallback int) int {
	if v, ok := t.Raw["focusHistoryID"].(float64); ok && v >= 0 {
		return int(v)
	}
	return fallback
}

func workspaceName(snap hypr.Snapshot, id int) string {
	for _, w := range snap.Workspaces {
		if w.ID == id && w.Name != "" {
			return w.Name
		}
	}
	return fmt.Sprint(id)
}
