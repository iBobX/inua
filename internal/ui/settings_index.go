package ui

import (
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
)

// settingsIcons are the pages' icons in the settings index, by page.
var settingsIcons = map[string]string{
	"appearance":    "preferences-desktop-appearance-symbolic",
	"bar":           "user-desktop-symbolic",
	"launcher":      "system-run-symbolic",
	"apps":          "view-app-grid-symbolic",
	"ai":            "applications-science-symbolic",
	"notifications": "preferences-system-notifications-symbolic",
	"clipboard":     "edit-paste-symbolic",
	"keyring":       "dialog-password-symbolic",
	"shortcuts":     "preferences-desktop-keyboard-shortcuts-symbolic",
	"wifi":          "network-wireless-signal-excellent-symbolic",
	"bluetooth":     "bluetooth-active-symbolic",
	"displays":      "video-display-symbolic",
	"keyboard":      "input-keyboard-symbolic",
	"mouse":         "input-mouse-symbolic",
	"about":         "help-about-symbolic",
}

// buildSettingsIndex lists the pages beside them, each its icon and
// title, the page shown selected; choosing one shows it.
func (s *shellUI) buildSettingsIndex() {
	st := &s.settings
	for _, name := range settingsPages {
		child := st.pages.GetChildByName(name)
		if child == nil {
			continue
		}
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		img := gtk.NewImageFromIconName(settingsIcons[name])
		img.AddCssClass("index-icon")
		gtkx.Append(box, &img.Widget)
		label := gtk.NewLabel(st.pages.GetPage(child).GetTitle())
		label.SetXalign(0)
		gtkx.Append(box, &label.Widget)
		row := gtk.NewListBoxRow()
		row.SetName(name)
		row.SetChild(&box.Widget)
		st.index.Append(&row.Widget)
	}
	s.onSettingsIndex = func(_ gtk.ListBox, ptr uintptr) {
		if ptr == 0 {
			return
		}
		if i := int(gtk.ListBoxRowNewFromInternalPtr(ptr).GetIndex()); i >= 0 && i < len(settingsPages) {
			if st.pages.GetVisibleChildName() != settingsPages[i] {
				st.pages.SetVisibleChildName(settingsPages[i])
			}
		}
	}
	st.index.ConnectRowSelected(&s.onSettingsIndex)
	s.selectSettingsIndex()
}

// selectSettingsIndex selects the page shown in the index.
func (s *shellUI) selectSettingsIndex() {
	st := &s.settings
	name := st.pages.GetVisibleChildName()
	for i, p := range settingsPages {
		if p == name {
			if row := st.index.GetRowAtIndex(int32(i)); row != nil {
				st.index.SelectRow(row)
			}
		}
	}
}
