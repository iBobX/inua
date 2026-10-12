package hypr

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Monitor mirrors the fields of Quickshell's HyprlandMonitor.
type Monitor struct {
	ID                int
	Name              string
	Description       string
	X, Y              int
	Width, Height     int
	Scale             float64
	Focused           bool
	ActiveWorkspaceID int
	Raw               map[string]any // lastIpcObject
}

// Workspace mirrors the fields of Quickshell's HyprlandWorkspace.
type Workspace struct {
	ID            int
	Name          string
	Monitor       string // monitor name, "" if none
	Active        bool   // shown on its monitor
	Focused       bool   // active on the focused monitor
	HasFullscreen bool
	Urgent        bool
	Raw           map[string]any
}

// Toplevel mirrors the fields of Quickshell's HyprlandToplevel.
type Toplevel struct {
	Address     string // "0x..." form, always lower case
	PID         int
	Title       string
	Class       string
	WorkspaceID int
	Monitor     string
	Activated   bool
	Urgent      bool
	Raw         map[string]any
}

// Snapshot is a consistent view of compositor state. Slices are ordered:
// monitors and workspaces by id, toplevels in Hyprland's client order.
type Snapshot struct {
	Monitors       []Monitor
	Workspaces     []Workspace
	Toplevels      []Toplevel
	ActiveToplevel string // address, "" if none
	FocusedMonitor string // name, "" if none
}

// NormalizeAddress turns "55d1c0" or "0x55D1C0" into "0x55d1c0".
func NormalizeAddress(a string) string {
	a = strings.ToLower(strings.TrimSpace(a))
	if a == "" {
		return ""
	}
	if !strings.HasPrefix(a, "0x") {
		a = "0x" + a
	}
	return a
}

type rawMonitor struct {
	ID              int     `json:"id"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	X               int     `json:"x"`
	Y               int     `json:"y"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	Scale           float64 `json:"scale"`
	Focused         bool    `json:"focused"`
	Disabled        bool    `json:"disabled"`
	ActiveWorkspace struct {
		ID int `json:"id"`
	} `json:"activeWorkspace"`
	SpecialWorkspace struct {
		ID int `json:"id"`
	} `json:"specialWorkspace"`
}

type rawWorkspace struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Monitor       string `json:"monitor"`
	HasFullscreen bool   `json:"hasfullscreen"`
}

type rawClient struct {
	Address   string `json:"address"`
	PID       int    `json:"pid"`
	Mapped    bool   `json:"mapped"`
	Title     string `json:"title"`
	Class     string `json:"class"`
	Monitor   int    `json:"monitor"`
	Workspace struct {
		ID int `json:"id"`
	} `json:"workspace"`
}

// decodeList decodes a JSON array into typed values and keeps each element as
// a generic map for lastIpcObject.
func decodeList[T any](data []byte) ([]T, []map[string]any, error) {
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return nil, nil, err
	}
	typed := make([]T, len(raws))
	maps := make([]map[string]any, len(raws))
	for i, r := range raws {
		if err := json.Unmarshal(r, &typed[i]); err != nil {
			return nil, nil, err
		}
		if err := json.Unmarshal(r, &maps[i]); err != nil {
			return nil, nil, err
		}
	}
	return typed, maps, nil
}

// QueryResult holds the raw replies of one refresh.
type QueryResult struct {
	Monitors, Workspaces, Clients, ActiveWindow []byte
}

// Query fetches everything a snapshot needs. Requests are separate, so the
// result may straddle a change; the store re-queries on the next event.
func (c *Client) Query(ctx context.Context) (QueryResult, error) {
	var q QueryResult
	var err error
	for _, step := range []struct {
		cmd string
		dst *[]byte
	}{
		{"j/monitors", &q.Monitors},
		{"j/workspaces", &q.Workspaces},
		{"j/clients", &q.Clients},
		{"j/activewindow", &q.ActiveWindow},
	} {
		if *step.dst, err = c.Request(ctx, step.cmd); err != nil {
			return q, fmt.Errorf("%s: %w", step.cmd, err)
		}
	}
	return q, nil
}

// BuildSnapshot derives a snapshot from query replies. urgent holds toplevel
// addresses marked urgent by events since they were last activated.
func BuildSnapshot(q QueryResult, urgent map[string]bool) (Snapshot, error) {
	var s Snapshot
	mons, monMaps, err := decodeList[rawMonitor](q.Monitors)
	if err != nil {
		return s, fmt.Errorf("monitors: %w", err)
	}
	wss, wsMaps, err := decodeList[rawWorkspace](q.Workspaces)
	if err != nil {
		return s, fmt.Errorf("workspaces: %w", err)
	}
	clients, clientMaps, err := decodeList[rawClient](q.Clients)
	if err != nil {
		return s, fmt.Errorf("clients: %w", err)
	}
	var active struct {
		Address string `json:"address"`
	}
	// j/activewindow replies "{}" when nothing is focused.
	if len(q.ActiveWindow) > 0 {
		if err := json.Unmarshal(q.ActiveWindow, &active); err != nil {
			return s, fmt.Errorf("activewindow: %w", err)
		}
	}
	s.ActiveToplevel = NormalizeAddress(active.Address)

	monName := map[int]string{}
	shown := map[int]string{} // workspace id -> monitor name
	focusedMon := ""
	for i, m := range mons {
		if m.Disabled {
			continue
		}
		monName[m.ID] = m.Name
		shown[m.ActiveWorkspace.ID] = m.Name
		if m.SpecialWorkspace.ID != 0 {
			shown[m.SpecialWorkspace.ID] = m.Name
		}
		if m.Focused {
			focusedMon = m.Name
		}
		s.Monitors = append(s.Monitors, Monitor{
			ID: m.ID, Name: m.Name, Description: m.Description,
			X: m.X, Y: m.Y, Width: m.Width, Height: m.Height, Scale: m.Scale,
			Focused: m.Focused, ActiveWorkspaceID: m.ActiveWorkspace.ID,
			Raw: monMaps[i],
		})
	}
	s.FocusedMonitor = focusedMon

	urgentWs := map[int]bool{}
	for i, c := range clients {
		if !c.Mapped {
			continue
		}
		addr := NormalizeAddress(c.Address)
		u := urgent[addr]
		if u {
			urgentWs[c.Workspace.ID] = true
		}
		s.Toplevels = append(s.Toplevels, Toplevel{
			Address: addr, PID: c.PID, Title: c.Title, Class: c.Class,
			WorkspaceID: c.Workspace.ID, Monitor: monName[c.Monitor],
			Activated: addr == s.ActiveToplevel, Urgent: u,
			Raw: clientMaps[i],
		})
	}

	for i, w := range wss {
		mon, isShown := shown[w.ID]
		s.Workspaces = append(s.Workspaces, Workspace{
			ID: w.ID, Name: w.Name, Monitor: w.Monitor,
			Active:        isShown,
			Focused:       isShown && mon == focusedMon && focusedMon != "",
			HasFullscreen: w.HasFullscreen,
			Urgent:        urgentWs[w.ID],
			Raw:           wsMaps[i],
		})
	}

	sort.Slice(s.Monitors, func(i, j int) bool { return s.Monitors[i].ID < s.Monitors[j].ID })
	sort.Slice(s.Workspaces, func(i, j int) bool { return s.Workspaces[i].ID < s.Workspaces[j].ID })
	return s, nil
}
