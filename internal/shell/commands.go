package shell

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/emoji"
)

// Command is something the launcher can do besides opening apps: the
// shell's own actions, the system's, and actions on the window that was
// focused when the launcher opened. The view carries them out by ID.
type Command struct {
	ID, Title, Detail, Icon string
	Keywords                []string
	// Confirm, when set, makes Enter ask once more first ("shut down").
	Confirm string
	// Window commands act on the window the launcher was opened over.
	Window bool
}

// Commands are the launcher's commands, matched like apps.
var Commands = []Command{
	{ID: "lock", Title: "Lock Screen", Detail: "Lock now", Icon: "system-lock-screen-symbolic", Keywords: []string{"lock"}},
	{ID: "suspend", Title: "Sleep", Detail: "Suspend, locking first", Icon: "system-suspend-symbolic", Keywords: []string{"suspend", "sleep"}},
	{ID: "reboot", Title: "Restart", Detail: "Restart the computer", Icon: "system-reboot-symbolic", Keywords: []string{"reboot"}, Confirm: "restart"},
	{ID: "poweroff", Title: "Shut Down", Detail: "Turn the computer off", Icon: "system-shutdown-symbolic", Keywords: []string{"shutdown", "power off", "turn off"}, Confirm: "shut down"},
	{ID: "logout", Title: "Log Out", Detail: "End this session", Icon: "system-log-out-symbolic", Keywords: []string{"logout", "sign out", "exit"}, Confirm: "log out"},
	{ID: "trash", Title: "Empty Trash", Detail: "Delete what is in the Trash for good", Icon: "user-trash-symbolic", Keywords: []string{"trash", "bin", "recycle"}, Confirm: "empty the Trash"},
	{ID: "settings", Title: "Settings", Detail: "inua's settings", Icon: "emblem-system-symbolic", Keywords: []string{"preferences", "config"}},
	{ID: "shot-area", Title: "Screenshot Area", Detail: "Drag out an area", Icon: "applets-screenshooter-symbolic", Keywords: []string{"screenshot", "capture", "snip"}},
	{ID: "shot-window", Title: "Screenshot Window", Detail: "Click a window", Icon: "applets-screenshooter-symbolic", Keywords: []string{"screenshot", "capture"}},
	{ID: "shot-screen", Title: "Screenshot Screen", Detail: "Click a screen", Icon: "applets-screenshooter-symbolic", Keywords: []string{"screenshot", "capture"}},
	{ID: "record", Title: "Record Screen", Detail: "Record an area, a window or a screen", Icon: "media-record-symbolic", Keywords: []string{"screencast", "video", "capture"}},
	{ID: "color", Title: "Pick a Colour", Detail: "From anywhere on the screen", Icon: "color-select-symbolic", Keywords: []string{"color", "eyedropper", "picker"}},
	{ID: "clear-notes", Title: "Clear Notifications", Detail: "All of them", Icon: "preferences-system-notifications-symbolic", Keywords: []string{"notifications", "dismiss"}},
	{ID: "focus", Title: "Focus", Detail: "Quiet notifications, or not", Icon: "weather-clear-night-symbolic", Keywords: []string{"do not disturb", "dnd", "quiet"}},
	{ID: "wifi", Title: "Toggle Wi-Fi", Detail: "Turn Wi-Fi on or off", Icon: "network-wireless-signal-excellent-symbolic", Keywords: []string{"wireless", "network", "wifi"}},
	{ID: "bluetooth", Title: "Toggle Bluetooth", Detail: "Turn Bluetooth on or off", Icon: "bluetooth-active-symbolic", Keywords: []string{"bluetooth"}},
	{ID: "restart-shell", Title: "Restart inua", Detail: "Start the shell again", Icon: "system-reboot-symbolic", Keywords: []string{"reload", "shell"}},
	{ID: "close-window", Title: "Close Window", Icon: "window-close-symbolic", Keywords: []string{"quit", "kill"}, Window: true},
	{ID: "float", Title: "Toggle Floating", Icon: "view-restore-symbolic", Keywords: []string{"float", "tile", "tiling"}, Window: true},
	{ID: "fullscreen", Title: "Toggle Fullscreen", Icon: "view-fullscreen-symbolic", Keywords: []string{"maximize", "maximise"}, Window: true},
	{ID: "center", Title: "Center Window", Icon: "view-restore-symbolic", Keywords: []string{"centre", "middle"}, Window: true},
}

// CommandByID finds a command.
func CommandByID(id string) (Command, bool) {
	i := slices.IndexFunc(Commands, func(c Command) bool { return c.ID == id })
	if i < 0 {
		return Command{}, false
	}
	return Commands[i], true
}

// "workspace 3", "ws 3": go there; "move to workspace 3", "send to ws 3":
// take the window there.
var workspaceRe = regexp.MustCompile(`^(?:(move|send)\s+(?:window\s+)?(?:to\s+)?)?(?:workspace|ws)\s*(\d{1,2})$`)

