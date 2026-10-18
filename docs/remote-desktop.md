# Remote Desktop

`inua-remote` shares this desktop with your other devices, and shows
another machine's desktop here, with this keyboard and mouse sent to it.
It speaks GameStream, the protocol of Moonlight and Sunshine: video encoded
by the graphics card and decoded by this one, at game-streaming speed, and
sound with it. Any Moonlight app (phones, tablets, Windows, macOS) can
connect to a desktop inua shares, and Remote Desktop can connect to any
Sunshine.

```
inua-remote                    open the window
inua-remote --share on         share this desktop, now and at login (off: stop)
inua-remote --virtual-screen on|off|2560x1440@60
                               a screen to share while no monitor is connected
inua-remote --pair PIN         pair the device waiting, with the PIN it shows
            [--device NAME|ADDRESS] [--name NAME] [--wait 1m]
inua-remote --status           whether it is shared, what sharing needs, the
                               devices waiting to pair and those paired
```

## The window

```
┌──────────────────┬───────────────────────────────────────────────┐
│ Remote Desktop   │ ▌[DESK]                                       │
│ THIS COMPUTER    │  Paired, and ready.          Forget  Connect  │
│ ▣ Sharing   Off  │  192.168.1.20:47989                           │
│ MACHINES       ↻ │ ▌[STREAM]                    for every machine│
│ ▣ Desk   Paired  │  Resolution             This monitor's  ▾     │
│ ▣ Work   tailnet │  Frames a second        60              ▾     │
│ + Add a machine… │  Bitrate                20 − +                │
└──────────────────┴───────────────────────────────────────────────┘
```

The index lists **This computer** (sharing), then the machines: those
saved, then those found sharing their desktop, on the local network (they
announce themselves, mDNS `_nvstream._tcp`) or on the tailnet (each online
peer is asked; ↻ looks again). *Add a machine…* takes an address: a name,
an IP address, either with `:port`.

## Sharing this desktop

