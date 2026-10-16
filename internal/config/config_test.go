package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/i18n"
)

var update = flag.Bool("update", false, "rewrite the generated sample config")

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func withDir(c Config, dir string) Config {
	c.Dir = dir
	return c
}

func TestMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "none.toml")
	c, w, err := Load(path)
	if err != nil || len(w) != 0 {
		t.Fatalf("err=%v warnings=%v", err, w)
	}
	if !reflect.DeepEqual(c, withDir(Default(), filepath.Dir(path))) {
		t.Errorf("got %+v", c)
	}
}

// The sample in the repository documents every setting at its default; it
// must parse cleanly and change nothing.
func TestSampleIsDefaults(t *testing.T) {
	path := "../../config/inua/config.toml"
	c, w, err := Load(path)
	if err != nil || len(w) != 0 {
		t.Fatalf("err=%v warnings=%v", err, w)
	}
	if want := withDir(Default(), filepath.Dir(path)); !reflect.DeepEqual(c, want) {
		t.Errorf("sample differs from Default():\n got %+v\nwant %+v", c, want)
	}
}

func TestPartialOverride(t *testing.T) {
	path := write(t, `
[bar]
clock = "%H:%M:%S"
workspaces = "all"
tray = false

[launcher]
terminal = "foot --app-id term"
`)
	c, w, err := Load(path)
	if err != nil || len(w) != 0 {
		t.Fatalf("err=%v warnings=%v", err, w)
	}
	want := withDir(Default(), filepath.Dir(path))
	want.Bar.Clock, want.Bar.Workspaces, want.Bar.Tray = "%H:%M:%S", "all", false
	want.Launcher.Terminal = "foot --app-id term"
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got %+v\nwant %+v", c, want)
	}
	if got := c.TerminalCommand(); !reflect.DeepEqual(got, []string{"foot", "--app-id", "term"}) {
		t.Errorf("terminal = %q", got)
	}
}

func TestBadValuesFallBack(t *testing.T) {
	path := write(t, `
[bar]
workspaces = "every"
clock = "  "
colour = "red"

[launcher]
mode = "commands"

[style]
theme = "neon"

[extra]
x = 1
`)
	c, w, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	d := Default()
	if c.Bar.Workspaces != d.Bar.Workspaces || c.Launcher.Mode != d.Launcher.Mode || c.Bar.Clock != d.Bar.Clock || c.Style.Theme != d.Style.Theme {
		t.Errorf("not reset: %+v", c)
	}
	all := strings.Join(w, "\n")
	for _, s := range []string{"unknown setting bar.colour", "unknown setting extra", "bar.workspaces", "launcher.mode", "bar.clock", "style.theme"} {
		if !strings.Contains(all, s) {
			t.Errorf("no warning about %s in:\n%s", s, all)
		}
	}
}

func TestSyntaxError(t *testing.T) {
	path := write(t, "[bar\nclock = 1")
	c, _, err := Load(path)
	if err == nil {
		t.Fatal("no error")
	}
	if !reflect.DeepEqual(c, withDir(Default(), filepath.Dir(path))) {
		t.Errorf("not the defaults: %+v", c)
	}
	// A wrong type is a decode error too, and also leaves only defaults.
	path = write(t, "[bar]\nclock = \"%H\"\ntitle = \"yes\"")
	c, _, err = Load(path)
	if err == nil || c.Bar.Clock != Default().Bar.Clock {
		t.Errorf("err=%v clock=%q", err, c.Bar.Clock)
	}
}

func TestCSSPath(t *testing.T) {
	c := Default()
	c.Dir = "/home/u/.config/inua"
	if got := c.CSSPath(); got != "/home/u/.config/inua/style.css" {
		t.Errorf("relative: %q", got)
	}
	c.Style.CSS = "/etc/inua.css"
	if got := c.CSSPath(); got != "/etc/inua.css" {
		t.Errorf("absolute: %q", got)
	}
	c.Style.CSS = ""
	if got := c.CSSPath(); got != "" {
		t.Errorf("none: %q", got)
	}
}

