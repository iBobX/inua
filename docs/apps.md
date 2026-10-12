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
[Mister Morph](https://github.com/quailyquaily/mistermorph)'s runtime. Ask
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
`~/.local/share/inua/keys.json`, readable only by you) or read from an
environment variable; never in `config.toml`. One
profile answers first; others marked Fallback take over, in order, when it
fails, and the window's drop-down switches profile for a conversation.
What the tools read is sent to the provider answering. The design:
[feat/assistant.md](feat/assistant.md).

## Viewer

Steps through a folder with the images beside the shown one already
loaded; zooms and pans, rotates, plays animations, shows a photo's EXIF;
copies, sets as wallpaper, moves to Trash. It registers for image files.
With `libturbojpeg0` installed it decodes JPEG several times faster.
