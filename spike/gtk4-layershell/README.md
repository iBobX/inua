# Experiment: GTK4 (puregotk) + gtk4-layer-shell

> **2026-10-09:** the experiment's code became the real code: shell state in
> `internal/shell`, the interface in `internal/ui`, layouts in `ui/` at the
> top of the repository, and the program `cmd/inua-shell` (the project has
> since been renamed inua; run it with `scripts/spike.sh shell
> nested|stress|run` and control it with `inua call
> launcher|control|shell …`). This file stays as the record of the
> experiment; the commands under "Running" are for the deleted experiment
> program.

The comparison prototype (MyGO) was deleted; see the git history. The
comparison and verdict are in [docs/toolkit.md](../../docs/toolkit.md).

The question: without QML and without cgo, can Go + GTK4 build a desktop
shell's two core surfaces?

- **Bar**: a layer-shell top surface anchored to the top edge with reserved
  space, showing Hyprland's workspaces, the focused window's title and a
  clock.
- **Launcher**: an overlay with exclusive keyboard, app and window modes,
  and focus back to the previous window on cancel.

Stack: [puregotk](https://codeberg.org/puregotk/puregotk) (GTK4 through
purego, pinned to commit `d92056b`), gtk4-layer-shell 1.3 (our own purego
binding, `internal/layershell`), `internal/gtkx` (puregotk's ownership,
callback and main-thread rules), `internal/hypr`, `internal/desktop`,
`internal/ipc`. Built with `CGO_ENABLED=0`.

Layouts are `ui/*.blp` (Blueprint), compiled by `scripts/blueprint.sh`
(`go generate`) into embedded `ui/*.ui` files; styles are in `ui/style.css`.
Go only fetches named widgets, fills them and handles events; repeated items
(result rows) are one `row.ui` instance each.

## Running

```sh
sudo apt install libgtk4-layer-shell0 hyprland
scripts/spike.sh gtk4 nested        # a nested Hyprland window in GNOME, running the experiment
scripts/spike.sh gtk4 stress 100    # in another terminal (export the two variables it prints first)
scripts/spike.sh gtk4 run           # straight in a real Hyprland session
```

Control: `bin/gtk4-layershell ctl toggle|apps|windows|control|close|state`.
In the launcher: type to search, Tab switches modes, arrows select, Enter
runs, Esc cancels. `control` toggles the control center (as does the status
button at the right of the bar).

Debugging: `ctl snapshot DIR` has GTK render each window (and an open tray
menu) to PNG, without compositor effects (blur, animation), for checking
layouts without a screenshot tool. A nested Hyprland that is not in front
sends no frame callbacks, so GTK does not lay out again and a snapshot may
be stale or blank. `ctl tray-menu N` opens the N-th tray item's menu. The
`GQS_INSTANCE` variable named the control socket (default `spike`), so an
instance in a nested session could run beside one in the real session; `ctl`
read it too.

Changing layouts:

```sh
sudo apt install blueprint-compiler
# after editing ui/*.blp:
go generate ./spike/gtk4-layershell      # regenerate ui/*.ui
bin/gtk4-layershell -ui-dir spike/gtk4-layershell/ui   # read .ui/.css from disk
```

With `-ui-dir` the launcher rereads `launcher.ui` and `row.ui` on every open,
so changes show without a restart; the bar and styles are read at start.

## Results (2026-10-08, Ubuntu 26.04, Hyprland 0.53.3 nested in GNOME, AMD GPU)

| | Result |
| --- | --- |
| Pure Go build | `CGO_ENABLED=0` works, builds in seconds |
| Bar as a layer surface | Pass: `hyprctl layers` shows `gqs-bar` on the top layer, 1280×30 |
| Reserved space | Pass: `hyprctl monitors` reports reserved `[0, 30, 0, 0]` |
| Launcher overlay, centred | Pass: `gqs-launcher` on the overlay layer, 640×472, centred |
| Exclusive keyboard | Pass: the entry got focus on every open (stress test 300/300) |
| Hyprland state | Pass: workspaces, title and window list follow events |
| Focus back on cancel | Pass: `focuswindow` to the window active before opening |
| Chinese interface text | Pass: renders fine |
| Chinese input (Fcitx 5) | **Not tested**: needs typing by hand in a real session |
| Several monitors / over fullscreen apps | **Not tested** |
| Repeated opening | Pass (with the fix for problem 1): 300 times without a hang |
| Memory | +5.6 MB over the first 100 (caches warming up), then about +0.5 MB per 100 |
| After moving layouts to `.ui` (Blueprint) + `gtkx` | 300 cycles: no hang, focus 300/300, no GTK warnings, memory 132.0 → 132.6 MB |
| Editing `launcher.ui` while running | Applies on the next open (`-ui-dir`) |

