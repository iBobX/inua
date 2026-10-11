package sni

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// MenuEntry is one visible com.canonical.dbusmenu item.
type MenuEntry struct {
	ID        int32
	Label     string // mnemonic underscores removed
	Enabled   bool
	Separator bool
	Toggle    string // "", "checkmark" or "radio"
	Checked   bool
	IconName  string
	Children  []MenuEntry
}

// Menu reads an item's current menu. Menus are read when they are about to be
// shown rather than tracked, so LayoutUpdated signals are not needed.
func (h *Host) Menu(key string) ([]MenuEntry, error) {
	e, err := h.lookup(key)
	if err != nil {
		return nil, err
	}
	if e.menu == "" || e.menu == "/" {
		return nil, fmt.Errorf("sni: %s has no menu", key)
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	obj := h.conn.Object(e.service, e.menu)
	// Lets apps that build menus lazily fill this one in; failures are fine.
	obj.CallWithContext(ctx, menuIface+".AboutToShow", 0, int32(0))

	call := obj.CallWithContext(ctx, menuIface+".GetLayout", 0, int32(0), int32(-1), []string{})
	if call.Err != nil {
		return nil, call.Err
	}
	if len(call.Body) != 2 {
		return nil, fmt.Errorf("sni: GetLayout returned %d values", len(call.Body))
	}
	root, ok := call.Body[1].([]any)
	if !ok {
		return nil, fmt.Errorf("sni: GetLayout layout has type %T", call.Body[1])
	}
	n, ok := decodeNode(root)
	if !ok {
		return nil, fmt.Errorf("sni: malformed menu layout")
	}
	return n.Children, nil
}

// MenuClicked activates a menu entry.
func (h *Host) MenuClicked(key string, id int32) error {
	e, err := h.lookup(key)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.conn.Object(e.service, e.menu).CallWithContext(ctx, menuIface+".Event", 0,
		id, "clicked", dbus.MakeVariant(int32(0)), uint32(time.Now().Unix())).Err
}

// decodeNode turns a (ia{sv}av) layout node into an entry, dropping hidden
// children.
func decodeNode(raw []any) (MenuEntry, bool) {
	if len(raw) != 3 {
		return MenuEntry{}, false
	}
	id, ok1 := raw[0].(int32)
	props, ok2 := raw[1].(map[string]dbus.Variant)
	children, ok3 := raw[2].([]dbus.Variant)
	if !ok1 || !ok2 || !ok3 {
		return MenuEntry{}, false
	}
	m := MenuEntry{ID: id, Enabled: true}
	if v, ok := props["label"].Value().(string); ok {
		m.Label = stripMnemonic(v)
	}
	if v, ok := props["enabled"].Value().(bool); ok {
		m.Enabled = v
	}
	if v, ok := props["type"].Value().(string); ok && v == "separator" {
		m.Separator = true
	}
	if v, ok := props["toggle-type"].Value().(string); ok {
		m.Toggle = v
	}
	if v, ok := props["toggle-state"].Value().(int32); ok {
		m.Checked = v == 1
	}
	if v, ok := props["icon-name"].Value().(string); ok {
		m.IconName = v
	}
	for _, c := range children {
		raw, ok := c.Value().([]any)
		if !ok {
			continue
		}
		child, ok := decodeNode(raw)
		if !ok {
			continue
		}
		if visible(c) {
			m.Children = append(m.Children, child)
		}
	}
	return m, true
}

func visible(c dbus.Variant) bool {
	raw, _ := c.Value().([]any)
	if len(raw) != 3 {
		return false
	}
	props, _ := raw[1].(map[string]dbus.Variant)
	if v, ok := props["visible"].Value().(bool); ok {
		return v
	}
	return true
}

// stripMnemonic removes GTK-style mnemonic markers: "_x" becomes "x" and
// "__" a literal underscore.
func stripMnemonic(s string) string {
	if !strings.Contains(s, "_") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			if i+1 < len(s) && s[i+1] == '_' {
				b.WriteByte('_')
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