func TestStrftime(t *testing.T) {
	ts := time.Date(2026, time.October, 9, 7, 5, 3, 0, time.FixedZone("JST", 9*3600))
	for _, c := range []struct{ format, want string }{
		{"%a %b %-d   %H:%M", "Fri Oct 9   07:05"},
		{"%A, %B %d %Y", "Friday, October 09 2026"},
		{"%e|%-e", " 9|9"},
		{"%I:%M %p", "07:05 AM"},
		{"%-H:%M:%S", "7:05:03"},
		{"%F %T %R", "2026-10-09 07:05:03 07:05"},
		{"%m/%-m %y %j", "10/10 26 282"},
		{"%Z %z", "JST +0900"},
		{"100%% %q %-q end%", "100% %q %-q end%"},
		{"%Y年%-m月%-d日 %H:%M", "2026年10月9日 07:05"},
	} {
		if got := Strftime(c.format, ts); got != c.want {
			t.Errorf("Strftime(%q) = %q, want %q", c.format, got, c.want)
		}
	}
	noon := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	midnight := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := Strftime("%I %p|", noon) + Strftime("%I %p", midnight); got != "12 PM|12 AM" {
		t.Errorf("12-hour clock: %q", got)
	}
	if HasSeconds("%H:%M") || !HasSeconds("%H:%M:%S") || !HasSeconds("%T") {
		t.Error("HasSeconds")
	}
}

// Every theme the setting accepts must exist in ui/themes.
func TestThemesExist(t *testing.T) {
	for _, name := range Themes {
		if _, err := os.Stat(filepath.Join("../../ui/themes", name+".css")); err != nil {
			t.Errorf("theme %s: %v", name, err)
		}
	}
	files, _ := filepath.Glob("../../ui/themes/*.css")
	if len(files) != len(Themes) {
		t.Errorf("%d theme files, %d in Themes", len(files), len(Themes))
	}
}

