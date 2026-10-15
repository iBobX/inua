package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/hyprconf"
)

// omarchyLua is the start of Omarchy's ~/.config/hypr/hyprland.lua.
const omarchyLua = `-- Omarchy's bootstrap keeps path setup out of this user config.
dofile((os.getenv("OMARCHY_PATH") or "/usr/share/omarchy") .. "/default/hypr/bootstrap.lua")

-- Load Omarchy defaults.
require("default.hypr.omarchy")

require("hypr.bindings")
require("hypr.autostart")`

func TestReplaceOmarchyShell(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hyprland.lua")
	os.WriteFile(path, []byte(omarchyLua), 0o600)
	if !OmarchyShell(path) {
		t.Fatal("Omarchy's config not recognised")
	}
	if err := ReplaceOmarchyShell(path); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path+".bak"); got != omarchyLua {
		t.Errorf("backup: %q", got)
	}
	got := read(t, path)
	block, require := strings.Index(got, OmarchyBlock), strings.Index(got, `require("default.hypr.omarchy")`)
	if block < 0 || block > require {
		t.Errorf("the block is not before Omarchy's defaults:\n%s", got)
	}
	if !strings.HasSuffix(got, `require("hypr.autostart")`+"\n\n-- inua's keyboard, touchpad and shortcuts (inua.lua), after Omarchy's\n"+hyprconf.LuaRequire+"\n") {
		t.Errorf("inua.lua is not loaded last:\n%s", got)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode())
	}
	// It now starts inua-shell; run again, nothing is left to replace.
	if where, err := FindShellStart(path); err != nil {
		t.Errorf("does not start inua-shell: %v", err)
	} else if !strings.HasPrefix(where, path+":") {
		t.Errorf("starts it at %s", where)
	}
	if OmarchyShell(path) {
		t.Error("still Omarchy's shell after replacing it")
	}
}

func TestReplaceOmarchyShellLoadsInuaOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hyprland.lua")
	content := omarchyLua + "\n" + hyprconf.LuaRequire + "\n"
	os.WriteFile(path, []byte(content), 0o644)
	os.WriteFile(filepath.Join(dir, hyprconf.LuaName), nil, 0o644)
	if err := ReplaceOmarchyShell(path); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); strings.Count(got, hyprconf.LuaRequire) != 1 {
		t.Errorf("inua.lua loaded twice:\n%s", got)
	}
}

func TestOmarchyShellOthers(t *testing.T) {
	dir := t.TempDir()
	conf := func(name, content string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(content), 0o644)
		return p
	}
	for name, content := range map[string]string{
		// Already starts inua-shell itself.
		"inua.lua": omarchyLua + "\nhl.on(\"hyprland.start\", function() hl.exec_cmd(\"inua-shell\") end)\n",
		// Commented out, or not Omarchy's.
		"commented.lua": "-- require(\"default.hypr.omarchy\")\n",
		"plain.lua":     "require(\"hypr.bindings\")\n",
		// Old Omarchy, hyprlang: its shell is waybar, started elsewhere.
		"hyprland.conf": "source = ~/.local/share/omarchy/default/hypr/autostart.conf\n",
	} {
		if OmarchyShell(conf(name, content)) {
			t.Errorf("%s: taken for Omarchy's shell", name)
		}
	}
	if OmarchyShell(filepath.Join(dir, "missing.lua")) {
		t.Error("missing file taken for Omarchy's shell")
	}
}
