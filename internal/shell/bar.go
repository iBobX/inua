package shell

import (
	"slices"

	"github.com/lyricat/inua/internal/hypr"
)

// knownMonitor reports whether Hyprland has a monitor with the connector
// name GDK gave a bar. Without layer-shell (a nested or foreign compositor)
// the names can differ; such a bar shows everything.
func knownMonitor(snap hypr.Snapshot, connector string) bool {
	return slices.ContainsFunc(snap.Monitors, func(m hypr.Monitor) bool { return m.Name == connector })
}

// BarWorkspaces lists the workspaces the bar on a monitor shows: the normal
// (positive id) workspaces on that monitor, or on all monitors, by id.
func BarWorkspaces(snap hypr.Snapshot, connector string, all bool) []hypr.Workspace {
	all = all || !knownMonitor(snap, connector)
	var out []hypr.Workspace
	for _, w := range snap.Workspaces {
		if w.ID > 0 && (all || w.Monitor == connector) {
			out = append(out, w)
		}
	}
	slices.SortFunc(out, func(a, b hypr.Workspace) int { return a.ID - b.ID })
	return out
}

// BarTitle is the window title the bar on a monitor shows: the focused
// window on the focused monitor, elsewhere the window last focused on that
// monitor's visible workspace.
func BarTitle(snap hypr.Snapshot, connector string) string {
	if !knownMonitor(snap, connector) || connector == snap.FocusedMonitor {
		for _, t := range snap.Toplevels {
			if t.Address == snap.ActiveToplevel {
				return t.Title
			}
		}
		return ""
	}
	ws := 0
	for _, m := range snap.Monitors {
		if m.Name == connector {
			ws = m.ActiveWorkspaceID
		}
	}
	title, best := "", -1
	for i, t := range snap.Toplevels {
		if t.WorkspaceID != ws {
			continue
		}
		if h := focusHistory(t, len(snap.Toplevels)+i); best < 0 || h < best {
			title, best = t.Title, h
		}
	}
	return title
}
