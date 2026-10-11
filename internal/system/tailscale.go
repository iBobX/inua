package system

import (
	"context"
	"os"
	"os/exec"

	"github.com/lyricat/inua/internal/apps/tailscale"
)

// TailscaleState is what the control center shows of Tailscale (the VPN).
type TailscaleState struct {
	Installed bool   // tailscale is installed
	Running   bool   // tailscaled answers
	Backend   string // its state: "Running", "Stopped", "NeedsLogin" …
	Online    int    // devices online in the tailnet
	ExitNode  string // the device traffic goes through, "" for none
}

// Tailscale reads and changes Tailscale through tailscaled's local API. It
// is read when wanted (the control center polls while open), not followed.
type Tailscale struct{ c *tailscale.Client }

func NewTailscale(socket string) *Tailscale { return &Tailscale{tailscale.NewClient(socket)} }

// Read reads the state now.
func (t *Tailscale) Read(ctx context.Context) TailscaleState {
	var s TailscaleState
	if _, err := exec.LookPath("tailscale"); err == nil {
		s.Installed = true
	} else if _, err := os.Stat(t.c.Socket); err == nil {
		s.Installed = true
	}
	st, err := t.c.Status(ctx)
	if err != nil {
		return s
	}
	s.Installed, s.Running, s.Backend, s.Online = true, true, st.BackendState, st.Online()
	for _, p := range st.Peer {
		if p.ExitNode {
			s.ExitNode = p.Name()
		}
	}
	return s
}

// SetRunning connects or disconnects this device. It fails with
// tailscale.ErrPermission until the user is tailscaled's operator.
func (t *Tailscale) SetRunning(ctx context.Context, on bool) error {
	_, err := t.c.EditPrefs(ctx, tailscale.Edit{WantRunning: on, WantRunningSet: true})
	return err
}
