"""At a Glance: inua's desk functions and its building blocks in one app.

Each of the first tabs reads something through app.call (system_status,
media_status, list_workspaces, clipboard_history, notification_history)
and follows changes with @app.watch, so it stays current while the window
is open. Components shows every building block, in its kinds, its inputs
live; Layouts the light apps' layouts: a sidebar, a panel at the right,
or both, around the page.
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


def components_tab(state):
    """Every building block, by name, in the kinds it comes in."""
    last = state.get("last") or "Use an input below: its event shows here."
    points = [50 + 30 * ((i * 37) % 11 - 5) / 5 for i in range(24)]
    return [
        ui.section("Last event", ui.text(last, style="mono")),
        ui.section("text",
                   *[ui.box(ui.text(s or "plain", style="kicker"),
                            ui.text("At a glance" if s != "hero" else "42", style=s), spacing=2)
                     for s in (None, "heading", "title", "sub", "mono", "readout", "kicker", "hero")]),
        ui.section("button",
                   ui.box(ui.button("demo:default", "Default"),
                          ui.button("demo:primary", "Primary", style="primary"),
                          ui.button("demo:danger", "Danger", style="danger"),
                          ui.button("demo:plain", "Plain", style="plain"),
                          ui.button("demo:icon", icon="edit-copy-symbolic", tooltip="An icon button"),
                          ui.button("demo:icon-plain", icon="list-add-symbolic", style="plain", tooltip="A plain icon button"),
                          horizontal=True)),
        ui.section("entry · search · textarea",
                   ui.entry("demo:entry", placeholder="An entry: Enter submits"),
                   ui.entry("demo:password", placeholder="A password", password=True),
                   ui.search("demo:search", placeholder="A search"),
                   ui.textarea("demo:textarea", value="A textarea,\nover lines.")),
        ui.section("switch · dropdown · segmented · slider",
                   ui.row("A switch", sub="On or off", end=[ui.switch("demo:switch", value=True)]),
                   ui.row("A drop-down", sub="One of a few", end=[ui.dropdown("demo:dropdown", ["Small", "Medium", "Large"], value="Medium")]),
                   ui.row("Segmented", sub="A few options, joined", end=[ui.segmented("demo:segmented", ["°C", "°F"], value="°C")]),
                   ui.slider("demo:slider", 0, 100, 5, value=40)),
        ui.section("progress · spinner · chart",
                   ui.progress(0.35, text="35%"),
                   ui.progress(1.0, text="done"),
                   ui.spinner("Something is going on"),
                   ui.chart(points, height=70, min=0, max=100)),
        ui.section("row · facts",
                   ui.row("A row", sub="Its line under the title"),
                   ui.row("With an icon", sub="And an id: a click is an event", id="demo:row", icon="folder-open-symbolic"),
                   ui.row("Selected", sub="As the one in use", icon="object-select-symbolic", selected=True),
                   ui.row("With widgets at its end", end=[ui.button("demo:row-end", icon="user-trash-symbolic", style="plain", tooltip="At the row's end")]),
                   ui.facts([("name", "value"), ("kind", "facts"), ("columns", "two")]),
                   meta="a section's meta"),
        ui.section("tiles",
                   ui.tiles(ui.tile("demo:tile-on", "On", sub="selected", icon="weather-clear-symbolic", selected=True),
                            ui.tile("demo:tile-off", "Off", sub="not selected", icon="weather-clear-night-symbolic"),
                            ui.tile("demo:tile-more", "Another", sub="tiles wrap", icon="bluetooth-active-symbolic"))),
        ui.section("image · box",
                   ui.box(ui.image(icon="image-x-generic-symbolic", width=48, height=48),
                          ui.box(ui.text("A box", style="title"),
                                 ui.text("Children in a column, or in a row (horizontal); spacing between them.", style="sub"),
                                 spacing=2),
                          horizontal=True, spacing=16)),
        ui.section("empty", ui.empty("Nothing here", text="What a page with nothing to show says", icon="folder-symbolic")),
        ui.section("tabs · columns · scroll · bar",
                   ui.text("The tabs over these pages are tabs; Layouts shows columns and scroll as a sidebar "
                           "and a panel. A bar sits at the bottom of a page, its actions at the right.", style="sub")),
    ]


LAYOUTS = ["Sidebar", "Right panel", "Both"]
PLACES = [("home", "Home", "user-home-symbolic"), ("documents", "Documents", "folder-symbolic"),
          ("music", "Music", "audio-x-generic-symbolic"), ("pictures", "Pictures", "image-x-generic-symbolic")]


def layouts_tab(state):
    """The light apps' layouts, live: a sidebar, a panel at the right, or both."""
    layout = state.get("layout") or LAYOUTS[0]
    place = state.get("place") or "documents"
    name = next(t for i, t, _ in PLACES if i == place)
    code = {
        "Sidebar": 'ui.columns(ui.scroll(…, style="sidebar", width=200), ui.scroll(…))',
        "Right panel": 'ui.columns(ui.scroll(…), ui.scroll(…, style="panel", width=240))',
        "Both": 'ui.columns(ui.scroll(…, style="sidebar", width=200), ui.scroll(…),\n'
                '           ui.scroll(…, style="panel", width=240))',
    }[layout]
    sidebar = ui.scroll(
        ui.text("PLACES", style="kicker"),
        *[ui.row(t, id="place:" + i, icon=icon, selected=i == place) for i, t, icon in PLACES],
        style="sidebar", width=200)
    page = ui.scroll(
        ui.box(ui.text(name, style="heading"), ui.segmented("layout", LAYOUTS, value=layout), horizontal=True, spacing=16),
        ui.text({"Sidebar": "A sidebar at the left, as in Pass and Files: choose a place there.",
                 "Right panel": "A panel at the right, as Viewer's details: what is chosen, in detail.",
                 "Both": "A page between its columns, as in Files: choose a place at the left; "
                         "its details show at the right."}[layout], style="sub"),
        ui.section("The layout", ui.text(code, style="mono")),
        ui.section("In the page", *[ui.row("Item %d in %s" % (k, name), sub="A row of the page") for k in range(1, 5)]))
    panel = ui.scroll(
        ui.image(icon=next(c for i, _, c in PLACES if i == place), width=56, height=56),
        ui.text(name, style="title"),
        ui.text("DETAILS", style="kicker"),
        ui.facts([("place", name), ("items", "4"), ("layout", layout)]),
        style="panel", width=240)
    kids = {"Sidebar": [sidebar, page], "Right panel": [page, panel], "Both": [sidebar, page, panel]}[layout]
    return [ui.columns(*kids)]


