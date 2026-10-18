package hypr

import (
	"context"
	"slices"
	"testing"
)

func TestSyncVirtualScreen(t *testing.T) {
	const (
		create = "output create headless " + VirtualScreen
		remove = "output remove " + VirtualScreen
		size   = "keyword monitor " + VirtualScreen + ",1920x1080@60,auto,1"
	)
	for _, tc := range []struct {
		name     string
		mode     string
		monitors string
		want     []string
	}{
		{"no monitor: made at its mode", "1920x1080@60",
			`[{"name":"FALLBACK","width":1920,"height":1080,"refreshRate":60}]`, []string{create, size}},
		{"already there", "1920x1080@60",
			`[{"name":"INUA-VIRTUAL","width":1920,"height":1080,"refreshRate":60.0}]`, nil},
		{"another size: resized", "1920x1080@60",
			`[{"name":"INUA-VIRTUAL","width":1280,"height":720,"refreshRate":60}]`, []string{size}},
		{"a monitor plugged in: removed", "1920x1080@60",
			`[{"name":"INUA-VIRTUAL","width":1920,"height":1080,"refreshRate":60},{"name":"HDMI-A-1","width":2560,"height":1440,"refreshRate":144}]`, []string{remove}},
		{"a monitor turned off still counts", "1920x1080@60",
			`[{"name":"eDP-1","disabled":true}]`, nil},
		{"setting off: removed", "",
			`[{"name":"INUA-VIRTUAL","width":1920,"height":1080,"refreshRate":60}]`, []string{remove}},
		{"setting off, none there", "", `[]`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHyprland(t)
			f.set("j/monitors all", tc.monitors)
			for _, cmd := range []string{create, remove, size} {
				f.set(cmd, "ok")
			}
			changed, err := NewClient(f.paths).SyncVirtualScreen(context.Background(), tc.mode)
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			got := slices.DeleteFunc(slices.Clone(f.requests), func(r string) bool { return r == "j/monitors all" })
			f.mu.Unlock()
			if !slices.Equal(got, tc.want) {
				t.Fatalf("requests = %q, want %q", got, tc.want)
			}
			if changed != (len(tc.want) > 0) {
				t.Fatalf("changed = %v with requests %q", changed, got)
			}
		})
	}
}

func TestSyncVirtualScreenRefused(t *testing.T) {
	f := newFakeHyprland(t)
	f.set("j/monitors all", `[]`)
	f.set("output create headless "+VirtualScreen, "error: no headless backend")
	if _, err := NewClient(f.paths).SyncVirtualScreen(context.Background(), "1920x1080@60"); err == nil {
		t.Fatal("Hyprland refused the output; want an error")
	}
	if _, err := NewClient(f.paths).SyncVirtualScreen(context.Background(), "1920x1080"); err == nil {
		t.Fatal("a mode without its rate; want an error")
	}
}

func TestParseMode(t *testing.T) {
	if w, h, r, ok := ParseMode("2560x1440@60"); !ok || w != 2560 || h != 1440 || r != 60 {
		t.Fatalf("ParseMode = %d %d %d %v", w, h, r, ok)
	}
	for _, bad := range []string{"", "1920x1080", "1920x1080@0", "x1080@60", "1920x1080@60,auto"} {
		if _, _, _, ok := ParseMode(bad); ok {
			t.Errorf("ParseMode(%q) accepted", bad)
		}
	}
}
