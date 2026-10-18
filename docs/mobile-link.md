# Mobile Link

Mobile Link, `inua-connect`, pairs this computer with phones, tablets and computers that
run KDE Connect (Android, iOS, Linux, Windows, macOS), GSConnect or Valent.
It speaks KDE Connect's protocol (version 8), written from the protocol's
behaviour (`internal/kdeconnect`); no KDE or GNOME component is needed.

## What it does

A paired device that is reachable:

- shows its **battery** in the window, and a note when it runs low; a
  laptop's battery shows on the device;
- **pings** (a note here) and **rings**: *Ring* in the window rings the
  phone, and the phone's *Ring my computer* rings this one until the note
  is closed, for a minute at most;
- shares the **clipboard** both ways: text copied on one is there to paste
  on the other. What a password manager copies, or a program listed in
  `[clipboard] sensitive_sources`, stays here (the same check the
  clipboard's history makes);
- **shares** files, text and links: files from the device go to Downloads
  under a name of their own (a second `photo.jpg` is `photo (2).jpg`), and a
  note opens them or their folder; a link opens in the browser; text goes on
  the clipboard. *Send files…* and the text box send them the other way; a
  link sent opens on the device;
- shows the device's **notifications** here, with their buttons; closing one
  here closes it there, and one that can be answered (a message) has
  *Reply*, which opens a small window. This computer's notifications show on
  the device (read from the session bus as a monitor; transient ones stay
  here);
- drives this computer's **players** (MPRIS): the list, what plays, play,
  pause, next, previous, seek, volume, shuffle and repeat;
- works as a **touchpad and keyboard** (the phone's *Remote input*): the
  pointer, clicks, dragging, scrolling, and any character typed, through the
  compositor's virtual pointer and virtual keyboard (`internal/vinput`); no
  `/dev/uinput` rule is needed;
- runs **commands** set up on *This computer* (*Run command* on the phone):
  each a name and what it runs (`sh -c`, as you).

Each paired device has switches for the clipboard, this computer's
notifications, the pointer and keyboard, and the commands; all are on when
it pairs.

## Pairing

Turn *Connect with your devices* on in the window (`inua-connect --on`),
open KDE Connect on the phone on the same network or the tailnet, and
choose this computer, or choose the phone in the window and *Pair*. Both
show the same eight-character key: pair only if they match. A request from
the device shows as a note with *Pair* and *Refuse*, as well as in the
window. A paired device's certificate is kept; one that comes back with
another is refused.

## Finding devices

Devices announce themselves by UDP broadcast on port 1716 and link over TLS
on a port from 1716 to 1764; files go on ports 1739 to 1764. On the tailnet,
where there is no broadcast, Mobile Link announces itself to each machine online
directly, and the phone connects back: a phone with Tailscale finds this
computer away from home. A device the broadcast misses can be added by its
address (*Add a device by address…*). A firewall must let those ports in.

## The service

The window is only a view: the protocol runs in `inua-connect --service`,
in the systemd user unit `inua-connect.service` (started with the session's
display and bus), and serves `io.github.lyricat.Inua.MobileLink.Service` on the session
bus. The window's switch starts it and keeps it on at login (the shell
starts it again), or stops it.

Its data is in `~/.local/share/inua/connect`: the identity (`certificate.pem`
and `private.pem`, readable only by you), the paired devices (`devices.json`)
and the commands (`commands.json`).

## From the command line

```
inua-connect --on | --off        turn the service on (now and at login) or off
inua-connect --devices           the devices: paired, found, waiting to pair
inua-connect --pair DEVICE       pair, or accept its request (by name or ID)
inua-connect --unpair DEVICE     refuse a request, or forget a paired device
inua-connect --ping DEVICE
inua-connect --ring DEVICE
inua-connect --send DEVICE FILE…
inua-connect --text DEVICE TEXT
```
