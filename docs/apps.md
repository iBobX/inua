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

Open **Games** from the dock's Play section or run `inua-apps games`.
Each game also has its own launcher and command. All nine run offline.
Boards follow the desktop theme; every game includes its rules, controls,
a result, and a restart button. Progress lasts until the game is restarted,
switched, or closed.

| Game | Command | Rules |
| --- | --- | --- |
| Last Tile (最后一格) | `inua-apps last-tile` | Escape a collapsing 5×5 floor in ten moves, collecting four coins. Ten repeatable maps. Arrow keys, WASD, or click a neighboring tile. |
| Full Fridge (冰箱塞不下) | `inua-apps full-fridge` | Pack nine shaped groceries into cold and fresh shelves. Select a food, rotate with R or the button, then click the top-left corner of its preview to place. Use Pick up food to remove an item. |
| Clock Out (下班前十步) | `inua-apps clock-out` | Reach the office exit while avoiding moving colleagues. Arrows or WASD move; Space waits. The board marks their next positions. Three patrol timings. |
| One-button Harbor (一键靠岸) | `inua-apps harbor` | Hold Space or the sea to turn clockwise; release to sail straight. Avoid rocks and dock heading right within 60 seconds. Three courses. |
| Three Dice Shop (三骰小店) | `inua-apps dice-shop` | Allocate three dice to stock, sales, and repairs. Pay daily rent and finish seven days with at least $65 cash. |
| Odd Groups (奇怪的分类) | `inua-apps odd-groups` | Find three groups of four words, with four mistakes allowed. Six English puzzles; answers appear at the end. |
| Minesweeper (扫雷) | `inua-apps minesweeper` | Clear a 9×9 or 12×12 field. The first reveal and its neighbors are safe. Right-click to flag, or toggle Flag mode with F; click a revealed number to open its neighbors when enough flags are placed. |
| Space Pinball (太空弹球) | `inua-apps space-pinball` | Space launches; A/Left and D/Right control the flippers. Holding either half of the table also controls its flipper. Three balls, 100 points per bumper hit. |
| Small Fortune (大富翁) | `inua-apps small-fortune` | Play twelve rounds against the computer: buy streets, build houses, collect rent, and draw chance events. Bankruptcy ends the game; otherwise, cash plus property value decides the winner. |

Harbor and Pinball start on request. P pauses; switching away from their
window pauses automatically. Resume with the button or P. Game state is
kept in memory; these games do not use accounts, network services, or a
database. Small Fortune is a short property game with its own board and
rules, rather than a reproduction of a commercial board game.

`inua-apps games minesweeper` opens a game within the Games window.

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
