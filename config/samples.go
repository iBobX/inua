// Package samples embeds the bootstrap configuration in this directory, so
// `inua init` can write it without the repository. Paths in FS are relative
// to $XDG_CONFIG_HOME: hypr/hyprland.conf, hypr/inua.conf (sourced by
// hyprland.conf, rewritten by inua), inua/config.toml, inua/style.css.
package samples

import "embed"

//go:embed hypr/hyprland.conf hypr/inua.conf inua/config.toml inua/style.css
var FS embed.FS

// Files lists FS in the order they are installed: inua.conf before the
// hyprland.conf that sources it, since Hyprland rejects a missing source.
var Files = []string{"hypr/inua.conf", "hypr/hyprland.conf", "inua/config.toml", "inua/style.css"}
