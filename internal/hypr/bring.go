package hypr

import (
	"context"
	"encoding/json"
	"fmt"
)

// BringHere moves the window of process pid to the active workspace, if
// it is elsewhere, and focuses it: for an app started again while its one
// window is open on another workspace.
func (c *Client) BringHere(ctx context.Context, pid int) error {
	active, err := c.Request(ctx, "j/activeworkspace")
	if err != nil {
		return err
	}
	clients, err := c.Request(ctx, "j/clients")
	if err != nil {
		return err
	}
	cmds, err := bringHere(active, clients, pid)
	if err != nil {
		return err
	}
	for _, cmd := range cmds {
		if err := c.Dispatch(ctx, cmd); err != nil {
			return err
		}
	}
	return nil
}

// bringHere is the dispatches BringHere sends, from Hyprland's replies.
func bringHere(activeJSON, clientsJSON []byte, pid int) ([]string, error) {
	var active struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(activeJSON, &active); err != nil {
		return nil, fmt.Errorf("activeworkspace: %w", err)
	}
	var clients []struct {
		Address   string `json:"address"`
		Pid       int    `json:"pid"`
		Mapped    bool   `json:"mapped"`
		Workspace struct {
			ID int `json:"id"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal(clientsJSON, &clients); err != nil {
		return nil, fmt.Errorf("clients: %w", err)
	}
	for _, cl := range clients {
		if cl.Pid != pid || !cl.Mapped {
			continue
		}
		addr := "address:" + NormalizeAddress(cl.Address)
		if cl.Workspace.ID == active.ID {
			return []string{"focuswindow " + addr}, nil
		}
		return []string{
			fmt.Sprintf("movetoworkspacesilent %d,%s", active.ID, addr),
			"focuswindow " + addr,
		}, nil
	}
	return nil, fmt.Errorf("no window of process %d", pid)
}
