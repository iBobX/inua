package portalconf

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SharePicker is the entry of inua portals that is not an interface: the
// screen's capture stays xdg-desktop-portal-hyprland's, which runs
// inua-share-picker to ask what to share (screencopy:custom_picker_binary
// in ~/.config/hypr/xdph.conf).
const SharePicker = "SharePicker"

const sharePickerProgram = "inua-share-picker"

var pickerLine = regexp.MustCompile(`(?m)^[ \t]*custom_picker_binary[ \t]*=[ \t]*(.*)$`)

const xdphOwn = "# Written by inua portals (docs/portals.md).\nscreencopy {\n    custom_picker_binary = %s\n}\n"

// sharePickerOn is whether xdph's config names inua's picker.
func (d Dirs) sharePickerOn() bool {
	b, err := os.ReadFile(d.Xdph)
	if err != nil {
		return false
	}
	m := pickerLine.FindSubmatch(b)
	return m != nil && filepath.Base(strings.TrimSpace(string(m[1]))) == sharePickerProgram
}

// setSharePicker names inua's picker in xdph's config, or takes it out,
// keeping the rest of the file.
func (d Dirs) setSharePicker(on bool) error {
	if d.Xdph == "" {
		return errors.New("no config of xdg-desktop-portal-hyprland to write")
	}
	b, err := os.ReadFile(d.Xdph)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(b)
	if !on {
		if !d.sharePickerOn() {
			return nil
		}
		exe := strings.TrimSpace(pickerLine.FindStringSubmatch(text)[1])
		if strings.HasPrefix(text, "# Written by inua portals") && text == strings.Replace(xdphOwn, "%s", exe, 1) {
			return os.Remove(d.Xdph) // only ever inua's
		}
		return writeIfChanged(d.Xdph, pickerLine.ReplaceAllString(text, ""))
	}
	exe, err := d.Exec(sharePickerProgram)
	if err != nil {
		return err
	}
	switch {
	case text == "":
		text = strings.Replace(xdphOwn, "%s", exe, 1)
	case pickerLine.MatchString(text):
		text = pickerLine.ReplaceAllString(text, "    custom_picker_binary = "+exe)
	case regexp.MustCompile(`(?m)^[ \t]*screencopy[ \t]*\{`).MatchString(text):
		text = regexp.MustCompile(`(?m)^([ \t]*screencopy[ \t]*\{[^\n]*\n)`).ReplaceAllString(text, "${1}    custom_picker_binary = "+exe+"\n")
	default:
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "\nscreencopy {\n    custom_picker_binary = " + exe + "\n}\n"
	}
	if err := os.MkdirAll(filepath.Dir(d.Xdph), 0o755); err != nil {
		return err
	}
	return writeIfChanged(d.Xdph, text)
}
