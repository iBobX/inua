# Repository Guidelines

## Project Structure

inua is a Hyprland desktop shell written in Go and GTK4. Read `README.md` and `docs/architecture.md` first.

- `cmd/`: entry points for the shell, the command line, the light apps, the games, the assistant and the keyring service.
- `internal/shell`, `internal/apps`, `internal/games`: state and logic without GTK; their views are in `internal/ui`, `internal/appui` and `internal/gamesui`.
- `internal/hypr`, `internal/system` and others: Hyprland IPC, D-Bus and system services.
- `ui/`: Blueprint layouts, the generated `.ui` files, CSS, themes and icons.
- `config/`: sample configuration and desktop entries; `scripts/`: build and session tools; `website/`: the static website.
- Tests live next to the Go sources, named `*_test.go`.

## Build and Development Commands

Use Go 1.26 or later; running the UI needs Hyprland, GTK4 and gtk4-layer-shell.

- `make build`: builds the programs with `CGO_ENABLED=0` into `bin/`.
- `make test`: runs the Python session script tests, `go vet` and the Go tests with the race detector; needs a C compiler.
- `go test ./internal/shell`: checks one package only.
- `make generate`: compiles `.blp` into `.ui` with `blueprint-compiler`; after changing a layout, commit both.
- `make i18n`: gathers the translatable text into `po/inua.pot` and updates the catalogs in `internal/i18n/locales`.
- `make nested`: runs in a nested Hyprland window; `make run`: runs in the current Hyprland session.
- `bin/inua-shell -ui-dir ui`: reads layouts and styles from disk.

## Code Style and Architecture

Format Go with `gofmt` and follow standard naming: exported identifiers start with a capital letter, package names are short and lower case. Blueprint and CSS use two-space indentation, and colours refer to theme tokens.

Services and state must not import GTK. GTK calls run on the main thread; background results come back through the existing `gtkx.Post` or `gtkx.Call`. Windows, workspaces and focus follow the compositor's state. Implement the needed behaviour directly; do not add wrappers that only rename, or duplicate state.

Text the user reads is translatable: in Go through `internal/i18n` (`i18n.T`, `i18n.Tf`, `i18n.Nf`, `i18n.M`), in Blueprint as `_("…")`; run `make i18n` and commit `po/inua.pot` and the catalogs with the change. See `docs/i18n.md`.

Icons: when an icon is needed, use the SVG from Phosphor Icons (https://phosphoricons.com, MIT, the regular weight) by default. The shell's own icons are listed in `scripts/phosphor-icons.sh`, which copies them into `ui/icons/` under the freedesktop name the shell asks for (`NAME-symbolic.svg`); add a line to its list, run it, and commit the SVG. Use another source only when Phosphor has nothing fitting, and say so.

## Testing

Use the Go standard library's `testing`; name test functions `TestXxx`. Add behaviour regression tests for logic changes, using temporary directories, fake services and the private D-Bus of `internal/dbustest`; tests that need `dbus-daemon` are skipped without it. There is no coverage threshold.

Documentation, wording, simple logging, and visual or interaction changes to the UI do not get new automated tests. Check UI changes by hand in a nested session and note the steps. Do not verify an implementation by connecting to a database or starting a PostgreSQL container.

## Commits and Pull Requests

History uses both `fix(dock): ...`, `feat: ...`, `docs: ...` and "Component: what changed". A commit title says briefly what behaviour changed; keep each commit to one topic.

A PR explains the problem, the behaviour after the change and how it was verified; link the related issue if there is one. Attach screenshots for UI changes, and note any environment the change was not run in. When configuration or command behaviour changes, update the samples and documentation with it.

## Configuration and Operational Boundaries

Do not commit secrets, local user names or local absolute paths. Do not run binaries downloaded from the internet without explicit permission. Install and session-setup commands write to user or system directories; do not run them as ordinary build checks.
