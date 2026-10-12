# Light apps

Small apps in the shell's look: each is its own window, with its own
application ID, so the launcher, the dock and Hyprland treat it as an app.
They follow inua's theme and style as you change them, and use the shell's
icons.

Open them from the launcher, from the drawer at the end of the dock, or
with `inua-apps NAME` (`inua-apps clock`, `inua-apps tailscale` …). An app
runs once: starting it again brings its window to the current workspace.
Viewer is the exception, with a window for each image opened.

Their data is kept in `~/.local/share/inua`.

## Games

Nine small games, each its own app with its own launcher in the dock's
Play section, run by `inua-games` (`inua-games minesweeper`). All nine run
offline and draw their boards in the desktop theme. Every window has the
rules behind its ⓘ button, its keys along the bottom, and New game (N).

| Game | Command | What it is |
| --- | --- | --- |
| Last Tile (最后一格) | `inua-games last-tile` | A 5×5 floor that falls away behind you: reach the exit within ten moves with as many of the four coins as you can. Ten maps; the best haul on each is kept. Arrows or WASD, or click a tile. |
| Full Fridge (冰箱塞不下) | `inua-games full-fridge` | Pack every grocery: frozen food in the freezer, the rest on the shelves, with exactly enough room. Pick a food from the bag, turn it (R or right-click), and put it where its shadow fits. Three fridges. |
| Clock Out (下班前十步) | `inua-games clock-out` | Cross the office to the exit while colleagues walk their rounds, one step for each of yours; the board shows where they step next. Space waits. Six offices. |
| One-button Harbor (一键靠岸) | `inua-games harbor` | Hold Space (or the sea) to turn right, let go to sail straight; dock in the berth heading right within a minute. Three courses; best times are kept. |
| Three Dice Shop (三骰小店) | `inua-games dice-shop` | Each morning, give three dice to stock, sales and repairs; the day's outcome shows before you open. Have $75 by the end of day seven. |
| Odd Groups (奇怪的分类) | `inua-games odd-groups` | Twelve words, three groups of four, four mistakes allowed. Eight puzzles. |
| Minesweeper (扫雷) | `inua-games minesweeper` | 9×9, 16×16 or 30×16. The first square is always safe; right-click flags; click a number with its flags placed to open around it. Best times are kept. |
| Space Pinball (太空弹球) | `inua-games space-pinball` | A table under a dome: pop bumpers, slingshots, a bank of drop targets and three rollover lanes that raise a multiplier up to ×5. Hold Space to pull the plunger; A/← and D/→ work the flippers, or hold the table's halves. Ball save at the start of each ball; three balls; the best score is kept. |
| Small Fortune (大富翁) | `inua-games small-fortune` | A property game against one to three computer players on a 24-square board: two dice (doubles roll again), eight pairs of streets, houses on full pairs, chance cards, jail, and a fountain that collects taxes and fines. 20 rounds, or until one player is left; the most worth wins. |

Harbor and Pinball pause with P, and on their own when their window is
left. Games are kept in memory; best scores are kept in
`~/.local/share/inua/games.json`. No accounts, network or database.
Small Fortune is a property game with its own board and rules, not a
copy of a commercial one.

## Clock

World clocks, alarms, a stopwatch and a timer. The shell rings the alarms,
so they go off with Clock closed.

## Weather

The weather now, for the next 24 hours and the next 7 days, from
[Open-Meteo](https://open-meteo.com); places to switch between.

## Stocks

A watchlist with a chart for each, from Yahoo Finance's unofficial
endpoints (which may change without notice).

## Calculator

A calculator with scientific keys.

## Calendar

The month with week numbers; a chosen day's distance from today, its week
and day of the year. The bar's clock opens a calendar panel too.

## Color Picker

Picks a colour from the screen with a loupe and keeps the colours picked,
in hex, RGB and HSL to copy. `inua call screenshot color` picks one
without opening the app.

## System Monitor

- **Overview:** each core, memory, the GPU, network and disks, with their
  last minute; the battery.
- **Processes:** sorted by CPU, memory or name, filtered by name or user. A
  process opens to its own page: CPU, memory and threads, its command,
  program, folder, open files and disk traffic. It can be asked to quit,
  interrupted, hung up, paused and resumed, or forced to quit, alone, with
  its children, or with every process of the same name; another user's
  processes ask for an administrator's password first.
- **System:** the computer, its processor, memory, graphics and storage.

## Tailscale

[Tailscale](https://tailscale.com) through tailscaled's local API:

- **Devices:** the tailnet's, online first, filtered by name, address, OS or
  tag; each with a menu to copy its address or MagicDNS name. A device
  opens to its addresses, how it is reached (direct or relayed), traffic,
  key expiry and tags, with Ping, Use as Exit Node, and a drop zone that
  sends files with Taildrop.
- **Exit node:** none or any device offering itself, and local network
  access while one is in use.
- **This device:** its addresses; Tailscale DNS, subnet routes, refusing
  incoming connections, offering itself as an exit node and Tailscale SSH;
  files received by Taildrop, saved to Downloads; logging out.

A notice says what is in the way and offers the fix: starting the daemon,
logging in, an admin's approval, or (once, with an administrator's
password) letting your user change Tailscale. The control center has a
Tailscale tile to connect and disconnect.

## Viewer

Steps through a folder with the images beside the shown one already
loaded; zooms and pans, rotates, plays animations, shows a photo's EXIF;
copies, sets as wallpaper, moves to Trash. It registers for image files.
With `libturbojpeg0` installed it decodes JPEG several times faster.
