package shell

import (
	"testing"

	"github.com/lyricat/inua/internal/system"
)

func TestVolumeChanged(t *testing.T) {
	on := system.AudioState{Available: true, Volume: 0.5}
	for _, c := range []struct {
		name     string
		old, new system.AudioState
		want     bool
	}{
		{"volume", on, system.AudioState{Available: true, Volume: 0.55}, true},
		{"mute", on, system.AudioState{Available: true, Volume: 0.5, Muted: true}, true},
		{"same", on, on, false},
		{"audio appears", system.AudioState{}, on, false},
		{"audio goes", on, system.AudioState{}, false},
	} {
		if got := VolumeChanged(c.old, c.new); got != c.want {
			t.Errorf("%s: VolumeChanged = %v, want %v", c.name, got, c.want)
		}
	}
}
