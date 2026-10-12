# Light apps

Small apps in the shell's look: each is its own window, with its own
application ID, so the launcher, the dock and Hyprland treat it as an app.
They follow inua's theme and style as you change them, and use the shell's
icons.

Open them from the launcher, from the drawer at the end of the dock, or
with `inua-apps NAME` (`inua-apps clock`, `inua-apps tailscale` …). An app
runs once: starting it again brings its window to the current workspace.
Viewer is the exception, with a window for each image opened. The
Assistant is a program of its own, `inua-assistant`.

Their data is kept in `~/.local/share/inua`.

## Games

Three small games, each its own app with its own launcher in the dock's
Play section, run by `inua-games` (`inua-games minesweeper`). All three run
offline and draw their boards in the desktop theme. Every window has the
rules behind its ⓘ button, its keys along the bottom, and New game (N).

| Game | Command | What it is |
| --- | --- | --- |
| Minesweeper (扫雷) | `inua-games minesweeper` | 9×9, 16×16 or 30×16. The first square is always safe; right-click flags; click a number with its flags placed to open around it. Best times are kept. |
| Space Pinball (太空弹球) | `inua-games space-pinball` | After the Windows table: fly missions from the mission targets and the launch ramp to climb nine ranks, with fuel to keep up, a hyperspace ramp of five levels, a black hole that locks balls for multiball, a wormhole, a skill shot picked by the plunger's pull, nudging and tilt, an end-of-ball bonus and sound. A display beside the table shows the rank, the mission and its clock, fuel and the latest news. |
| Small Fortune (大富翁) | `inua-games small-fortune` | A property game against one to three computer players on a printed washi board, seen from above and in front: a ring of 40 squares laid along a track shaped like the city (the Yamanote Line, Manhattan, Puxi in the Huangpu's bend, Rome's walls) and two branch roads across it or, in New York, over the rivers (at a fork, chance picks the road), with the city's landmarks standing on the map; 34 streets in twelve colour groups, four stations or piers, two utilities (40 properties), chance cards, jail, and a fountain that collects taxes and fines. Before a game, choose the city (Tokyo, New York, Shanghai or Rome, each with its own names and buildings), the rounds (20 to 60), the starting money, the rivals, and the rules: one die or two, and whether a street may go up a level the turn it is bought. Every street of yours you pass or land on may go up one level that turn, four houses then a hotel; a whole colour group doubles bare rent and adds half to built rents. After the last round, or when one player is left, the most worth wins. |

Pinball pauses with P, and on its own when its window is
left; M turns its sound off (it plays through `pw-cat`, silent without it). Games are kept in memory; best scores are kept in
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

## Assistant

An agent with inua's tools, in a program of its own (`inua-assistant`), on
[Mister Morph](https://github.com/quailyquaily/mistermorph)'s runtime. While
enabled, it starts with the shell and stays running in the background,
whether or not its Console endpoint is enabled. Closing its window hides
it; opening it again returns to the same conversation. Ask
it about this computer ("what is using the CPU?", "what did I miss?",
"which devices on my tailnet are online?") or to do something ("turn on
Focus and set the volume to 30%", "move Firefox to workspace 2"); it can
also search the web. The answer streams in, with a line for each thing it
does; anything that changes the system waits for Allow, and powering off,
ending processes, closing windows and the opt-in file and shell tools ask
every time. Ask from the launcher (the last row, or `?` before the query)
or with `inua-assistant --ask "…"`; questions go to the open window and
keep its conversation, and + starts a new one.

Set it up in Settings → AI. Each profile is a model: a provider
(OpenAI, Anthropic, Gemini, xAI, DeepSeek, Kimi, OpenRouter, Groq, or a
compatible server such as one on this computer), the model, and its key,
typed there (kept in the keyring, the Secret Service: in pass with
inua-keyring, see the shell's features; without one, in
`~/.local/share/inua/keys.json`, readable only by you); never in
`config.toml`. One
profile answers first; others marked Fallback take over, in order, when it
fails, and the window's drop-down switches profile for a conversation.
What the tools read is sent to the provider answering.

### Console endpoint

Other Mister Morph Consoles can connect to this desktop as a Runtime API
endpoint. Configure the assistant's model first, then create its access
token in **Settings → AI → Console endpoint** with **Create token**.
Set **Listen address** to `127.0.0.1:8794` for a Console on this computer
and turn on **Serve endpoint**, then use **Copy URL** and **Copy token**
to fill in Console's endpoint settings. **Show token** reads the saved token; **Hide token** clears it
from the field. Closing settings hides it too. Turn off **Serve endpoint**
to stop HTTP serving while keeping the address, token and assistant.

You can also create or read the same token from the command line:

```sh
inua-assistant --serve-token
```

The command prints the existing token, or creates one if missing. It stores
the token in the Secret Service; with inua-keyring, the secret is encrypted
in pass. A Secret Service is required: this token never falls back to
`keys.json` and is never written to `config.toml`. Keep the printed value
for the Console's endpoint settings.

Set the listening address under the existing `[assistant]` section in
`config.toml`:

```toml
[assistant]
serve_enable = true
serve_listen = "127.0.0.1:8794"
```

`serve_enable = false` (the default) disables HTTP serving while the
assistant continues running. The address above accepts connections from this
computer. For a Console on another computer, use the desktop's reachable
address, such as its Tailscale IP, and that address in the endpoint URL.

In the other Console, add an endpoint with:

- **URL:** `http://127.0.0.1:8794/runtime` (replace the address for a remote desktop).
- **Auth token:** the value printed by `inua-assistant --serve-token`.

The API uses `Authorization: Bearer TOKEN`. Console can submit tasks, read
their status and results, continue a topic, and cancel tasks. Local tasks
run while HTTP serving is enabled are also saved for Console to list;
remote topics keep their own history. Changing the local window's model
does not change a remote topic's model selection.

Remote tasks use the same desktop tools and confirmation rules as local
ones. When a tool needs approval, the assistant window appears with a
**Remote Console** card; approval happens on the desktop. Canceling the
task cancels its pending card. File attachments and remote approval of
these desktop cards are not supported by this endpoint.

Settings and saved keys are checked while the assistant runs. A reload
stops the old endpoint and cancels its remote tasks before starting the
replacement. A local answer finishes before settings reload. Missing
keys or listener errors are shown in the assistant window. Closing that
window leaves the process running; disabling the assistant stops its
endpoint. `SIGINT` or `SIGTERM` stops remote tasks and exits the process.

The shell starts it with `inua-assistant --background`; running this command
again reaches the existing process without showing its window. Starting
`inua-assistant` normally, or choosing it in the dock, shows the window.

## Viewer

Steps through a folder with the images beside the shown one already
loaded; zooms and pans, rotates, plays animations, shows a photo's EXIF;
copies, sets as wallpaper, moves to Trash. It registers for image files.
With `libturbojpeg0` installed it decodes JPEG several times faster.
