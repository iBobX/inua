package bootstrap

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	samples "github.com/lyricat/inua/config"
)

var testFS = fstest.MapFS{
	"hypr/hyprland.conf": {Data: []byte("monitor = , preferred, auto, auto\n")},
	"inua/config.toml":   {Data: []byte("[bar]\n")},
}

var names = []string{"hypr/hyprland.conf", "inua/config.toml"}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func outcomes(rs []Result) []Outcome {
	var out []Outcome
	for _, r := range rs {
		out = append(out, r.Outcome)
	}
	return out
}

func TestInstallFresh(t *testing.T) {
	dir := t.TempDir()
	rs, err := Install(testFS, names, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := outcomes(rs); len(got) != 2 || got[0] != Written || got[1] != Written {
		t.Fatalf("outcomes %v", got)
	}
	if got := read(t, filepath.Join(dir, "inua/config.toml")); got != "[bar]\n" {
		t.Errorf("content %q", got)
	}
	// Running again finds them current.
	rs, _ = Install(testFS, names, dir, false)
	if got := outcomes(rs); got[0] != Same || got[1] != Same {
		t.Errorf("second run %v", got)
	}
}

func TestInstallKeepsUserFiles(t *testing.T) {
	dir := t.TempDir()
	mine := filepath.Join(dir, "hypr/hyprland.conf")
	os.MkdirAll(filepath.Dir(mine), 0o755)
	os.WriteFile(mine, []byte("# mine\n"), 0o644)

	rs, err := Install(testFS, names, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if rs[0].Outcome != Skipped || rs[1].Outcome != Written {
		t.Fatalf("outcomes %v", outcomes(rs))
	}
	if got := read(t, mine); got != "# mine\n" {
		t.Errorf("user file changed: %q", got)
	}

	// -force replaces it and keeps the old one.
	rs, err = Install(testFS, names, dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if rs[0].Outcome != Replaced || rs[0].Backup != mine+".bak" || rs[1].Outcome != Same {
		t.Fatalf("forced: %+v", rs)
	}
	if read(t, mine+".bak") != "# mine\n" || read(t, mine) != "monitor = , preferred, auto, auto\n" {
		t.Error("backup or replacement wrong")
	}
}

// The embedded samples are the files in config/, and Files names them all.
func TestEmbeddedSamples(t *testing.T) {
	for _, name := range samples.Files {
		embedded, err := samples.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if disk := read(t, filepath.Join("../../config", name)); disk != string(embedded) {
			t.Errorf("%s differs from config/%s", name, name)
		}
	}
	dir := t.TempDir()
	if _, err := Install(samples.FS, samples.Files, dir, false); err != nil {
		t.Fatal(err)
	}
}

func TestSamplesUseDefaultHyprlandConfig(t *testing.T) {
	dir := t.TempDir()
	hypr := filepath.Join(dir, "hypr/hyprland.conf")
	if _, err := Install(samples.FS, samples.Files, dir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hypr); err != nil {
		t.Fatalf("default Hyprland config missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "inua/hyprland.conf")); !os.IsNotExist(err) {
		t.Errorf("should not create a separate Hyprland config: %v", err)
	}
	if err := os.WriteFile(hypr, []byte("# existing Hyprland session\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(samples.FS, samples.Files, dir, false); err != nil {
		t.Fatal(err)
	}
	if got := read(t, hypr); got != "# existing Hyprland session\n" {
		t.Errorf("changed the existing session: %q", got)
	}
}

func TestFindShellStart(t *testing.T) {
	dir := t.TempDir()
	conf := func(name, content string) string {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
		return p
	}
	main := conf("plain.conf", "monitor = , preferred, auto, auto\nexec-once = inua-shell\n")
	if where, err := FindShellStart(main); err != nil || where != main+":2" {
		t.Errorf("plain: %q %v", where, err)
	}
	// Commented out, or another program: not started.
	for _, content := range []string{
		"# exec-once = inua-shell\n",
		"exec-once = waybar  # inua-shell later\n",
		"bind = SUPER, R, exec, inua-shellx\n",
		"",
	} {
		p := conf("none.conf", content)
		if _, err := FindShellStart(p); !errors.Is(err, ErrNoShell) {
			t.Errorf("%q: %v", content, err)
		}
	}
	// exec (not exec-once) with arguments counts.
	if _, err := FindShellStart(conf("exec.conf", "exec = inua-shell -n main\n")); err != nil {
		t.Errorf("exec: %v", err)
	}
	// Through source lines: relative, globbed, and a loop that ends.
	conf("parts/inua.conf", "exec-once = inua-shell\n")
	conf("parts/loop.conf", "source = ../sourcing.conf\n")
	src := conf("sourcing.conf", "source = parts/loop.conf\nsource = parts/*.conf\n")
	if where, err := FindShellStart(src); err != nil || !strings.HasSuffix(where, "parts/inua.conf:1") {
		t.Errorf("source: %q %v", where, err)
	}
	if _, err := FindShellStart(filepath.Join(dir, "missing.conf")); err == nil || errors.Is(err, ErrNoShell) {
		t.Errorf("missing file: %v", err)
	}
	// The sample starts it.
	if _, err := FindShellStart("../../config/hypr/hyprland.conf"); err != nil {
		t.Errorf("sample: %v", err)
	}
}

func TestFindSource(t *testing.T) {
	dir := t.TempDir()
	conf := func(name, content string) string {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
		return p
	}
	target := conf("inua.conf", "input {\n    kb_options = caps:escape\n}\n")
	// Last, after the settings it overrides: fine. inua.conf's own
	// kb_options is not an override.
	main := conf("last.conf", "input {\n    kb_options = ctrl:nocaps\n}\nsource = inua.conf\nbind = SUPER, Q, killactive\n")
	where, over, err := FindSource(main, target)
	if err != nil || where != main+":4" || len(over) != 0 {
		t.Errorf("last: %q %v %v", where, over, err)
	}
	// Settings after the source line win over inua's.
	main = conf("early.conf", "source = ./inua.conf\ninput {\n    kb_options = ctrl:nocaps\n    touchpad {\n    }\n}\ndevice {\n    name = x-touchpad\n    sensitivity = 0.5\n}\n")
	if _, over, err := FindSource(main, target); err != nil || strings.Join(over, " ") != main+":3 "+main+":9" {
		t.Errorf("early: %v %v", over, err)
	}
	// Through another sourced file, by a relative path.
	conf("parts/input.conf", "source = ../inua.conf\n")
	if where, _, err := FindSource(conf("nested.conf", "source = parts/input.conf\n"), target); err != nil || !strings.HasSuffix(where, "parts/input.conf:1") {
		t.Errorf("nested: %q %v", where, err)
	}
	for _, content := range []string{"", "# source = inua.conf\n", "source = other.conf\n"} {
		if _, _, err := FindSource(conf("none.conf", content), target); !errors.Is(err, ErrNoSource) {
			t.Errorf("%q: %v", content, err)
		}
	}
	// The sample sources inua.conf last, as the sample inua.conf is
	// installed next to it.
	t.Setenv("HOME", dir)
	if _, err := Install(samples.FS, samples.Files, filepath.Join(dir, ".config"), false); err != nil {
		t.Fatal(err)
	}
	where, over, err = FindSource(filepath.Join(dir, ".config/hypr/hyprland.conf"), filepath.Join(dir, ".config/hypr/inua.conf"))
	if err != nil || len(over) != 0 {
		t.Errorf("sample: %q %v %v", where, over, err)
	}
}

func TestSetPinentry(t *testing.T) {
	dir := t.TempDir()
	prog := "/home/u/.local/bin/inua-pinentry"
	// No file: written with the line.
	if changed, _, err := SetPinentry(dir, prog, false); err != nil || !changed {
		t.Fatalf("fresh: %v %v", changed, err)
	}
	if got := read(t, filepath.Join(dir, "gpg-agent.conf")); !strings.Contains(got, "pinentry-program "+prog+"\n") {
		t.Errorf("fresh file %q", got)
	}
	// Set already: nothing to do.
	if changed, _, _ := SetPinentry(dir, prog, false); changed {
		t.Error("rewrote a current file")
	}
	// Another pinentry: kept unless forced; the other lines stay.
	os.WriteFile(filepath.Join(dir, "gpg-agent.conf"), []byte("default-cache-ttl 600\npinentry-program /usr/bin/pinentry-qt\n"), 0o600)
	if changed, msg, _ := SetPinentry(dir, prog, false); changed || !strings.Contains(msg, "pinentry-qt") {
		t.Errorf("replaced another pinentry without -force: %q", msg)
	}
	if changed, _, _ := SetPinentry(dir, prog, true); !changed {
		t.Error("-force did not set it")
	}
	if got := read(t, filepath.Join(dir, "gpg-agent.conf")); got != "default-cache-ttl 600\npinentry-program "+prog+"\n" {
		t.Errorf("forced file %q", got)
	}
}
