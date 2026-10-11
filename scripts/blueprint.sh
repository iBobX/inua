#!/bin/sh
# Compile every Blueprint file (*.blp) in the given directories to GtkBuilder
# XML (*.ui) next to it. The .ui files are committed, so only people who edit
# .blp files need blueprint-compiler (sudo apt install blueprint-compiler).
# BLUEPRINT overrides the compiler command.
set -eu
BLUEPRINT=${BLUEPRINT:-blueprint-compiler}
for dir in "$@"; do
	for blp in "$dir"/*.blp; do
		$BLUEPRINT compile --output "${blp%.blp}.ui" "$blp"
	done
done