## Problems found

1. **Showing the same layer surface again after hiding it hangs the main
   loop.** With a GPU renderer (Vulkan by default, or `GSK_RENDERER=ngl`),
   the fourth time it is shown again GTK waits forever presenting a frame
   (gdb: `gsk_renderer_render` → the driver's
   `wl_display_dispatch_queue_timeout`). Speed does not matter; the software
   renderer (`cairo`) is fine. Probably the swapchain's buffers are not
   released by the compositor while the surface is hidden, and run out on
   the fourth time.
   **Fix:** the launcher builds a new window on every open and destroys it on
   close; 100 cycles under each of the three renderers never hung. Whether
   real (not nested) Hyprland does the same is still to check, and it should
   be reported to gtk4-layer-shell / Hyprland.
2. **puregotk's ownership rules.** Every widget's constructor (a
   GInitiallyUnowned) calls `g_object_ref_sink`, and there is no finalizer:
   Go holds a strong reference that leaks unless dropped (about +200 KB per
   cycle before this was handled). Rules:
   - Once a widget is in its parent, `Unref()` your reference; widgets you
     keep get `Unref()` after their window is destroyed.
   - Plain GObjects (event controllers) are not sunk at construction, and
     `AddController` takes the only reference: **do not** `Unref()` them (GTK
     reports a CRITICAL).
   - Values from `GetFirstChild` and the like carry a new reference;
     `Unref()` them when done.
3. **A limit on callbacks.** purego makes a native trampoline for every Go
   function pointer, at most 2000, never freed. Signal handlers must be
   long-lived variables (telling widgets apart by name), not new closures per
   widget. Posting to the main thread also uses one fixed idle callback.
4. **Missing interface signals.** puregotk generates no `Connect` for
   GtkEditable's `changed`; connect it with `gobject.SignalConnect`.
5. **gtk4-layer-shell's load order.** It must load before
   libwayland-client, but puregotk loads GTK during package initialization.
   `layershell.EnsurePreloaded` re-executes the process at the start of
   `main` with `LD_PRELOAD`, and restores the original value in the child's
   environment so launched apps do not inherit it.
6. **A nested Hyprland may hang on exit.** After `hyprctl dispatch exit` or
   SIGTERM it does not exit; `scripts/spike.sh` uses SIGKILL.

## What gtkx wraps

`internal/gtkx` handles problems 2–4 in one place; interface code never calls
`Unref` or creates native callbacks itself:

| Case | How |
| --- | --- |
| A widget Go made goes into a container | `gtkx.Append(box, &w.Widget)` |
| Empty a container | `gtkx.Clear(box)` |
| Objects kept from a `.ui` | `ui.Get(id, &x)`, tracked in the window's `gtkx.Scope`, `Release()` after the window is destroyed |
| Objects from a `.ui` handed to a parent | `ui.Take(id, &x)` + `gtkx.Append` |
| Signal handlers | Long-lived fields (`s.onKey`), shared by all widgets |
| Interface signals (GtkEditable::changed) | `gtkx.ConnectChanged` |
| Updating the interface from a goroutine | `gtkx.Post` / `gtkx.Call` |

## Conclusion

Layer shell, exclusive keyboard, the Hyprland data path, `.ui` markup and the
Go structure all work, and the build is far simpler than with Qt. puregotk's
reference and callback rules are in `internal/gtkx`. What remained was
problem 1 in a real session, and Chinese input, both to check by hand in
Hyprland. This approach was chosen for the interface; see
[docs/toolkit.md](../../docs/toolkit.md).
