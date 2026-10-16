// Package config reads the shell's settings file,
// $XDG_CONFIG_HOME/inua/config.toml. Every setting has a default, so a
// missing file, a missing key or a bad value never stops the shell: bad
// values are reported as warnings and replaced by their defaults. The
// documented sample is config/inua/config.toml at the top of the repository.
package config

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the shell's settings.
type Config struct {
	// Language is the interface's language, one of Languages: "auto"
	// follows the environment (LANGUAGE, LC_ALL, LC_MESSAGES, LANG).
	Language   string     `toml:"language"`
	Bar        Bar        `toml:"bar"`
	Launcher   Launcher   `toml:"launcher"`
	Overview   Overview   `toml:"overview"`
	Control    Control    `toml:"control"`
	Style      Style      `toml:"style"`
	Background Background `toml:"background"`
	Input      Input      `toml:"input"`
	Notify     Notify     `toml:"notifications"`
	Dock       Dock       `toml:"dock"`
	Assistant  Assistant  `toml:"assistant"`
	Keyring    Keyring    `toml:"keyring"`
	MCP        MCP        `toml:"mcp"`
	Screenshot Screenshot `toml:"screenshot"`
	Clipboard  Clipboard  `toml:"clipboard"`
	Shortcuts  []Shortcut `toml:"shortcuts"`

	// Dir is the directory the file was read from; relative paths in the
	// file are resolved against it.
	Dir string `toml:"-"`
}

type Bar struct {
	// Enable shows the bar. Off, the launcher, control center and
	// notification center are still a key away.
	Enable bool `toml:"enable"`
	// Clock is a strftime format (see Strftime).
	Clock string `toml:"clock"`
	// Workspaces is "monitor" (each bar shows its monitor's workspaces)
	// or "all".
	Workspaces string `toml:"workspaces"`
	Title      bool   `toml:"title"`
	Tray       bool   `toml:"tray"`
}

type Launcher struct {
	// Mode is what `launcher toggle` opens: "apps" or "windows".
	Mode string `toml:"mode"`
	// Terminal is the default terminal: `inua terminal` opens it, and it
	// runs Terminal=true apps, followed by "-e" and the command. Empty
	// means $TERMINAL, then xdg-terminals.list's, then xdg-terminal-exec.
	Terminal string `toml:"terminal"`
	// Search is the web search offered after the results: a URL with %s
	// for the words typed.
	Search string `toml:"search"`
	// Quicklinks search a site when the query starts with their keyword
	// ("gh inua").
	Quicklinks []Quicklink `toml:"quicklinks"`
}

type Overview struct {
	MRU        bool `toml:"mru"`
	PerMonitor bool `toml:"per_monitor"`
}

// Quicklink is a site the launcher searches by keyword.
type Quicklink struct {
	Keyword string `toml:"keyword"` // the query's first word: "gh"
	Name    string `toml:"name"`    // "GitHub"
	URL     string `toml:"url"`     // with %s for the rest of the query
}

// DefaultQuicklinks are the quicklinks of a file that names none.
var DefaultQuicklinks = []Quicklink{
	{"g", "Google", "https://www.google.com/search?q=%s"},
	{"ddg", "DuckDuckGo", "https://duckduckgo.com/?q=%s"},
	{"gh", "GitHub", "https://github.com/search?q=%s"},
	{"yt", "YouTube", "https://www.youtube.com/results?search_query=%s"},
	{"wiki", "Wikipedia", "https://en.wikipedia.org/w/index.php?search=%s"},
	{"maps", "Google Maps", "https://www.google.com/maps/search/%s"},
	{"tr", "Google Translate", "https://translate.google.com/?text=%s"},
}