@app.view
def view(state):
    # The page does not scroll: each tab's content does, and Layouts'
    # columns fill the window.
    return ui.page(ui.tabs("tab",
        ui.tab("system", "System", ui.scroll(*system_tab(state))),
        ui.tab("media", "Playing", ui.scroll(*media_tab(state))),
        ui.tab("workspaces", "Workspaces", ui.scroll(*workspaces_tab(state))),
        ui.tab("clipboard", "Copied", ui.scroll(*clipboard_tab(state))),
        ui.tab("notes", "Notifications", ui.scroll(*notes_tab(state))),
        ui.tab("files", "Files", ui.scroll(*file_tab(state))),
        ui.tab("components", "Components", ui.scroll(*components_tab(state))),
        ui.tab("layouts", "Layouts", *layouts_tab(state)),
        value=state.get("tab")), scroll=False)


@app.on("demo:*")
def demo(ev):
    """Any input of Components: what it sent."""
    shown = ev.value if not (ev.id or "").endswith("password") else "•" * len(ev.value or "")
    app.state["last"] = "%s %s = %r" % (ev.type, ev.id, shown)


@app.on("layout", "change")
def layout(ev):
    app.state["layout"] = ev.value


@app.on("place:*", "click")
def place(ev):
    app.state["place"] = ev.id[len("place:"):]


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
