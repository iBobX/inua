// Package ui holds the shell's layouts: Blueprint sources (*.blp), the
// GtkBuilder files compiled from them (*.ui, committed), style.css, the
// colour themes it uses (themes/*.css, colour tokens only), and the
// Phosphor icons of style.icons = "phosphor" (icons/, from
// scripts/phosphor-icons.sh). The
// views in internal/ui load them by name; `inua-shell -ui-dir ui` reads this
// directory from disk instead of the embedded copy.
package ui

//go:generate sh ../scripts/blueprint.sh .

import "embed"

//go:embed *.ui style.css themes/*.css icons/*.svg
var FS embed.FS