// ValidSearchURL reports whether u is a web address with %s in it.
func ValidSearchURL(u string) bool {
	return (strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")) && strings.Count(u, "%s") == 1
}

type Control struct {
	// Shell commands behind the lock and screenshot buttons.
	Lock       string `toml:"lock"`
	Screenshot string `toml:"screenshot"`
}

type Screenshot struct {
	// Folder is where screenshots are saved, "~/" for the home directory.
	// Empty: they are not saved.
	Folder string `toml:"folder"`
	// Copy puts each screenshot on the clipboard as well.
	Copy bool `toml:"copy"`
	// Recordings is where screen recordings are saved, "~/" for the home
	// directory.
	Recordings string `toml:"recordings"`
}

type Style struct {
	// Theme is the colour theme, one of Themes or a theme of the user's
	// (ThemeNames).
	Theme string `toml:"theme"`
	// CSS is a stylesheet loaded after the built-in style, relative to the
	// config directory. Ignored if the file does not exist.
	CSS string `toml:"css"`
	// Icons is the icon set of the shell's own icons, one of IconSets or a
	// set of the user's (IconSetNames).
	Icons string `toml:"icons"`
	// EdgeGaps leaves a small gap between the screen edges and the bar and
	// dock; off puts them flush against the edges.
	EdgeGaps bool `toml:"edge_gaps"`
	// TextScale sizes the built-in style's text, 80 to 150 percent. Icons,
	// spacing and the user's stylesheet keep their sizes.
	TextScale int `toml:"text_scale"`
}

type Background struct {
	// Enable draws the wallpaper; off leaves the background to another
	// program (or to Hyprland).
	Enable bool `toml:"enable"`
	// Image is the picture, "~/" for the home directory, relative paths
	// against the config directory. Empty: the colour with a fine texture.
	Image string `toml:"image"`
	// Fit is one of Fits.
	Fit string `toml:"fit"`
	// Color shows around and behind the image: "#rrggbb", or empty for the
	// theme's ground.
	Color string `toml:"color"`
}

// Shortcut is a keyboard shortcut set in inua's settings window. Written
// to hypr/inua.conf, it unbinds its keys from anything else, and the keys
// its action had in hyprland.conf (Replaces), then binds them.
type Shortcut struct {
	// Action is one of ShortcutActions, or "exec" for Command.
	Action string `toml:"action"`
	// Keys are modifiers and a key, "SUPER+SHIFT+S"; "" turns the action's
	// shortcut off.
	Keys     string   `toml:"keys"`
	Command  string   `toml:"command"` // for "exec"
	Name     string   `toml:"name"`    // for "exec", shown in the settings window
	Replaces []string `toml:"replaces"`
}

// ShortcutActions are the actions a shortcut can run besides "exec"; the
// commands behind them are in internal/hyprconf.
var ShortcutActions = []string{
	"launcher", "windows", "overview", "control", "notifications", "clipboard", "focus", "settings", "restart", "lock",
	"shot-area", "shot-screen", "shot-window", "shot-record", "color",
}

// renamedActions are actions that became others; a config naming one is
// read as the new one.
var renamedActions = map[string]string{
	"shot-area-file": "shot-area", "shot-screen-file": "shot-screen",
}

// OldScreenshotCommand was the screenshot button's default before inua took
// screenshots itself; a config still holding it gets the new default.
const OldScreenshotCommand = `grim -g "$(slurp)" - | wl-copy`

// OldLockCommand was the lock button's default before inua had a lock
// screen; a config still holding it gets the new default.
const OldLockCommand = "pidof hyprlock || hyprlock"

var keysRe = regexp.MustCompile(`^((SUPER|CTRL|ALT|SHIFT)\+)*[A-Za-z0-9_]+$`)

// ValidKeys reports whether s is modifiers and a key ("SUPER+SHIFT+S"), or
// empty (off).
func ValidKeys(s string) bool { return s == "" || keysRe.MatchString(s) }

// Assistant is the agent in inua-assistant (docs/feat/assistant.md).
type Assistant struct {
	Enable bool `toml:"enable"`
	// Profile names the profile that answers; the window can switch to
	// another for a conversation.
	Profile string `toml:"profile"`
	// Fallback are profiles tried in turn when the one answering fails
	// (an outage, a rate limit): Mister Morph's main_loop route.
	Fallback []string `toml:"fallback"`
	// Decision names the profile that makes quick choices from a fixed
	// list, as the games' AI players do: Mister Morph's decision route;
	// "" for the one that answers, or, when it makes none, the first that
	// does.
	Decision string `toml:"decision"`
	// Tools are Mister Morph's built-in tools to add to inua's.
	Tools []string `toml:"tools"`
	// AskBeforeChanges makes the assistant ask before changing anything;
	// power, ending processes and closing windows always ask.
	AskBeforeChanges bool `toml:"ask_before_changes"`
	// ServeEnable controls the Console Runtime API independently of its address.
	ServeEnable bool   `toml:"serve_enable"`
	ServeListen string `toml:"serve_listen"`
	// Profiles are the models it can use, as Mister Morph's llm.profiles.
	Profiles []AIProfile `toml:"profiles"`
}

// AIProfile is one model the assistant can use.
type AIProfile struct {
	Name string `toml:"name"`
	// Provider is Mister Morph's inference provider: one of AIProviders,
	// or another Mister Morph knows, kept as it is.
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
	// Endpoint is the API's address, for a compatible server or a proxy;
	// "" for the provider's own.
	Endpoint string `toml:"endpoint"`
	// Reasoning is the reasoning effort (ReasoningEfforts); "" for the
	// model's default.
	Reasoning string `toml:"reasoning"`
	// Abilities are what the profile is for (AIAbilities): answering
	// (AbilityText), quick choices (AbilityDecision); none for both.
	Abilities []string `toml:"abilities"`
}

// What a profile is for, as Mister Morph's abilities: "text" answers
// questions, running the agent; "decision" makes quick choices from a
// list (Mister Morph's Evaluate), and a profile with it but not "text"
// only makes those; "image" makes pictures, which inua does not ask for
// but keeps.
const (
	AbilityText     = "text"
	AbilityDecision = "decision"
	AbilityImage    = "image"
)

// AIAbilities are the abilities a profile may list.
var AIAbilities = []string{AbilityText, AbilityDecision, AbilityImage}

// Can reports whether the profile is for ability; one listing none is for
// answering and quick choices, and one of a provider that only makes
// those (DecisionOnly) is for them alone.
func (p AIProfile) Can(ability string) bool {
	if DecisionOnly(p.Provider) {
		return ability == AbilityDecision
	}
	return CanDo(p.Abilities, ability)
}

// CanDo reports whether abilities include ability, none being answering
// and quick choices.
func CanDo(abilities []string, ability string) bool {
	if len(abilities) == 0 {
		return ability == AbilityText || ability == AbilityDecision
	}
	return slices.Contains(abilities, ability)
}

// WithAbility turns ability on or off in abilities; answering and quick
// choices alone come back as none.
func WithAbility(abilities []string, ability string, on bool) []string {
	var out []string
	for _, a := range AIAbilities {
		if a == ability && on || a != ability && CanDo(abilities, a) {
			out = append(out, a)
		}
	}
	if slices.Equal(out, []string{AbilityText, AbilityDecision}) {
		return nil
	}
	return out
}

// AbilityList lists what the profile is for, none read as both: for
// Mister Morph, which reads none differently from one version to another.
func (p AIProfile) AbilityList() []string {
	if DecisionOnly(p.Provider) {
		return []string{AbilityDecision}
	}
	if len(p.Abilities) == 0 {
		return []string{AbilityText, AbilityDecision}
	}
	return slices.Clone(p.Abilities)
}

// ProfileNamed returns the profile named, or false.
func (a Assistant) ProfileNamed(name string) (AIProfile, bool) {
	for _, p := range a.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return AIProfile{}, false
}

// AIProviders are the inference providers Mister Morph knows, as the
// settings window offers them.
var AIProviders = []struct{ ID, Name string }{
	{"openai", "OpenAI"},
	{"anthropic", "Anthropic"},
	{"gemini", "Google Gemini"},
	{"xai", "xAI"},
	{"deepseek", "DeepSeek"},
	{"kimi", "Kimi (Moonshot)"},
	{"openrouter", "OpenRouter"},
	{"groq", "Groq"},
	{"mistermorph_pro", "Mister Morph Pro"},
	{"openai_chat_compatible", "OpenAI-compatible (Chat Completions)"},
	{"openai_response_compatible", "OpenAI-compatible (Responses)"},
	{"anthropic_compatible", "Anthropic-compatible"},
	{"typesafe", "TypeSafe"},
}

// DecisionOnly reports whether provider only makes quick choices, never
// chats: TypeSafe, which Mister Morph asks only on the decision route.
func DecisionOnly(provider string) bool { return provider == "typesafe" }

// AIProviderIDs lists AIProviders' IDs.
func AIProviderIDs() []string {
	ids := make([]string, len(AIProviders))
	for i, p := range AIProviders {
		ids[i] = p.ID
	}
	return ids
}

// ReasoningEfforts are the reasoning efforts a profile may ask for.
var ReasoningEfforts = []string{"", "minimal", "low", "medium", "high"}

// profileNameRe is a profile's name: Mister Morph's route keys.
var profileNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ValidProfileName reports whether a profile may be named so: lowercase
// letters, digits, "-" and "_", and not Mister Morph's "default".
func ValidProfileName(name string) bool { return profileNameRe.MatchString(name) && name != "default" }

// validate drops profiles without a usable name, provider or model, and
// points Profile, Fallback and Decision at profiles for them. A provider
// inua does not list is kept: Mister Morph may know it, and says if not.
func (a *Assistant) validate(d Assistant) []string {
	var w []string
	seen := map[string]bool{}
	a.Profiles = slices.DeleteFunc(a.Profiles, func(p AIProfile) bool {
		var why string
		switch {
		case !ValidProfileName(p.Name):
			why = `want a name of lowercase letters, digits, "-" and "_", not "default"`
		case seen[p.Name]:
			why = "named twice"
		case strings.TrimSpace(p.Provider) == "":
			why = "want a provider of " + strings.Join(AIProviderIDs(), ", ")
		case strings.TrimSpace(p.Model) == "":
			why = "want a model"
		case !slices.Contains(ReasoningEfforts, p.Reasoning):
			why = "want reasoning of " + strings.Join(ReasoningEfforts[1:], ", ") + ` or ""`
		}
		if why != "" {
			w = append(w, fmt.Sprintf("assistant.profiles %q: %s; leaving it out", p.Name, why))
			return true
		}
		seen[p.Name] = true
		return false
	})
	for i := range a.Profiles {
		p := &a.Profiles[i]
		var abilities []string
		for _, ab := range p.Abilities {
			switch {
			case !slices.Contains(AIAbilities, ab):
				w = append(w, fmt.Sprintf("assistant.profiles %q: ability %q: want %s; leaving it out", p.Name, ab, strings.Join(AIAbilities, ", ")))
			case !slices.Contains(abilities, ab):
				abilities = append(abilities, ab)
			}
		}
		p.Abilities = abilities
		if DecisionOnly(p.Provider) && !slices.Equal(p.Abilities, []string{AbilityDecision}) {
			if len(p.Abilities) > 0 {
				w = append(w, fmt.Sprintf(`assistant.profiles %q: %s makes quick choices only; abilities ["decision"]`, p.Name, p.Provider))
			}
			p.Abilities = []string{AbilityDecision}
		}
	}
	if len(a.Profiles) == 0 {
		a.Profiles = slices.Clone(d.Profiles)
	}
	answers := func(name string) bool {
		p, ok := a.ProfileNamed(name)
		return ok && p.Can(AbilityText)
	}
	if i := slices.IndexFunc(a.Profiles, func(p AIProfile) bool { return p.Can(AbilityText) }); i < 0 {
		w = append(w, `assistant.profiles: none has the "text" ability; the assistant cannot answer`)
		if _, ok := a.ProfileNamed(a.Profile); !ok {
			a.Profile = a.Profiles[0].Name
		}
	} else if first := a.Profiles[i].Name; !answers(a.Profile) {
		switch _, ok := a.ProfileNamed(a.Profile); {
		case !ok && a.Profile != "":
			w = append(w, fmt.Sprintf("assistant.profile %q: no such profile; using %q", a.Profile, first))
		case ok:
			w = append(w, fmt.Sprintf(`assistant.profile %q: does not have the "text" ability; using %q`, a.Profile, first))
		}
		a.Profile = first
	}
	used := map[string]bool{a.Profile: true}
	a.Fallback = slices.DeleteFunc(a.Fallback, func(name string) bool {
		if answers(name) && !used[name] {
			used[name] = true
			return false
		}
		w = append(w, fmt.Sprintf("assistant.fallback %q: not another profile that answers; leaving it out", name))
		return true
	})
	if len(a.Fallback) == 0 {
		a.Fallback = nil // as a file with "fallback = []" reads
	}
	if a.Decision != "" {
		switch p, ok := a.ProfileNamed(a.Decision); {
		case !ok:
			w = append(w, fmt.Sprintf("assistant.decision %q: no such profile; using the one that answers", a.Decision))
			a.Decision = ""
		case !p.Can(AbilityDecision):
			w = append(w, fmt.Sprintf(`assistant.decision %q: does not have the "decision" ability; using the one that answers`, a.Decision))
			a.Decision = ""
		}
	}
	return w
}

// DecisionProfile is the profile that makes quick choices: Decision, else
// the one that answers, else the first that makes them; "" for none.
func (a Assistant) DecisionProfile() string {
	if a.Decision != "" {
		return a.Decision
	}
	if p, ok := a.ProfileNamed(a.Profile); ok && p.Can(AbilityDecision) {
		return p.Name
	}
	for _, p := range a.Profiles {
		if p.Can(AbilityDecision) {
			return p.Name
		}
	}
	return ""
}

// AssistantTools are the built-in tools the assistant may add, the first
// two by default; the rest read files or run commands, and ask each time.
var AssistantTools = []string{"web_search", "url_fetch", "read_file", "bash"}

// Keyring is inua-keyring, the Secret Service kept in pass
// (docs/feat/keyring.md).
type Keyring struct {
	// Folder is the password store's folder for its items.
	Folder string `toml:"folder"`
	// Notify tells of each app's first secret read since inua-keyring
	// started; off by default. Settings → Keyring lists every read.
	Notify bool `toml:"notify"`
	// Untrusted answers a read by an app not trusted with the secret when
	// no one can be asked (no shell): "allow" (recorded) or "deny".
	Untrusted string `toml:"untrusted"`
	// LockAfter locks the keyring after so many minutes without a read;
	// 0 locks it only with the screen, before suspend, or when asked.
	LockAfter int `toml:"lock_after"`
}

// MCP is inua as an MCP server: agents on this computer use the
// assistant's tools through `inua mcp` (docs/feat/mcp.md).
type MCP struct {
	// Enable lets agents connect; off by default.
	Enable bool `toml:"enable"`
	// Policy is what an agent may do: "read" (look only), "ask" (ask
	// before changes) or "allow" (change without asking; the riskiest
	// still ask).
	Policy string `toml:"policy"`
}

// MCPPolicies are mcp.policy's values.
var MCPPolicies = []string{"read", "ask", "allow"}

// Dock is the app dock along a screen edge of each monitor.
type Dock struct {
	Enable bool `toml:"enable"`
	// Position is the screen edge the dock occupies.
	Position string `toml:"position"`
	// Autohide keeps the dock out of sight until the pointer reaches the
	// selected edge; otherwise windows make room for it.
	Autohide bool `toml:"autohide"`
	// Size is the icons' size in pixels, one of DockSizes.
	Size int `toml:"size"`
	// Drawer shows a button at the dock's end that opens inua's light apps.
	Drawer bool `toml:"drawer"`
	// Pinned lists the apps always on the dock, by desktop ID
	// ("org.gnome.Nautilus" for org.gnome.Nautilus.desktop), in order.
	Pinned []string `toml:"pinned"`
}

// DockSizes are the icon sizes the settings window offers.
var DockSizes = []int{28, 36, 44, 56}

// DockPositions are the edges offered in the dock settings.
var DockPositions = []string{"bottom", "top", "left", "right"}

// Clipboard is the clipboard history (internal/clipboard) and its panel.
type Clipboard struct {
	// Enable keeps a history of what is copied; off, nothing is recorded.
	Enable bool `toml:"enable"`
	// Bar shows a clipboard history button in the top bar.
	Bar bool `toml:"bar"`
	// Opacity is the panel's, 50 to 100 percent.
	Opacity int `toml:"opacity"`
	// Limit is how many entries the history keeps, 20 to 1000.
	Limit int `toml:"limit"`
	// PasteOnClick pastes a clicked entry into the window that was
	// focused; off, it is only copied.
	PasteOnClick bool `toml:"paste_on_click"`
	// SensitiveSources are programs and window classes whose copies are
	// not recorded, in any case: clipboard.FindSource says where a copy
	// comes from.
	SensitiveSources []string `toml:"sensitive_sources"`
}

// DefaultSensitiveSources are the password managers whose copies are not
// recorded unless the settings say otherwise. KeePassXC and inua's Pass
// also mark their copies as sensitive.
var DefaultSensitiveSources = []string{"pass", "gopass", "passmenu", "rofi-pass", "keepassxc", "org.keepassxc.KeePassXC", "bitwarden", "1password", "io.github.lyricat.Inua.Pass"}

// ClipboardLimitChoices are the history limits the settings window offers.
var ClipboardLimitChoices = []int{50, 100, 300, 500}

// Notify is the notification center (internal/notify).
type Notify struct {
	// Enable makes the shell the notification server. Off leaves
	// notifications to another daemon (mako, dunst).
	Enable bool `toml:"enable"`
	// Banner is how long a banner shows, in seconds. Critical
	// notifications stay until dismissed.
	Banner int `toml:"banner_seconds"`
	// CriticalInFocus lets critical notifications show banners in Focus.
	CriticalInFocus bool `toml:"critical_in_focus"`
	// Updates checks once a day for a new inua release and says so in a
	// notification (internal/upgrade).
	Updates bool `toml:"updates"`
	// History keeps the notifications that came (internal/notify,
	// HistoryPath), for the desk functions; off clears it.
	History bool `toml:"history"`
}

// BannerChoices are the banner durations the settings window offers.
var BannerChoices = []int{3, 5, 8, 12}

// Input is the Hyprland input settings inua manages. They are written to
// hypr/inua.conf (internal/hyprconf), which hyprland.conf sources. Unset
// values (nil, "") are not written, leaving them to hyprland.conf.
type Input struct {
	// KbOptions is Hyprland's input:kb_options for every keyboard: XKB
	// options, comma-separated ("caps:escape,ctrl:swap_lalt_lctl").
	KbOptions *string `toml:"kb_options"`
	// TouchpadSpeed is the touchpads' sensitivity, -1 to 1.
	TouchpadSpeed *float64 `toml:"touchpad_speed"`
	// TouchpadAccel is the touchpads' accel_profile: "adaptive" or "flat".
	TouchpadAccel string `toml:"touchpad_accel"`
}

// Fits are the ways an image fills a monitor: cover (fill, cropping),
// contain (whole image, bars around it), tile (repeat at its own size),
// center (its own size, centred).
var Fits = []string{"cover", "contain", "tile", "center"}

// IconSets are the shell's icon sets: "phosphor", Phosphor's regular icons
// (ui/icons), or "system", the system's icon theme. App icons always come
// from the system's. Sets of the user's follow them (IconSetNames).
var IconSets = []string{"phosphor", "system"}

// TextScales are the text sizes, in percent, the settings window offers.
var TextScales = []int{90, 100, 110, 125, 150}

// Languages are the interface's languages: "auto" (the environment's),
// English, then the catalogs in internal/i18n/locales.
var Languages = []string{"auto", "en", "zh_CN", "zh_TW", "ja", "ko", "es", "fr"}

// Themes are the built-in colour themes (ui/themes/<name>.css): three
// everyday ones, then four dark editions.
var Themes = []string{"yoru", "washi", "aojashin", "suimen", "rinko", "bunko", "denko"}

// Default returns the settings used when the file says nothing.
func Default() Config {
	return Config{
		Language: "auto",
		Bar: Bar{
			Enable:     true,
			Clock:      "%a %b %-d  %H:%M",
			Workspaces: "monitor",
			Title:      true,
			Tray:       true,
		},
		Launcher: Launcher{Mode: "apps", Search: "https://www.google.com/search?q=%s", Quicklinks: slices.Clone(DefaultQuicklinks)},
		Overview: Overview{MRU: false, PerMonitor: true},
		Control: Control{
			Lock:       "inua call lock now",
			Screenshot: "inua call screenshot area",
		},
		Style:      Style{Theme: "yoru", CSS: "style.css", Icons: "phosphor", EdgeGaps: true, TextScale: 100},
		Background: Background{Enable: true, Fit: "cover"},
		Notify:     Notify{Enable: true, Banner: 5, CriticalInFocus: true, Updates: true},
		Screenshot: Screenshot{Folder: "~/Pictures/Screenshots", Copy: true, Recordings: "~/Videos/Screencasts"},
		Clipboard:  Clipboard{Enable: true, Bar: true, Opacity: 84, Limit: 300, PasteOnClick: true, SensitiveSources: slices.Clone(DefaultSensitiveSources)},
		Dock:       Dock{Enable: true, Position: "bottom", Size: 36, Drawer: true, Pinned: []string{"firefox", "io.github.lyricat.Inua.Files", "kitty"}},
		Assistant: Assistant{Enable: true, Profile: "openai", Tools: []string{"web_search", "url_fetch"}, AskBeforeChanges: true, ServeListen: "127.0.0.1:8794",
			Profiles: []AIProfile{{Name: "openai", Provider: "openai", Model: "gpt-5.2"}}},
		Keyring: Keyring{Folder: "inua-keyring", Untrusted: "allow"},
		MCP:     MCP{Policy: "ask"},
	}
}

// DefaultPath is $XDG_CONFIG_HOME/inua/config.toml (~/.config/...).
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "inua", "config.toml")
}

