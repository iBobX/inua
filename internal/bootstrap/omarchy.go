package bootstrap

import (
	"bufio"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/hyprconf"
)

// omarchyModule is what an Omarchy hyprland.lua requires for Omarchy's
// defaults; their autostart starts Omarchy's shell (Quickshell) with
// hl.exec_cmd("omarchy-launch-shell"), from Omarchy's own files, which
// inua leaves alone.
const omarchyModule = "default.hypr.omarchy"

// OmarchyBlock goes before that require: hl is a plain table, so the
// command Omarchy's autostart gives hl.exec_cmd for its shell becomes
// inua-shell. Every other command runs as it was.
const OmarchyBlock = `-- inua: inua-shell in place of Omarchy's shell (Quickshell), which
-- Omarchy's autostart starts with omarchy-launch-shell. Delete this block,
-- and the line that loads inua at the end, to have Omarchy's shell back.
do
  local exec_cmd = hl.exec_cmd
  hl.exec_cmd = function(cmd, ...)
    if cmd == "omarchy-launch-shell" then return exec_cmd("inua-shell") end
    return exec_cmd(cmd, ...)
  end
end
`

// OmarchyShell reports whether the Lua config at path loads Omarchy's
// defaults, so starts Omarchy's shell, and does not start inua-shell.
func OmarchyShell(path string) bool {
	if !isLua(path) {
		return false
	}
	if _, err := FindShellStart(path); !errors.Is(err, ErrNoShell) {
		return false
	}
	return omarchyLine(path) >= 0
}

// omarchyLine is the index of the line of the config at path that
// requires Omarchy's defaults, or -1.
func omarchyLine(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return -1
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for i := 0; sc.Scan(); i++ {
		if luaRequired(strings.TrimSpace(stripLuaComment(sc.Text()))) == omarchyModule {
			return i
		}
	}
	return -1
}

// ReplaceOmarchyShell has the Omarchy config at path (OmarchyShell) start
// inua-shell in place of Omarchy's shell, in every session that reads it:
// OmarchyBlock before the line that requires Omarchy's defaults, and the
// line that loads inua.lua last, if nothing loads it, so inua's shortcuts
// replace Omarchy's. The old file is kept as path.bak.
func ReplaceOmarchyShell(path string) error {
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	at := omarchyLine(path)
	if at < 0 {
		return errors.New("does not load Omarchy's defaults")
	}
	lines := strings.SplitAfter(string(old), "\n")
	lines = slices.Insert(lines, at, OmarchyBlock+"\n")
	if _, _, err := FindSource(path, hyprconf.Target(path)); errors.Is(err, ErrNoSource) {
		if last := lines[len(lines)-1]; last != "" && !strings.HasSuffix(last, "\n") {
			lines[len(lines)-1] += "\n"
		}
		lines = append(lines, "\n-- inua's keyboard, touchpad and shortcuts (inua.lua), after Omarchy's\n"+hyprconf.LuaRequire+"\n")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path+".bak", old, info.Mode().Perm()); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "")), info.Mode().Perm())
}
