# Choosing the interface toolkit

Decision: **GTK4 + puregotk, with layouts in Blueprint/`.ui` markup.**

## The options

| Option | What | Verdict |
| --- | --- | --- |
| **GTK4 + puregotk** | A purego binding of GTK4, no cgo; layouts as GtkBuilder `.ui` / Blueprint | **Chosen** |
| gotk4 | A cgo binding of GTK4 | The fallback; the interface code and `.ui` files mostly carry over |
| MyGO | Views are Go functions drawn by MyGO, on GTK3 | No markup layouts; young, one author |
| Qt/QML | Qt Quick through cgo bindings | Needs Qt, cgo, moc and C++ |
| Web views | WebKitGTK | 100 MB or more per view, too heavy for a resident shell |

Both GTK4 + puregotk and MyGO were built as the same bar and launcher and
tested in a nested Hyprland ([spike/gtk4-layershell](../spike/gtk4-layershell/README.md)):

| | GTK4 + puregotk | MyGO |
| --- | --- | --- |
| Layouts in markup | **Yes**: Blueprint → `.ui`, loadable at run time | No: Go only |
| Change a layout without rebuilding | Yes (`-ui-dir`) | No |
| Layer shell | gtk4-layer-shell 1.3 used as intended; an `LD_PRELOAD` re-exec (wrapped) | gtk-layer-shell 0.10, relying on MyGO internals |
| 300 open/close cycles | Pass (overlays build a new window on each open) | Pass |
| Memory growth over 300 cycles | 0.5 MB | 3.7 MB |
| Maturity | GTK4 mature; puregotk experimental, gotk4 behind it | Very new, no fallback |
| Go version / binary | 1.24 / 8.5 MB | 1.27 / 17 MB |

## Why GTK4 + puregotk

1. **Markup layouts.** Layout, style and Go logic stay apart: `.blp`
   describes widget trees and static properties, `style.css` the look, and
   Go only fetches named widgets, fills them and handles events. Layouts
   change without a rebuild.
2. **The intended layer-shell path.** gtk4-layer-shell is used the way the
   library means, not through another framework's internals.
3. **A mature base.** Text input and input methods, accessibility and lists
   come from GTK4. If puregotk cannot go on, gotk4 binds the same GTK4 API,
   so the `.ui` files and most interface code stay.
4. **The results.** The wrapped version was stable over 300 cycles, with flat
   memory and no GTK warnings.

The cost: updating the interface from state is hand-written, unlike
declarative toolkits. The answer is to keep state and logic in toolkit-free
Go packages with tests, and the view thin.

## Risks

| Risk | Answer |
| --- | --- |
| puregotk is experimental, unreleased, and has a few wrong APIs | Pin a commit; use only the patterns `gtkx` wraps; run `scripts/spike.sh shell stress` on upgrades; move to gotk4 if needed |
| Ownership and callback rules are easy to get wrong | Interface code goes through `internal/gtkx` (`Append`, `Clear`, `Scope`, `Builder.Get/Take`, long-lived handlers); stress tests watch memory and GTK warnings |
| Showing the same layer surface again hangs | Overlays build a new window on every open; check whether it happens outside nested sessions and report it to gtk4-layer-shell / Hyprland |
| More code to update the interface from state | Shell state, search and focus rules live in `internal/shell` with tests; the view only maps state to widgets |
| Editing `.blp` needs blueprint-compiler | The generated `.ui` files are committed; only people who change layouts need it |

## Still to check in a real session

In a real Hyprland session (`scripts/spike.sh shell run`):

1. Fcitx 5 Chinese input in the launcher: candidate window position,
   committing, and Esc/Enter/arrow keys while composing.
2. Opening the launcher over a fullscreen app and typing.
3. Whether the hang when showing a layer surface again happens outside
   nested sessions.
4. State after long runs and Hyprland restarts.

Several monitors (a bar per monitor, overlays on the focused one) work in a
real session.