// Load reads the file at path over the defaults. A missing file gives the
// defaults. The error is for a file that exists but cannot be read or
// parsed; the returned Config is then the defaults. Warnings name unknown
// keys and invalid values, which were replaced by their defaults.
func Load(path string) (Config, []string, error) {
	c := Default()
	c.Dir = filepath.Dir(path)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil, nil
	}
	if err != nil {
		return c, nil, err
	}
	md, err := toml.Decode(string(data), &c)
	if err != nil {
		d := Default()
		d.Dir = c.Dir
		return d, nil, fmt.Errorf("%s: %w", path, err)
	}
	var warnings []string
	for _, k := range md.Undecoded() {
		if k.String() == "assistant.profiles.api_key_env" {
			continue // keys were once read from a variable; now only the keyring holds them
		}
		warnings = append(warnings, fmt.Sprintf("unknown setting %s", k))
	}
	warnings = append(warnings, c.validate()...)
	return c, warnings, nil
}

// validate replaces invalid values with defaults and says which.
func (c *Config) validate() []string {
	d := Default()
	var w []string
	oneOf := func(key string, v *string, def string, allowed ...string) {
		if slices.Contains(allowed, *v) {
			return
		}
		w = append(w, fmt.Sprintf("%s = %q: want %s; using %q", key, *v, strings.Join(allowed, " or "), def))
		*v = def
	}
	oneOf("bar.workspaces", &c.Bar.Workspaces, d.Bar.Workspaces, "monitor", "all")
	oneOf("launcher.mode", &c.Launcher.Mode, d.Launcher.Mode, "apps", "windows")
	if !ValidSearchURL(c.Launcher.Search) {
		w = append(w, fmt.Sprintf("launcher.search = %q: want a web address with %%s for the words; using %q", c.Launcher.Search, d.Launcher.Search))
		c.Launcher.Search = d.Launcher.Search
	}
	w = append(w, c.Assistant.validate(d.Assistant)...)
	if f := c.Keyring.Folder; f == "" || filepath.IsAbs(f) || slices.Contains(strings.Split(filepath.ToSlash(f), "/"), "..") {
		w = append(w, fmt.Sprintf("keyring.folder = %q: want a folder inside the password store; using %q", f, d.Keyring.Folder))
		c.Keyring.Folder = d.Keyring.Folder
	}
	oneOf("keyring.untrusted", &c.Keyring.Untrusted, d.Keyring.Untrusted, "allow", "deny")
	oneOf("mcp.policy", &c.MCP.Policy, d.MCP.Policy, MCPPolicies...)
	if c.Keyring.LockAfter < 0 {
		w = append(w, fmt.Sprintf("keyring.lock_after = %d: want minutes, or 0 for never; using 0", c.Keyring.LockAfter))
		c.Keyring.LockAfter = 0
	}
	c.Assistant.Tools = slices.DeleteFunc(c.Assistant.Tools, func(t string) bool {
		if slices.Contains(AssistantTools, t) {
			return false
		}
		w = append(w, fmt.Sprintf("assistant.tools %q: want one of %s; leaving it out", t, strings.Join(AssistantTools, ", ")))
		return true
	})
	keywords := map[string]bool{}
	c.Launcher.Quicklinks = slices.DeleteFunc(c.Launcher.Quicklinks, func(l Quicklink) bool {
		var why string
		switch {
		case l.Keyword == "" || strings.ContainsAny(l.Keyword, " \t"):
			why = "want a keyword without spaces"
		case keywords[l.Keyword]:
			why = "another quicklink has this keyword"
		case !ValidSearchURL(l.URL):
			why = "want a web address with %s for the words"
		}
		keywords[l.Keyword] = true
		if why != "" {
			w = append(w, fmt.Sprintf("launcher.quicklinks %q: %s; leaving it out", l.Keyword, why))
			return true
		}
		return false
	})
	oneOf("language", &c.Language, d.Language, Languages...)
	oneOf("style.theme", &c.Style.Theme, d.Style.Theme, c.ThemeNames()...)
	oneOf("style.icons", &c.Style.Icons, d.Style.Icons, c.IconSetNames()...)
	if c.Style.TextScale < 80 || c.Style.TextScale > 150 {
		w = append(w, fmt.Sprintf("style.text_scale = %d: want 80 to 150; using %d", c.Style.TextScale, d.Style.TextScale))
		c.Style.TextScale = d.Style.TextScale
	}
	oneOf("background.fit", &c.Background.Fit, d.Background.Fit, Fits...)
	if !ValidColor(c.Background.Color) {
		w = append(w, fmt.Sprintf("background.color = %q: want #rrggbb; using the theme's ground", c.Background.Color))
		c.Background.Color = ""
	}
	if p := c.ImagePath(); p != "" {
		if _, err := os.Stat(p); err != nil {
			w = append(w, fmt.Sprintf("background.image: %v", err))
		}
	}
	if o := c.Input.KbOptions; o != nil && !ValidKbOptions(*o) {
		w = append(w, fmt.Sprintf("input.kb_options = %q: want XKB options like \"caps:escape,ctrl:swap_lalt_lctl\"; leaving it to hyprland.conf", *o))
		c.Input.KbOptions = nil
	}
	if v := c.Input.TouchpadSpeed; v != nil && (*v < -1 || *v > 1) {
		w = append(w, fmt.Sprintf("input.touchpad_speed = %v: want -1 to 1; leaving it to hyprland.conf", *v))
		c.Input.TouchpadSpeed = nil
	}
	if a := c.Input.TouchpadAccel; a != "" && a != "adaptive" && a != "flat" {
		w = append(w, fmt.Sprintf("input.touchpad_accel = %q: want \"adaptive\" or \"flat\"; leaving it to hyprland.conf", a))
		c.Input.TouchpadAccel = ""
	}
	if c.Control.Screenshot == OldScreenshotCommand {
		c.Control.Screenshot = d.Control.Screenshot
	}
	if c.Control.Lock == OldLockCommand {
		c.Control.Lock = d.Control.Lock
	}
	if c.Clipboard.Opacity < 50 || c.Clipboard.Opacity > 100 {
		w = append(w, fmt.Sprintf("clipboard.opacity = %d: want 50 to 100; using %d", c.Clipboard.Opacity, d.Clipboard.Opacity))
		c.Clipboard.Opacity = d.Clipboard.Opacity
	}
	if c.Clipboard.Limit < 20 || c.Clipboard.Limit > 1000 {
		w = append(w, fmt.Sprintf("clipboard.limit = %d: want 20 to 1000; using %d", c.Clipboard.Limit, d.Clipboard.Limit))
		c.Clipboard.Limit = d.Clipboard.Limit
	}
	// Renamed actions are read as the new ones; one may now repeat another.
	seen := map[string]bool{}
	c.Shortcuts = slices.DeleteFunc(c.Shortcuts, func(sc Shortcut) bool {
		if sc.Action == "exec" {
			return false
		}
		a := cmp.Or(renamedActions[sc.Action], sc.Action)
		dup := seen[a]
		seen[a] = true
		return dup
	})
	for i, sc := range c.Shortcuts {
		if a, ok := renamedActions[sc.Action]; ok {
			c.Shortcuts[i].Action = a
		}
	}
	c.Shortcuts = slices.DeleteFunc(c.Shortcuts, func(sc Shortcut) bool {
		bad := ""
		switch {
		case sc.Action != "exec" && !slices.Contains(ShortcutActions, sc.Action):
			bad = fmt.Sprintf("action %q: want one of %s or \"exec\"", sc.Action, strings.Join(ShortcutActions, ", "))
		case sc.Action == "exec" && (strings.TrimSpace(sc.Command) == "" || strings.ContainsAny(sc.Command, "\n\r")):
			bad = "an exec shortcut needs a one-line command"
		case !ValidKeys(sc.Keys) || slices.ContainsFunc(sc.Replaces, func(k string) bool { return k == "" || !ValidKeys(k) }):
			bad = fmt.Sprintf("keys %q: want modifiers and a key, like \"SUPER+SHIFT+S\"", sc.Keys)
		}
		if bad != "" {
			w = append(w, "shortcuts: "+bad+"; dropped")
		}
		return bad != ""
	})
	if !slices.Contains(DockPositions, c.Dock.Position) {
		w = append(w, fmt.Sprintf("dock.position = %q: want bottom, top, left or right; using %q", c.Dock.Position, d.Dock.Position))
		c.Dock.Position = d.Dock.Position
	}
	if c.Dock.Size < 24 || c.Dock.Size > 96 {
		w = append(w, fmt.Sprintf("dock.size = %d: want 24 to 96; using %d", c.Dock.Size, d.Dock.Size))
		c.Dock.Size = d.Dock.Size
	}
	if c.Notify.Banner < 2 || c.Notify.Banner > 60 {
		w = append(w, fmt.Sprintf("notifications.banner_seconds = %d: want 2 to 60; using %d", c.Notify.Banner, d.Notify.Banner))
		c.Notify.Banner = d.Notify.Banner
	}
	if strings.TrimSpace(c.Bar.Clock) == "" {
		w = append(w, fmt.Sprintf("bar.clock is empty; using %q", d.Bar.Clock))
		c.Bar.Clock = d.Bar.Clock
	}
	return w
}

