package hypr

import (
	"context"
	"encoding/json"
	"fmt"
)

// Tile tiles process pid's floating window: for an app that is a normal
// window though a window rule of an older inua.conf floats it. It says
// whether there was one to tile.
func (c *Client) Tile(ctx context.Context, pid int) (bool, error) {
	clients, err := c.Request(ctx, "j/clients")
	if err != nil {
		return false, err
	}
	addr, err := floatingOf(clients, pid)
	if err != nil || addr == "" {
		return false, err
	}
	return true, c.Dispatch(ctx, "settiled address:"+addr)
}

// floatingOf is the address of process pid's mapped floating window, ""
// when its window is not floating.
func floatingOf(clientsJSON []byte, pid int) (string, error) {
	var clients []struct {
		Address  string `json:"address"`
		Pid      int    `json:"pid"`
		Mapped   bool   `json:"mapped"`
		Floating bool   `json:"floating"`
	}
	if err := json.Unmarshal(clientsJSON, &clients); err != nil {
		return "", fmt.Errorf("clients: %w", err)
	}
	found := false
	for _, cl := range clients {
		if cl.Pid != pid || !cl.Mapped {
			continue
		}
		found = true
		if cl.Floating {
			return NormalizeAddress(cl.Address), nil
		}
	}
	if !found {
		return "", fmt.Errorf("no window of process %d", pid)
	}
	return "", nil
}