// The sample is generated: config/inua/config.toml == Format(Default()).
// After changing a setting or the template, regenerate it with
//
//	go test ./internal/config -run TestSampleIsFormat -update
func TestSampleIsFormat(t *testing.T) {
	path := "../../config/inua/config.toml"
	want := Format(Default())
	if *update {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := os.ReadFile(path); string(got) != want {
		t.Errorf("%s is not Format(Default()); run with -update", path)
	}
}

func TestFormatRoundTrip(t *testing.T) {
	c := Default()
	c.Bar.Clock = `%H:%M "now"`
	c.Bar.Tray = false
	c.Launcher.Terminal = "foot"
	c.Control.Lock = `swaylock -c '000000'`
	c.Control.Screenshot = "grim - | wl-copy\tx"
	c.Style.Theme = "suimen"
	c.Background.Image = "/usr/share/backgrounds/a b.png"
	c.Background.Fit = "tile"
	c.Background.Color = "#102030"
	c.Assistant.ServeEnable = true
	c.Assistant.ServeListen = "127.0.0.1:9876"
	c.MCP = MCP{Enable: true, Policy: "allow"}
	path := filepath.Join(t.TempDir(), "inua", "config.toml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, w, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c.Dir = filepath.Dir(path)
	// The image does not exist here; that is the only warning.
	if len(w) != 1 || !strings.Contains(w[0], "background.image") {
		t.Errorf("warnings %v", w)
	}
	if !reflect.DeepEqual(got, c) {
		t.Errorf("round trip:\n got %+v\nwant %+v", got, c)
	}
}

func TestMCPPolicyValidation(t *testing.T) {
	c, w, _ := Load(write(t, "[mcp]\npolicy = \"everything\"\n"))
	if c.MCP.Policy != "ask" || !strings.Contains(strings.Join(w, "\n"), "mcp.policy") {
		t.Errorf("policy %q, warnings %q", c.MCP.Policy, w)
	}
}

func TestBackgroundValidation(t *testing.T) {
	path := write(t, "[background]\nfit = \"stretch\"\ncolor = \"red\"\nimage = \"~/no-such-file.png\"\n")
	c, w, _ := Load(path)
	if c.Background.Fit != "cover" || c.Background.Color != "" {
		t.Errorf("not reset: %+v", c.Background)
	}
	home, _ := os.UserHomeDir()
	if c.ImagePath() != filepath.Join(home, "no-such-file.png") {
		t.Errorf("image path %q", c.ImagePath())
	}
	if all := strings.Join(w, "\n"); !strings.Contains(all, "background.fit") || !strings.Contains(all, "background.color") || !strings.Contains(all, "background.image") {
		t.Errorf("warnings:\n%s", all)
	}
}

func TestInputRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	for _, opts := range []string{"caps:escape,ctrl:swap_lalt_lctl", ""} {
		c := Default()
		speed := 0.35
		c.Input = Input{KbOptions: &opts, TouchpadSpeed: &speed, TouchpadAccel: "flat"}
		if err := Save(path, c); err != nil {
			t.Fatal(err)
		}
		got, w, err := Load(path)
		if err != nil || len(w) != 0 {
			t.Fatalf("err=%v warnings=%v", err, w)
		}
		if !reflect.DeepEqual(got.Input, c.Input) {
			t.Errorf("round trip: got %+v, want %+v", got.Input, c.Input)
		}
	}
	// Unset settings stay unset: an empty kb_options is a setting.
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	got, _, _ := Load(path)
	if got.Input.KbOptions != nil || got.Input.TouchpadSpeed != nil {
		t.Errorf("unset settings came back set: %+v", got.Input)
	}
}

func TestInputValidation(t *testing.T) {
	c, w, err := Load(write(t, "[input]\nkb_options = \"caps:escape\\nexec = rm\"\ntouchpad_speed = 2.0\ntouchpad_accel = \"fast\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(w) != 3 {
		t.Errorf("want 3 warnings, got %v", w)
	}
	if c.Input != (Input{}) {
		t.Errorf("bad values should be left to hyprland.conf: %+v", c.Input)
	}
	for _, ok := range []string{"", "caps:escape", "ctrl:swap_lalt_lctl,ctrl:swap_ralt_rctl", "grp:alt_shift_toggle,lv3:ralt_switch"} {
		if !ValidKbOptions(ok) {
			t.Errorf("ValidKbOptions(%q) = false", ok)
		}
	}
	for _, bad := range []string{"caps:escape,", ",caps:escape", "caps escape", "a#b", "a\nb"} {
		if ValidKbOptions(bad) {
			t.Errorf("ValidKbOptions(%q) = true", bad)
		}
	}
}

func TestShortcuts(t *testing.T) {
	c := Default()
	c.Shortcuts = []Shortcut{
		{Action: "launcher", Keys: "SUPER+Space", Replaces: []string{"SUPER+Space", "ALT+F1"}},
		{Action: "shot-area", Keys: ""},
		{Action: "exec", Keys: "SUPER+SHIFT+Return", Command: `kitty --class "float"`, Name: "Floating terminal"},
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, w, err := Load(path)
	if err != nil || len(w) != 0 {
		t.Fatalf("err=%v warnings=%v", err, w)
	}
	if !reflect.DeepEqual(got.Shortcuts, c.Shortcuts) {
		t.Errorf("round trip:\n got %+v\nwant %+v", got.Shortcuts, c.Shortcuts)
	}

	bad, w, _ := Load(write(t, `[[shortcuts]]
action = "launcher"
keys = "SUPER+Space"

[[shortcuts]]
action = "fly"
keys = "SUPER+F"

[[shortcuts]]
action = "exec"
keys = "SUPER+E"

[[shortcuts]]
action = "control"
keys = "SUPER+A, exec, rm -rf ~"
`))
	if len(bad.Shortcuts) != 1 || bad.Shortcuts[0].Action != "launcher" || len(w) != 3 {
		t.Errorf("kept %+v, warnings %v", bad.Shortcuts, w)
	}
}

func TestRenamedShotActions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	src := `[control]
screenshot = 'grim -g "$(slurp)" - | wl-copy'
lock = "pidof hyprlock || hyprlock"

[[shortcuts]]
action = "shot-area-file"
keys = "SUPER+Print"

[[shortcuts]]
action = "shot-area"
keys = "Print"

[[shortcuts]]
action = "shot-screen-file"
keys = "SHIFT+Print"
`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Control.Screenshot != Default().Control.Screenshot {
		t.Errorf("old screenshot command kept: %q", c.Control.Screenshot)
	}
	if c.Control.Lock != Default().Control.Lock {
		t.Errorf("old lock command kept: %q", c.Control.Lock)
	}
	if len(c.Shortcuts) != 2 || c.Shortcuts[0].Action != "shot-area" || c.Shortcuts[0].Keys != "SUPER+Print" || c.Shortcuts[1].Action != "shot-screen" {
		t.Errorf("shortcuts = %+v", c.Shortcuts)
	}
}

func TestQuicklinks(t *testing.T) {
	c, w, err := Load(write(t, `[launcher]
search = "ftp://nope"

[[launcher.quicklinks]]
keyword = "gh"
name = "GitHub"
url = "https://github.com/search?q=%s"

[[launcher.quicklinks]]
keyword = "gh"
name = "Again"
url = "https://example.com/?q=%s"

[[launcher.quicklinks]]
keyword = "bad one"
url = "https://example.com/?q=%s"

[[launcher.quicklinks]]
keyword = "nos"
url = "https://example.com/"
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Launcher.Quicklinks) != 1 || c.Launcher.Quicklinks[0].Name != "GitHub" {
		t.Errorf("quicklinks %+v", c.Launcher.Quicklinks)
	}
	if c.Launcher.Search != Default().Launcher.Search || len(w) != 4 {
		t.Errorf("search %q, warnings %q", c.Launcher.Search, w)
	}
	// None named: the defaults; an empty array: none.
	if c, _, _ := Load(write(t, "[launcher]\nmode = \"apps\"\n")); len(c.Launcher.Quicklinks) != len(DefaultQuicklinks) {
		t.Errorf("defaults: %d", len(c.Launcher.Quicklinks))
	}
	if c, _, _ := Load(write(t, "[launcher]\nquicklinks = []\n")); len(c.Launcher.Quicklinks) != 0 {
		t.Errorf("none: %+v", c.Launcher.Quicklinks)
	}
	// Written and read back alike.
	d := Default()
	d.Launcher.Quicklinks = nil
	if c, w, err := Load(write(t, Format(d))); err != nil || len(w) != 0 || len(c.Launcher.Quicklinks) != 0 {
		t.Errorf("empty round trip: %v %q %+v", err, w, c.Launcher.Quicklinks)
	}
}

func TestDockPosition(t *testing.T) {
	c, w, err := Load(write(t, "[dock]\nposition = \"left\"\n[style]\nedge_gaps = false\n"))
	if err != nil || len(w) != 0 || c.Dock.Position != "left" || c.Style.EdgeGaps {
		t.Fatalf("left: %q gaps=%v %v %v", c.Dock.Position, c.Style.EdgeGaps, w, err)
	}
	c, w, _ = Load(write(t, "[dock]\nposition = \"middle\"\n"))
	if len(w) != 1 || c.Dock.Position != "bottom" || !c.Style.EdgeGaps {
		t.Errorf("middle: %q gaps=%v %v", c.Dock.Position, c.Style.EdgeGaps, w)
	}
}

func TestAssistantProfiles(t *testing.T) {
	c, w, err := Load(write(t, `[assistant]
profile = "gone"
fallback = ["local", "work", "local", "nope"]

[[assistant.profiles]]
name = "work"
provider = "anthropic"
model = "claude-sonnet-5-5"
api_key_env = "ANTHROPIC_API_KEY" # from older configs: ignored, without a warning

[[assistant.profiles]]
name = "local"
provider = "openai_chat_compatible"
model = "qwen3"
endpoint = "http://127.0.0.1:11434/v1"

[[assistant.profiles]]
name = "default"
provider = "openai"
model = "gpt-5.2"

[[assistant.profiles]]
name = "work"
provider = "openai"
model = "gpt-5.2"

[[assistant.profiles]]
name = "odd"
provider = "nobody"
model = "x"
`))
	if err != nil {
		t.Fatal(err)
	}
	a := c.Assistant
	var names []string
	for _, p := range a.Profiles {
		names = append(names, p.Name)
	}
	// "default" is Mister Morph's and a name twice is left out; a provider
	// inua does not list is kept, for Mister Morph to know; the profile
	// falls back to the first; fallbacks are the others, once each.
	if fmt.Sprint(names) != "[work local odd]" || a.Profile != "work" || fmt.Sprint(a.Fallback) != "[local]" {
		t.Errorf("%+v", a)
	}
	if len(w) != 6 {
		t.Errorf("warnings %q", w)
	}
	// Saved, it stays.
	again, _, err := Load(write(t, Format(c)))
	if err != nil || len(again.Assistant.Profiles) != 3 || again.Assistant.Profiles[2].Provider != "nobody" {
		t.Errorf("saved again: %+v %v", again.Assistant.Profiles, err)
	}
}

// Abilities say what a profile is for; the profile that answers, its
// fallbacks and the one for quick choices have the abilities for it.
func TestAssistantAbilities(t *testing.T) {
	c, w, err := Load(write(t, `[assistant]
profile = "judge"
fallback = ["judge", "art", "chat"]
decision = "chat"

[[assistant.profiles]]
name = "judge"
provider = "openai"
model = "gpt-6-luna"
abilities = ["decision", "decision", "telepathy"]

[[assistant.profiles]]
name = "art"
provider = "openai"
model = "gpt-image-2"
abilities = ["image"]

[[assistant.profiles]]
name = "chat"
provider = "openai"
model = "gpt-5.2"
abilities = ["text"]

[[assistant.profiles]]
name = "both"
provider = "openai"
model = "gpt-5.2"
`))
	if err != nil {
		t.Fatal(err)
	}
	a := c.Assistant
	judge, _ := a.ProfileNamed("judge")
	if fmt.Sprint(judge.Abilities) != "[decision]" || judge.Can(AbilityText) || !judge.Can(AbilityDecision) {
		t.Errorf("judge %+v", judge)
	}
	if both, _ := a.ProfileNamed("both"); !both.Can(AbilityText) || !both.Can(AbilityDecision) || both.Can(AbilityImage) ||
		fmt.Sprint(both.AbilityList()) != "[text decision]" {
		t.Errorf("both %+v", both)
	}
	// judge only chooses and art only draws: chat answers, alone; chat
	// makes no quick choices, so the one that answers would, and as it
	// cannot, judge does.
	if a.Profile != "chat" || a.Fallback != nil || a.Decision != "" || a.DecisionProfile() != "judge" {
		t.Errorf("%+v", a)
	}
	if len(w) != 6 {
		t.Errorf("warnings %q", w)
	}
	if got := WithAbility(nil, AbilityText, false); fmt.Sprint(got) != "[decision]" {
		t.Errorf("without text: %q", got)
	}
	if got := WithAbility([]string{AbilityDecision}, AbilityText, true); got != nil {
		t.Errorf("both again: %q", got)
	}
	if got := WithAbility([]string{AbilityImage}, AbilityDecision, true); fmt.Sprint(got) != "[decision image]" {
		t.Errorf("image kept: %q", got)
	}
}

// The language is one of Languages, each with its catalog; another is
// reported and falls back to the environment's.
func TestLanguage(t *testing.T) {
	defer i18n.Use()
	for _, l := range Languages[2:] {
		if i18n.Use(l) != l {
			t.Errorf("no catalog for %s", l)
		}
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	for _, c := range []struct{ file, want string }{
		{"", "auto"},
		{`language = "ja"`, "ja"},
		{`language = "klingon"`, "auto"},
	} {
		if err := os.WriteFile(path, []byte(c.file+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, warnings, err := Load(path)
		if err != nil || cfg.Language != c.want || (c.file == `language = "klingon"`) != (len(warnings) > 0) {
			t.Errorf("%q: %q, %v, %v", c.file, cfg.Language, warnings, err)
		}
	}
	// What the settings window saves, it reads back.
	d := Default()
	d.Language = "zh_TW"
	if err := os.WriteFile(path, []byte(Format(d)), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg, _, _ := Load(path); cfg.Language != "zh_TW" {
		t.Errorf("saved zh_TW, read %q", cfg.Language)
	}
}

// Text is at 100% unless the file says otherwise; a size outside 80 to
// 150 is reported and falls back to 100.
func TestTextScale(t *testing.T) {
	for _, c := range []struct {
		file   string
		scale  int
		warned bool
	}{
		{"", 100, false},
		{"[style]\ntext_scale = 125\n", 125, false},
		{"[style]\ntext_scale = 115\n", 115, false},
		{"[style]\ntext_scale = 200\n", 100, true},
		{"[style]\ntext_scale = 0\n", 100, true},
	} {
		cfg, warnings, err := Load(write(t, c.file))
		if err != nil || cfg.Style.TextScale != c.scale || (len(warnings) > 0) != c.warned {
			t.Errorf("%q: %d %v %v", c.file, cfg.Style.TextScale, warnings, err)
		}
	}
}

// The clipboard history is kept by default; opacity and limit outside
// their ranges are reported and fall back to the defaults.
func TestClipboard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	for _, c := range []struct {
		file          string
		enable        bool
		opacity, keep int
		warned        bool
	}{
		{"", true, 84, 300, false},
		{"[clipboard]\nenable = false\nopacity = 50\nlimit = 1000\n", false, 50, 1000, false},
		{"[clipboard]\nopacity = 30\nlimit = 10\n", true, 84, 300, true},
	} {
		if err := os.WriteFile(path, []byte(c.file), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, warnings, err := Load(path)
		got := cfg.Clipboard
		if err != nil || got.Enable != c.enable || got.Opacity != c.opacity || got.Limit != c.keep || (len(warnings) > 0) != c.warned {
			t.Errorf("%q: %+v %v %v", c.file, got, warnings, err)
		}
	}
}

// TypeSafe only makes quick choices, whatever its abilities say; it
// cannot be the profile that answers.
func TestTypeSafe(t *testing.T) {
	c, w, err := Load(write(t, `[assistant]
profile = "ts"
decision = "ts"

[[assistant.profiles]]
name = "ts"
provider = "typesafe"
model = "jev-1.13.0"
abilities = ["text"]

[[assistant.profiles]]
name = "chat"
provider = "openai"
model = "gpt-5.2"
`))
	if err != nil {
		t.Fatal(err)
	}
	a := c.Assistant
	ts, _ := a.ProfileNamed("ts")
	if fmt.Sprint(ts.Abilities) != "[decision]" || ts.Can(AbilityText) || !ts.Can(AbilityDecision) ||
		a.Profile != "chat" || a.Decision != "ts" || len(w) != 2 {
		t.Errorf("%+v %q", a, w)
	}
}