var (
	colorRe     = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	kbOptionsRe = regexp.MustCompile(`^([A-Za-z0-9_:.+-]+(,[A-Za-z0-9_:.+-]+)*)?$`)
)

// ValidKbOptions reports whether s is a comma-separated list of XKB options
// (or empty). It also keeps anything but options out of the generated
// Hyprland file.
func ValidKbOptions(s string) bool { return kbOptionsRe.MatchString(s) }

// ValidColor reports whether s is a valid background.color: "#rrggbb" or
// empty for the theme's ground.
func ValidColor(s string) bool { return s == "" || colorRe.MatchString(s) }

// ImagePath is the wallpaper's path, "" for none.
func (c Config) ImagePath() string { return c.resolve(c.Background.Image) }

// ShotFolder is screenshot.folder as an absolute path, "" for not saving.
func (c Config) ShotFolder() string { return c.resolve(c.Screenshot.Folder) }

// RecordingFolder is screenshot.recordings as an absolute path, the home
// directory's Videos when empty.
func (c Config) RecordingFolder() string {
	if p := c.resolve(c.Screenshot.Recordings); p != "" {
		return p
	}
	return c.resolve("~/Videos")
}

// resolve expands "~/" and makes a path relative to the config directory
// absolute.
func (c Config) resolve(p string) string {
	switch {
	case p == "":
		return ""
	case p == "~" || strings.HasPrefix(p, "~/"):
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	case !filepath.IsAbs(p):
		return filepath.Join(c.Dir, p)
	}
	return p
}

// CSSPath is the user stylesheet's path, "" if none is configured.
func (c Config) CSSPath() string {
	return c.resolve(c.Style.CSS)
}

// TerminalCommand splits Launcher.Terminal into argv, nil if unset.
func (c Config) TerminalCommand() []string {
	return strings.Fields(c.Launcher.Terminal)
}