// searchCommands are the commands matching q, with window commands only
// when there is a window (title) to act on.
func searchCommands(q, window string) []Result {
	if q == "" {
		return nil
	}
	var rs []Result
	if m := workspaceRe.FindStringSubmatch(q); m != nil {
		if m[1] == "" {
			rs = append(rs, Result{Title: "Go to Workspace " + m[2], Detail: "Workspace", Icon: "view-app-grid-symbolic", Command: "workspace", Arg: m[2], score: 1000})
		} else if window != "" {
			rs = append(rs, Result{Title: "Move Window to Workspace " + m[2], Detail: window, Icon: "view-app-grid-symbolic", Command: "move-window", Arg: m[2], score: 1000})
		}
	}
	for _, c := range Commands {
		if c.Window && window == "" {
			continue
		}
		score := commandScore(q, c)
		if score == 0 {
			continue
		}
		detail := c.Detail
		if c.Window {
			detail = window
		}
		rs = append(rs, Result{Title: c.Title, Detail: detail, Icon: c.Icon, Command: c.ID, score: score})
	}
	return rs
}

// commandScore ranks a command like an app, but only on a word of its
// title or the start of a keyword: letters scattered through "Record
// Screen" are no reason to offer it for "code".
func commandScore(q string, c Command) int {
	best := 0
	if m := match(c.Title, q); m >= wordStart {
		best = 2 * m
	}
	for _, k := range c.Keywords {
		if m := match(k, q); m >= prefix {
			best = max(best, m)
		}
	}
	return best
}

// Quicklink is a site the launcher searches by keyword (config's).
type Quicklink struct{ Keyword, Name, URL string }

// SearchLink fills a search address with words.
func SearchLink(template, words string) string {
	return strings.Replace(template, "%s", url.QueryEscape(words), 1)
}

// engineName names a search address: its quicklink's name, else its host.
func engineName(template string, links []Quicklink) string {
	for _, l := range links {
		if l.URL == template {
			return l.Name
		}
	}
	if u, err := url.Parse(template); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Host, "www.")
	}
	return "the web"
}

// A web address typed as is: a scheme, or a domain with a dot and letters
// after it ("example.com/x").
var (
	schemeRe = regexp.MustCompile(`^(?i)(https?|ftp)://\S+$`)
	domainRe = regexp.MustCompile(`^(?i)(?:[a-z0-9-]+\.)+[a-z]{2,}(?::\d+)?(?:/\S*)?$`)
)

// webResults are the query's quicklink, address and web search: the
// first two go first, the search last.
func webResults(query, search string, links []Quicklink) (first, last []Result) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	word, rest, _ := strings.Cut(q, " ")
	rest = strings.TrimSpace(rest)
	for _, l := range links {
		switch {
		case strings.EqualFold(word, l.Keyword) && rest != "":
			first = append(first, Result{Title: fmt.Sprintf("Search %s for “%s”", l.Name, rest), Detail: l.Keyword + " · " + hostOf(l.URL), Icon: "system-search-symbolic", URL: SearchLink(l.URL, rest)})
		case rest == "" && (strings.EqualFold(q, l.Keyword) || rank(strings.ToLower(q), l.Name) >= 2*prefix):
			first = append(first, Result{Title: l.Name, Detail: fmt.Sprintf("Type %s and words to search", l.Keyword), Icon: "system-search-symbolic", Fill: l.Keyword + " "})
		}
	}
	if !strings.Contains(q, " ") {
		switch {
		case schemeRe.MatchString(q):
			first = append(first, Result{Title: "Open " + q, Detail: "Web address", Icon: "system-search-symbolic", URL: q})
		case domainRe.MatchString(q) && !strings.Contains(q, ".desktop"):
			first = append(first, Result{Title: "Open " + q, Detail: "Web address", Icon: "system-search-symbolic", URL: "https://" + q})
		}
	}
	if search != "" {
		last = append(last, Result{Title: fmt.Sprintf("Search %s for “%s”", engineName(search, links), q), Detail: "Web search", Icon: "system-search-symbolic", URL: SearchLink(search, q)})
	}
	return first, last
}

func hostOf(template string) string {
	if u, err := url.Parse(template); err == nil {
		return strings.TrimPrefix(u.Host, "www.")
	}
	return ""
}

// emojiResults are the emoji and symbols for a query after ":".
func emojiResults(q string) []Result {
	var rs []Result
	for _, it := range emoji.Search(q, 60) {
		kind := "emoji"
		if it.Symbol {
			kind = "symbol"
		}
		detail := kind
		if len(it.Keywords) > 0 {
			detail = kind + " · " + strings.Join(it.Keywords[:min(4, len(it.Keywords))], ", ")
		}
		rs = append(rs, Result{Title: it.Name, Detail: detail, Glyph: it.Char, Copy: it.Char})
	}
	return rs
}