The switch runs [Sunshine](https://github.com/LizardByte/Sunshine), the
host Moonlight connects to, with a configuration inua writes in
`~/.config/inua/sharing/`: this computer's name, the desktop as its only
app, its web interface answering this computer only, and a certificate and
an admin password of inua's own (the password's salted hash written to
Sunshine's credentials file, never on a command line). Sunshine runs as the
user unit `inua-sharing.service`, with this session's display; the shell
starts it again at login while sharing is on. Run from outside the session
(`inua-remote --share on` over SSH), it takes the display of the Hyprland
running for you, found by its `$XDG_RUNTIME_DIR/hypr/*/hyprland.lock`; with
none running, it refuses rather than start a Sunshine with nothing to
capture.

A device pairs once: its user picks this computer, their screen shows a
PIN, and the device appears here under *Waiting to pair*, where the PIN is
typed. Paired devices are listed, each with *Unpair*.

Without the window (over SSH, say): `inua-remote --pair 1234` pairs the
device waiting, waiting up to a minute for it to ask if it has not yet;
when several wait, `--device` names the one (by its name or address), and
`--name` is what it is called here (its own name if not given).

What sharing needs, and the page says so when it is missing:

- **Sunshine**, which is not in Ubuntu's or Debian's archive: install the
  package for your system from its
  [releases](https://github.com/LizardByte/Sunshine/releases)
  (`sudo apt install ./sunshine_…_amd64.deb`; Arch has it in the AUR,
  NixOS as `sunshine`).
- **`/dev/uinput`, writable by you**: the other device's keyboard and mouse
  reach this desktop through a virtual one. Sunshine's packages install
  the udev rule; without it, the page gives the line that does.
- **A graphical session**: Sunshine captures the screen of the session
  inua-remote runs in, or of the one running for you.
- **A screen**: a computer without a monitor has nothing to capture, and a
  device connecting to it hears that it cannot show its screen. See below.

### A computer without a monitor

*Virtual screen*, under the sharing switch, is off unless you choose a
size. With one chosen, the shell adds a headless output to Hyprland,
`INUA-VIRTUAL` at that size, while no monitor is connected, and removes it
when one is plugged in; a monitor turned off (a laptop with its lid
closed) still counts as connected. Sunshine looks for screens and encoders
as it starts, so a running Sunshine is started again when the virtual
screen comes or goes. The setting is the file `virtual` in
`~/.config/inua/sharing/`; `inua-remote --virtual-screen` sets it from a
shell (`on` is 1920x1080@60).

The output needs the session to be running: for a computer nobody logs
in to, let the display manager start inua's session by itself. With
greetd, for example, beside its `default_session`:

```toml
[initial_session]
command = "/usr/local/bin/inua-session"
user = "you"
```

The size is set with `hyprctl keyword monitor`, which a Lua configuration
(Hyprland 0.55 and later) may not take; the screen is then Hyprland's
default size.

Encryption: the control stream, which carries the other device's input,
is always encrypted (AES-GCM, the session's key). Video and sound are not
on the local network (`lan_encryption_mode = 0`), and are where the device
asks, elsewhere (`wan_encryption_mode = 1`).

## Connecting to a machine

**Pair** shows a PIN: type it on the machine (in its Remote Desktop under
Sharing, or Sunshine's PIN page). Pairing proves to both sides that each
knows the PIN, without sending it, and each keeps the other's certificate:
this computer's identity is kept in `~/.local/share/inua/remote-desktop/`,
and the machine's certificate with the machine in
`~/.local/share/inua/remote-desktop.json`. A later connection trusts that
certificate and no other.

**Connect** opens the machine's desktop in a window of its own. The
stream's settings, for every machine: the resolution (this monitor's, or
720p to 4K), frames a second (30 to 120), the bitrate (2 to 150 Mb/s), the
codec (H.264, HEVC or AV1; the page says whether this computer decodes it
with its graphics card), and whether the sound also plays on the machine.
If the machine is already streaming, connecting joins that session; *End
its session* ends it for every device.

In the stream's window every key goes to the machine, the compositor's
shortcuts (Super and the rest) too, by their place on the keyboard: the
machine's layout gives them their letters. The pointer is the machine's.

| Keys | Action |
| --- | --- |
| Ctrl+Alt+Shift+Q | Disconnect (the session on the machine goes on, to be joined again) |
| Ctrl+Alt+Shift+F | Full screen, or back |
| Ctrl+Alt+Shift+Z | This desktop's shortcuts back, or the machine's again |

Keys held when the window loses the keyboard are let go on the machine.

## How it works

| Package | What |
| --- | --- |
| `internal/gamestream` | The client: this computer's identity (an RSA certificate), the host's server info, pairing by PIN (AES with the PIN's key, each side's signed secret), the app list and launching (HTTPS with the client's certificate, the host's pinned), the RTSP handshake and the session's description, the control stream on ENet (AES-GCM messages, input among them), video frames rebuilt from their RTP packets, Opus from the audio packets, mDNS discovery |
| `internal/gamestream/enet` | The part of ENet the control stream uses: connecting, reliable and unsequenced packets, acknowledgements, sending again, ordering, fragments, pings |
| `internal/apps/remotedesk` | Sharing (Sunshine's configuration, its unit, its pairing API), the machines saved and found (mDNS, the tailnet), the stream's settings, keys by their place (evdev to Windows virtual keys), the pointer on the picture |
| `internal/gst` (`stream.go`) | A live stream decoded as it comes: the graphics card's decoder through VA-API when there is one (`vah264dec`, `vah265dec`, `vaav1dec`), else the processor's; Opus played |
| `internal/remoteui`, `cmd/inua-remote`, `ui/remote.blp` | The window, and a window for each stream: its picture, the keyboard, the pointer and the wheel sent to the machine |

The client is written from the protocol's behaviour, Moonlight's and
Sunshine's (both GPL-3) read for it, not copied: inua is Apache-2.0. A lost
video packet is not repaired from the parity packets: the frame, and those
that depend on it, are dropped and the host is asked for a new IDR frame,
which on a local network is rarely needed. Video and audio encryption, and
the RTSP encryption newer clients offer, are not spoken: a host that
requires them is refused with that said.

## Tests

`internal/gamestream` runs against fakes of each of the host's parts: its
HTTP and HTTPS (pairing with the right and a wrong PIN, a certificate not
the one paired), its RTSP (the requests in order, every attribute Sunshine
requires in the description), an ENet host that drops and reorders, and a
whole session (video, audio, input, and the host ending it); the control
messages are checked against the host's own way of reading them, and the
input packets byte for byte. `internal/gst` decodes an H.264 stream it
encodes itself, and plays Opus into a fake sink. `internal/apps/remotedesk`
runs against a fake Sunshine API served with the certificate inua made,
fake programs for systemd, and a fake Hyprland socket for the virtual
screen; `internal/hypr` finds the running instance among leftover
directories. The window was checked rendered offscreen
(Broadway).

Against a real Sunshine (2026.914), sharing a nested Hyprland (`capture =
wlr`, so the real screen was not shared): inua started it with its own
configuration and was let into its API with the password file it wrote;
a client paired with it by PIN, both sides inua's; the app list, and a
stream of six seconds each: H.264 at 1280×720 and 30 frames a second, HEVC
and AV1 at 1920×1080 and 60, every frame whole (one IDR frame each), the
pictures decoded by the graphics card (AMD, VA-API), and the sound; and
input: two absolute pointer moves, a relative one and left Shift pressed
and let go, each read by Sunshine as sent and the real session's pointer
moved by them (Sunshine's virtual devices reach the session it runs in,
whatever screen it captures).
