"""At a Glance: inua's desk functions and its building blocks in one app.

Each tab reads something through app.call (system_status, media_status,
list_workspaces, clipboard_history, notification_history) and follows
changes with @app.watch, so it stays current while the window is open.
"""

from inua import app, ui, InuaError

volumes = []  # the volume as it changed, for the chart


def read(method, **params):
    """A desk function's answer, or the error's text."""
    try:
        return app.call(method, **params)
    except InuaError as e:
        return {"error": str(e)}


@app.init
def start():
    app.state["system"] = read("system_status")
    app.state["media"] = read("media_status")


@app.watch("system")
def system_changed(ev):
    app.state["system"] = ev.value
    v = (ev.value.get("volume") or {}).get("percent")
    if v is not None:
        volumes.append(v)
        del volumes[:-60]


@app.watch("media")
def media_changed(ev):
    app.state["media"] = ev.value


def system_tab(state):
    st = state.get("system") or {}
    if "error" in st:
        return [ui.empty("The shell does not answer", text=st["error"], icon="dialog-warning-symbolic")]
    bat = st.get("battery") or {}
    vol = st.get("volume") or {}
    wifi = st.get("wifi") or {}
    kids = [
        ui.box(ui.text("%d%%" % bat.get("percent", 0) if bat else "—", style="hero"),
               ui.text("BATTERY" + (" · CHARGING" if bat.get("charging") else ""), style="kicker"), spacing=2),
        ui.tiles(
            ui.tile("wifi", "Wi-Fi", sub=wifi.get("network") or ("on" if wifi.get("on") else "off"),
                    icon="network-wireless-signal-excellent-symbolic", selected=bool(wifi.get("on"))),
            ui.tile("focus", "Focus", sub="on" if st.get("focus") else "off",
                    icon="weather-clear-night-symbolic", selected=bool(st.get("focus"))),
            ui.tile("saver", "Power saver", sub="on" if st.get("power_saver") else "off",
                    icon="power-profile-power-saver-symbolic", selected=bool(st.get("power_saver"))),
        ),
        ui.section("Volume", ui.slider("volume", 0, 100, 1, value=vol.get("percent", 0)),
                   meta="muted" if vol.get("muted") else None),
    ]
    if len(volumes) > 1:
        kids.append(ui.chart(volumes, height=60, min=0, max=100))
    return kids


def media_tab(state):
    m = state.get("media") or {}
    if not m.get("available"):
        return [ui.empty("Nothing plays", text=m.get("error"), icon="media-playback-start-symbolic")]
    return [
        ui.text(m.get("title") or "—", style="heading"),
        ui.text(m.get("artist") or "", style="sub"),
        ui.text(m.get("player", "").upper(), style="kicker"),
        ui.box(ui.button("previous", icon="media-skip-backward-symbolic", tooltip="Previous"),
               ui.button("play_pause", "Pause" if m.get("playing") else "Play", style="primary"),
               ui.button("next", icon="media-skip-forward-symbolic", tooltip="Next"), horizontal=True),
    ]


def workspaces_tab(state):
    ws = read("list_workspaces")
    if "error" in ws:
        return [ui.empty("No workspaces", text=ws["error"])]
    rows = [ui.row("Workspace %s" % w["name"], sub="%s · %d windows" % (w["monitor"], w["windows"]),
                   id="ws:%s" % w["name"], selected=w.get("focused")) for w in ws["workspaces"]]
    mons = [(m["name"], "%d×%d at %g" % (m["width"], m["height"], m["scale"])) for m in ws["monitors"]]
    return [ui.section("Workspaces", *rows), ui.section("Monitors", ui.facts(mons))]


def clipboard_tab(state):
    show = state.get("clip_kind") or "Text"
    es = read("clipboard_history", query=state.get("clip_q", ""), limit=30)
    if isinstance(es, dict):
        return [ui.empty("No history", text=es["error"])]
    es = [e for e in es if (e["type"] == "image") == (show == "Images")]
    rows = []
    for e in es:
        if e["type"] == "image":
            rows.append(ui.box(ui.image(src=e["path"], height=90),
                               ui.button("copy:" + e["id"], "Copy again"), spacing=4))
        else:
            rows.append(ui.row(e["text"].strip().splitlines()[0][:80] if e["text"].strip() else "(blank)",
                               sub="%s · %s" % (e.get("source") or "unknown app", e["time"][11:16]),
                               end=[ui.button("copy:" + e["id"], icon="edit-copy-symbolic", style="plain", tooltip="Copy again")]))
    return [ui.box(ui.search("clip_q", placeholder="Search what you copied"),
                   ui.segmented("clip_kind", ["Text", "Images"], value=show), horizontal=True),
            *(rows or [ui.empty("Nothing here")])]


def notes_tab(state):
    rs = read("notification_history", limit=30)
    if isinstance(rs, dict):
        return [ui.empty("No history", text=rs["error"], icon="preferences-system-notifications-symbolic")]
    rows = [ui.row(r["summary"], sub="%s · %s" % (r["app"], r["time"][:16].replace("T", " "))) for r in rs]
    return rows or [ui.empty("No notifications yet")]


def file_tab(state):
    kids = [ui.text("Pick a file; inua's own dialog opens over this window.")]
    kids.append(ui.box(ui.button("pick", "Choose a file…"), ui.button("save", "Save as…"), horizontal=True))
    if state.get("picked"):
        kids.append(ui.facts([("Chosen", state["picked"])]))
    if state.get("busy"):
        kids.append(ui.spinner("Reading…"))
    if state.get("progress") is not None:
        kids.append(ui.progress(state["progress"], text="%d%%" % (state["progress"] * 100)))
    return kids


@app.view
def view(state):
    return ui.page(ui.tabs("tab",
        ui.tab("system", "System", *system_tab(state)),
        ui.tab("media", "Playing", *media_tab(state)),
        ui.tab("workspaces", "Workspaces", *workspaces_tab(state)),
        ui.tab("clipboard", "Copied", *clipboard_tab(state)),
        ui.tab("notes", "Notifications", *notes_tab(state)),
        ui.tab("files", "Files", *file_tab(state)),
        value=state.get("tab")))


@app.on("volume", "change")
def volume(ev):
    read("set_system", setting="volume", value=str(int(ev.value)))


@app.on("wifi", "click")
@app.on("focus", "click")
@app.on("saver", "click")
def toggle(ev):
    st = app.state.get("system") or {}
    on = {"wifi": (st.get("wifi") or {}).get("on"), "focus": st.get("focus"), "saver": st.get("power_saver")}[ev.id]
    read("set_system", setting=ev.id, value="off" if on else "on")


@app.on("play_pause", "click")
@app.on("next", "click")
@app.on("previous", "click")
def media(ev):
    read("media_control", action=ev.id)


@app.on("ws:*", "click")
def workspace(ev):
    read("switch_workspace", workspace=ev.id[3:])


@app.on("copy:*", "click")
def copy(ev):
    read("clipboard_history_copy", id=ev.id[5:])


@app.on("pick", "click")
def pick(ev):
    path = app.call("dialog.openFile", title="Choose a file")
    app.state["picked"] = path or ""
    app.state["progress"] = 1.0 if path else None


@app.on("save", "click")
def save(ev):
    path = app.call("dialog.saveFile", title="Save as", name="glance.txt")
    if path:
        with open(path, "w") as f:
            f.write("Saved by At a Glance\n")
        app.state["picked"] = path


app.run()
