# Start-up time

How long inua's apps take from launch to a window with their content, what
the time went to, and what was done about it. The apps measured are
System Monitor and Calculator (`inua-apps`), Files (`inua-files`, on a
folder of 457 items) and Mobile Link (`inua-connect`).

## Measured

In a nested Hyprland, nine launches of each program, before and after
taking turns; the medians, in milliseconds from the launch:

| | Before | After |
| --- | ---: | ---: |
| Go's package initialisation done (every program) | 435 | 265 |
| System Monitor: first frame | 1853 | 1450 |
| System Monitor: real CPU and network numbers | 2177 | 1204 (in the first frame) |
| Calculator: first frame | 1765 | 1312 |
| Mobile Link: first frame, with the devices | 1887 | 1436 |
| Files: first frame | 2113 | 1679 |
| Files: the folder's first rows drawn | 3196 | 2141 |
| Files: every row in | 3196 | 3158 |

The marks were temporary: a line printed with the time since the launch
at the start of `main`, after the window's `Present`, after each frame the
window's frame clock painted, when System Monitor showed a sample, when
Files gave the folder to its list, and when Mobile Link showed the
devices. To measure again, put such marks back and launch each program
in a nested session several times, the old and the new build in turns:
the machine's load moves every number by a few hundred milliseconds.

## Where the time went, and what was done

**Binding GTK's functions, ~165 ms in every program.** puregotk's
generated packages bind every function of GTK, GLib, Pango and the others
when the program starts: 27,054 of them, each a `dlsym` and a trampoline,
of which a program calls a few hundred. inua carries puregotk in
`third_party/puregotk`, patched to bind each function on its first call
instead (`third_party/puregotk/INUA.md`); go.mod points at it with a
`replace`. Loading the libraries themselves (~100 ms) is left.

**The icon theme, read twice, ~290 ms in the light apps.**
GtkApplication's start looks the app's icon up in the icon theme, which
reads the theme with every theme it inherits; a large desktop theme
such as Numix-Circle, which inherits five others, takes a quarter of a
second more than Adwaita. The apps then switched to their own Phosphor
theme, which inherited the desktop's, and the whole chain was read again.
Now the apps' theme is set before the application starts
(`appui.run`), and it inherits Adwaita: the apps' icons are Phosphor's,
and Adwaita has the symbolic icons the set lacks. What shows the desktop's
icons (Files' files, folders, places and the apps it offers to open a
file with) looks them up in an icon theme of the desktop's alone, as
before; Files has that theme read on a thread of its own while GTK starts
(`Preparer`), since GtkIconTheme takes lookups from any thread once made.

**System Monitor's first sample, a second late.** The window showed a
sample at once, from readings taken microseconds apart (no CPU use, no
traffic), and the next a second later. The first sample now comes a
quarter of a second after the readings, about when the window is first
drawn.

**Files' first rows, ~1 s late.** An empty list makes a row for each item
it might show, up to 200, at over a millisecond each and more for their
icons, all before the frame that shows any of them: 457 items gave one
frame of ~700 ms. An empty view now gets a screenful (48 items) first;
the rest come 48 a frame, and the selection once all are in. Every row is
in at about the same time as before, but the first are drawn a second
sooner, and no frame is longer than ~300 ms.

## What is left

From launch to the first frame, about 1.3 s for a light app:

- loading GTK and its libraries, ~100 ms, and the bindings' stubs;
- GTK's start: the display, the settings portal, the application's name
  on the bus;
- the style: `ui/style.css` (119 KB, the shell's and the apps' rules
  together) with the theme, ~110 ms to parse in each app. Splitting out
  the rules only the shell uses would save part of it;
- the first frame, ~250 ms: the GPU renderer's start (Vulkan; limiting
  the Vulkan drivers looked at to the machine's own,
  `VK_DRIVER_FILES`, saves ~80 ms, but that is the system's setting),
  and fontconfig's list of the fonts (~60 ms with 759 fonts);
- in Files, each row: ~1.1 ms to make and ~0.35 ms to bind, plus the
  icons it is the first to show. Lighter rows would make every row-filling
  frame shorter.

Going further would take a process kept running that starts each app with
GTK, the style and the icon themes already loaded (a "zygote", as
Android's), which has its own costs: memory held all the time, and an app's
crash or leak reaching the next.

## Lessons for the next time

- **Time what the user sees.** Mark the first frame and the moment the
  real content shows, not only functions. The two largest gains here
  (System Monitor's numbers and Files' rows, a second each) saved little
  CPU time: they moved the content forward.
- **Measure on a real desktop's settings.** The icon theme's cost depends
  on the theme: a quarter of a second with Numix-Circle, none with
  Adwaita. A clean test machine would not have shown it.
- **Look for work done twice and for its order.** The icon theme was read
  at start and again after the apps switched theme; setting it first
  removed the second read.
- **Compare builds fairly.** Run the old and the new build in turns, many
  times, and take medians: another program's load once added more than a
  second to every number. When a number looks wrong, read one run's raw
  marks before believing the summary: a mark printed again later (Mobile
  Link's list, refreshed every three seconds) once showed a slowdown that
  was not there.
- **Try the other way before choosing.** Files' rest of the folder in one
  frame and in parts finished at the same time; the parts kept every frame
  short. Only measuring both said so.
- **Wrong first data is a bug, not slowness.** System Monitor's first
  sample compared readings microseconds apart.
- **Find who depends on what a fix changes.** Inheriting Adwaita would have
  dropped the desktop's app icons from Files' "Open with" list; searching
  for the desktop theme's users found it before a person did.
- **Tell the toolkit's cost from ours.** The same window in a few lines of
  Python and GTK 4 showed the icon theme's cost was GTK's, and
  `GODEBUG=inittrace=1` showed the bindings' share of the start.
- **Write down what is left and why.** Costs not worth fixing yet stay in
  the list above, so they are not measured again from nothing.
