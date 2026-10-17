# Workarounds

Bugs in the libraries and the compositor that inua works around, what the
workaround costs, and when it can go.

## Lock windows are not in the GtkApplication

**Bug:** [gtk4-layer-shell#122](https://github.com/wmww/gtk4-layer-shell/issues/122):
unlocking crashes a program whose lock windows belong to its
`GtkApplication`.

**Where:** gtk4-layer-shell 1.2 and 1.3.0 with GTK 4.22 or later. When the
lock ends (an unlock, or a lock the compositor refuses), the library
unrealizes each lock window and then destroys it. Destroying it takes it
out of its application, and GTK 4.22 then removes the window's surface from
the XDG session (`gtk_application_impl_wayland_window_forget`); the
surface is already gone, and GDK dereferences a NULL display. Unlocking
with the password and `inua call lock unlock` both took the shell down
right after the screen unlocked.

Some distributions patch GTK with a NULL check there (Ubuntu's 4.22.4 has
"gtkapplication-wayland: Add a missing NULL check when forgetting"), and
gtk4-layer-shell 1.1 does not destroy lock windows, so not every machine
crashes; Arch, with GTK as released, does.

**Workaround:** `buildLockSurface` (`internal/ui/lock.go`) does not call
`SetApplication` on the lock windows. They use no application actions or
accelerators, and the shell holds the application, so nothing else
changes.

**Remove when:** inua requires a gtk4-layer-shell release with the fix
(commit 4419f1b, PR #125, merged June 2026; not in 1.3.0). Then the call
can come back if the lock screen ever needs application actions.

**Check:** `dbus-run-session -- python3 scripts/test-monitor-lock.py`
locks and unlocks in a nested Hyprland. It shows the crash only on a GTK
4.22 without the distribution's NULL check and with gtk4-layer-shell 1.2
or 1.3.0 loaded (the shell logs the version it loaded at start).
